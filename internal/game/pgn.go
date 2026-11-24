package game

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const MaxPGNBytes = 256 * 1024
const MaxVariationDepth = 16

var sanPattern = regexp.MustCompile(`^(?:O-O(?:-O)?|0-0(?:-0)?|[KQRBN]?[a-h]?[1-8]?x?[a-h][1-8](?:=[QRBN])?)[+#]?[!?]{0,2}$`)
var tagPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// ImportPGN accepts one game's bounded mainline. Variations and comments are
// scanned with explicit nesting limits but are not imported into match history.
func ImportPGN(input string) (*Game, error) {
	if len(input) > MaxPGNBytes {
		return nil, errors.New("PGN exceeds 256 KiB")
	}
	p := pgnScanner{text: input}
	tags := map[string]string{}
	for {
		if err := p.space(); err != nil {
			return nil, err
		}
		if p.at == len(input) || input[p.at] != '[' {
			break
		}
		name, value, err := p.tag()
		if err != nil {
			return nil, err
		}
		if _, ok := tags[name]; ok {
			return nil, fmt.Errorf("duplicate PGN tag %s", name)
		}
		tags[name] = value
		if len(tags) > 64 {
			return nil, errors.New("too many PGN tags")
		}
	}
	initial := StartFEN
	if fen, ok := tags["FEN"]; ok {
		initial = fen
	}
	if tags["SetUp"] == "1" && tags["FEN"] == "" {
		return nil, errors.New("SetUp 1 requires a FEN tag")
	}
	if setup, ok := tags["SetUp"]; ok && setup != "0" && setup != "1" {
		return nil, errors.New("SetUp tag must be 0 or 1")
	}
	if tags["SetUp"] == "0" && tags["FEN"] != "" && tags["FEN"] != StartFEN {
		return nil, errors.New("SetUp 0 contradicts a nonstandard FEN tag")
	}
	variant := strings.ToLower(tags["Variant"])
	chess960 := variant == "chess960" || variant == "fischerandom" || variant == "fischer random"
	if variant != "" && variant != "standard" && !chess960 {
		return nil, errors.New("unsupported PGN variant")
	}
	g, err := New(initial, chess960)
	if err != nil {
		return nil, fmt.Errorf("PGN starting position: %w", err)
	}
	g.Tags = tags
	result := ""
	depth := 0
	for {
		if err := p.space(); err != nil {
			return nil, err
		}
		if p.at == len(input) {
			break
		}
		switch input[p.at] {
		case '(':
			if result != "" && depth == 0 {
				return nil, errors.New("variation follows final result")
			}
			depth++
			p.at++
			if depth > MaxVariationDepth {
				return nil, errors.New("PGN variations exceed depth limit")
			}
			continue
		case ')':
			if depth == 0 {
				return nil, errors.New("unmatched PGN variation end")
			}
			depth--
			p.at++
			continue
		case '[':
			return nil, errors.New("only one PGN game may be imported")
		}
		token, err := p.token()
		if err != nil {
			return nil, err
		}
		if depth > 0 {
			continue
		}
		if strings.HasPrefix(token, "$") {
			if len(token) < 2 || strings.Trim(token[1:], "0123456789") != "" {
				return nil, errors.New("invalid PGN annotation")
			}
			continue
		}
		if token == "..." {
			continue
		}
		// Move numbers can be separate (1. e4) or attached (1.e4, 1...e5).
		i := 0
		for i < len(token) && token[i] >= '0' && token[i] <= '9' {
			i++
		}
		if i > 0 && i < len(token) && token[i] == '.' {
			for i < len(token) && token[i] == '.' {
				i++
			}
			token = token[i:]
			if token == "" {
				continue
			}
		}
		if validResult(token) {
			if result != "" {
				return nil, errors.New("PGN must have exactly one final result marker")
			}
			result = token
			continue
		}
		if result != "" {
			return nil, errors.New("moves follow the final PGN result marker")
		}
		if g.Result != "*" {
			return nil, fmt.Errorf("move %d continues after %s", len(g.Moves)+1, g.Reason)
		}
		if !sanPattern.MatchString(token) {
			return nil, fmt.Errorf("move %d has invalid notation: %s", len(g.Moves)+1, token)
		}
		san := strings.TrimRight(token, "!?")
		m, err := g.Board.ParseMove(san)
		if err != nil {
			return nil, fmt.Errorf("move %d is illegal or ambiguous: %s", len(g.Moves)+1, token)
		}
		canonical := m.San(g.Board)
		if (strings.HasSuffix(san, "#") && !strings.HasSuffix(canonical, "#")) || (strings.HasSuffix(san, "+") && !strings.HasSuffix(canonical, "+") && !strings.HasSuffix(canonical, "#")) {
			return nil, fmt.Errorf("move %d incorrectly claims check or checkmate: %s", len(g.Moves)+1, token)
		}
		if strings.TrimRight(strings.ReplaceAll(san, "0", "O"), "+#") != strings.TrimRight(canonical, "+#") {
			return nil, fmt.Errorf("move %d is not valid SAN: %s (expected %s)", len(g.Moves)+1, token, canonical)
		}
		if _, err = g.Apply(m.Uci(g.Board)); err != nil {
			return nil, fmt.Errorf("move %d: %w", len(g.Moves)+1, err)
		}
	}
	if depth != 0 {
		return nil, errors.New("unterminated PGN variation")
	}
	if result == "" {
		return nil, errors.New("PGN needs exactly one final result marker (*, 1-0, 0-1 or 1/2-1/2)")
	}
	if declared, ok := tags["Result"]; ok && declared != result {
		return nil, fmt.Errorf("Result tag says %s, but movetext ends %s", declared, result)
	}
	if g.Result != "*" && g.Result != result {
		return nil, fmt.Errorf("result %s contradicts terminal position (%s by %s)", result, g.Result, g.Reason)
	}
	if g.Result == "*" && result != "*" {
		g.Result, g.Reason = result, "imported_result"
	}
	g.Tags["Result"] = result
	return g, nil
}

func validResult(s string) bool { return s == "*" || s == "1-0" || s == "0-1" || s == "1/2-1/2" }

type pgnScanner struct {
	text string
	at   int
}

func (p *pgnScanner) space() error {
	for p.at < len(p.text) {
		switch c := p.text[p.at]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.at++
		case c == ';':
			for p.at < len(p.text) && p.text[p.at] != '\n' {
				p.at++
			}
		case c == '{':
			p.at++
			for p.at < len(p.text) && p.text[p.at] != '}' {
				if p.text[p.at] == '{' {
					return errors.New("nested PGN comment")
				}
				p.at++
			}
			if p.at == len(p.text) {
				return errors.New("unterminated PGN comment")
			}
			p.at++
		default:
			return nil
		}
	}
	return nil
}

func (p *pgnScanner) token() (string, error) {
	start := p.at
	for p.at < len(p.text) && !strings.ContainsRune(" \t\r\n{};()[]", rune(p.text[p.at])) {
		p.at++
		if p.at-start > 128 {
			return "", errors.New("PGN token is too long")
		}
	}
	if start == p.at {
		return "", errors.New("unexpected PGN delimiter")
	}
	return p.text[start:p.at], nil
}

func (p *pgnScanner) tag() (string, string, error) {
	p.at++
	for p.at < len(p.text) && unicode.IsSpace(rune(p.text[p.at])) {
		p.at++
	}
	start := p.at
	for p.at < len(p.text) && !unicode.IsSpace(rune(p.text[p.at])) && p.text[p.at] != '"' {
		p.at++
	}
	name := p.text[start:p.at]
	if !tagPattern.MatchString(name) || len(name) > 64 {
		return "", "", errors.New("invalid PGN tag name")
	}
	for p.at < len(p.text) && unicode.IsSpace(rune(p.text[p.at])) {
		p.at++
	}
	if p.at == len(p.text) || p.text[p.at] != '"' {
		return "", "", errors.New("PGN tag value must be quoted")
	}
	p.at++
	var value strings.Builder
	for p.at < len(p.text) && p.text[p.at] != '"' {
		c := p.text[p.at]
		p.at++
		if c == '\\' {
			if p.at == len(p.text) || (p.text[p.at] != '\\' && p.text[p.at] != '"') {
				return "", "", errors.New("invalid PGN tag escape")
			}
			c = p.text[p.at]
			p.at++
		}
		if c < 32 || c == 127 {
			return "", "", errors.New("control character in PGN tag")
		}
		value.WriteByte(c)
		if value.Len() > 2048 {
			return "", "", errors.New("PGN tag value is too long")
		}
	}
	if p.at == len(p.text) {
		return "", "", errors.New("unterminated PGN tag")
	}
	p.at++
	for p.at < len(p.text) && unicode.IsSpace(rune(p.text[p.at])) {
		p.at++
	}
	if p.at == len(p.text) || p.text[p.at] != ']' {
		return "", "", errors.New("unterminated PGN tag")
	}
	p.at++
	return name, value.String(), nil
}

func ExportPGN(g *Game) string {
	tags := map[string]string{}
	for k, v := range g.Tags {
		if tagPattern.MatchString(k) {
			tags[k] = v
		}
	}
	result := g.Result
	if !validResult(result) {
		result = "*"
	}
	tags["Result"] = result
	if g.InitialFEN != StartFEN {
		tags["SetUp"] = "1"
		tags["FEN"] = g.InitialFEN
	} else {
		delete(tags, "SetUp")
		delete(tags, "FEN")
	}
	if g.Chess960 {
		tags["Variant"] = "Chess960"
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, k := range keys {
		v := strings.Map(func(r rune) rune {
			if r < 32 || r == 127 {
				return ' '
			}
			return r
		}, tags[k])
		v = strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(v)
		fmt.Fprintf(&out, "[%s \"%s\"]\n", k, v)
	}
	out.WriteByte('\n')
	b, err := ValidateFEN(g.InitialFEN)
	if err != nil {
		return out.String() + result + "\n"
	}
	moveNr, side := b.MoveNr, b.SideToMove
	for i, m := range g.Moves {
		if side == 0 {
			out.WriteString(strconv.Itoa(moveNr))
			out.WriteString(". ")
		} else if i == 0 {
			out.WriteString(strconv.Itoa(moveNr))
			out.WriteString("... ")
		}
		out.WriteString(m.SAN)
		out.WriteByte(' ')
		if side == 1 {
			moveNr++
		}
		side ^= 1
	}
	out.WriteString(result)
	out.WriteByte('\n')
	return out.String()
}

// Package game defines Chess's strict input and adjudication contract on top of
// malbrecht/chess's legal move generator.
package game

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/malbrecht/chess"
)

const StartFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
const MaxMoves = 2048

type Move struct {
	UCI string `json:"uci"`
	SAN string `json:"san"`
	FEN string `json:"fen"`
}

type Game struct {
	InitialFEN string
	Board      *chess.Board
	Moves      []Move
	History    []*chess.Board
	Result     string
	Reason     string
	Tags       map[string]string
	Chess960   bool
}

func New(initial string, chess960 bool) (*Game, error) {
	b, err := ValidateFEN(initial)
	if err != nil {
		return nil, err
	}
	// Custom FENs can retain Chess960 castling even without a Variant tag.
	for side, rook := range b.CastleSq {
		if rook == chess.NoSquare {
			continue
		}
		file := 0
		if side >= 2 {
			file = 7
		}
		if rook.File() != file || b.Piece[chess.Square(4, rook.Rank())] != chess.Piece((side&1)|chess.King) {
			chess960 = true
		}
	}
	g := &Game{InitialFEN: b.Fen(), Board: b, History: []*chess.Board{b}, Tags: map[string]string{}, Chess960: chess960}
	g.Result, g.Reason = Outcome(b, g.History)
	return g, nil
}

func Replay(initial string, moves []string, chess960 bool) (*Game, error) {
	if len(moves) > MaxMoves {
		return nil, errors.New("too many moves")
	}
	g, err := New(initial, chess960)
	if err != nil {
		return nil, err
	}
	for i, uci := range moves {
		if _, err = g.Apply(uci); err != nil {
			return nil, fmt.Errorf("move %d: %w", i+1, err)
		}
	}
	return g, nil
}

func (g *Game) Apply(uci string) (Move, error) {
	if g.Result != "*" {
		return Move{}, errors.New("game has already ended")
	}
	if len(g.Moves) >= MaxMoves {
		return Move{}, errors.New("move limit reached")
	}
	m, err := LegalMove(g.Board, uci, g.Chess960)
	if err != nil {
		return Move{}, err
	}
	entry := Move{UCI: m.Uci(g.Board), SAN: m.San(g.Board)}
	g.Board = g.Board.MakeMove(m)
	entry.FEN = g.Board.Fen()
	g.Moves = append(g.Moves, entry)
	g.History = append(g.History, g.Board)
	g.Result, g.Reason = Outcome(g.Board, g.History)
	return entry, nil
}

var uciPattern = regexp.MustCompile(`^[a-h][1-8][a-h][1-8][qrbn]?$`)

// LegalMove resolves strict UCI against generated legal moves rather than using
// the library's deliberately forgiving algebraic parser at the HTTP boundary.
func LegalMove(b *chess.Board, uci string, chess960 bool) (chess.Move, error) {
	if !uciPattern.MatchString(uci) {
		return chess.NullMove, errors.New("enter a coordinate move such as e2e4 or a7a8n")
	}
	for _, m := range b.LegalMoves() {
		if m.Uci(b) == uci {
			return m, nil
		}
		if !chess960 && len(uci) == 4 && b.Piece[m.From].Type() == chess.King && b.Piece[m.To] == chess.Piece(b.SideToMove|chess.Rook) && m.From.File() == 4 {
			toFile := 6
			if m.To < m.From {
				toFile = 2
			}
			if m.From.String()+chess.Square(toFile, m.From.Rank()).String() == uci {
				return m, nil
			}
		}
	}
	return chess.NullMove, errors.New("that move is not legal in the current position")
}

// ValidateFEN validates six complete fields before invoking the permissive
// library parser. It checks position consistency, not historical reachability.
func ValidateFEN(fen string) (*chess.Board, error) {
	if len(fen) > 256 {
		return nil, errors.New("FEN is too long")
	}
	f := strings.Fields(fen)
	if len(f) != 6 {
		return nil, errors.New("FEN must contain all six fields")
	}
	ranks := strings.Split(f[0], "/")
	if len(ranks) != 8 {
		return nil, errors.New("FEN must contain eight ranks")
	}
	kingCount := [2]int{}
	for rank, line := range ranks {
		width := 0
		previousDigit := false
		for _, c := range line {
			if c >= '1' && c <= '8' {
				if previousDigit {
					return nil, errors.New("adjacent empty-square counts in FEN")
				}
				width += int(c - '0')
				previousDigit = true
			} else {
				if !strings.ContainsRune("PNBRQKpnbrqk", c) {
					return nil, errors.New("invalid FEN piece")
				}
				if (rank == 0 || rank == 7) && (c == 'p' || c == 'P') {
					return nil, errors.New("pawns cannot occupy the first or eighth rank")
				}
				if c == 'K' {
					kingCount[0]++
				}
				if c == 'k' {
					kingCount[1]++
				}
				width++
				previousDigit = false
			}
		}
		if width != 8 {
			return nil, errors.New("each FEN rank must contain eight squares")
		}
	}
	if kingCount != [2]int{1, 1} {
		return nil, errors.New("FEN must contain exactly one king of each color")
	}
	if f[1] != "w" && f[1] != "b" {
		return nil, errors.New("FEN active color must be w or b")
	}
	for i := 4; i < 6; i++ {
		if f[i] == "" || strings.Trim(f[i], "0123456789") != "" {
			return nil, errors.New("FEN counters must be decimal integers")
		}
		n, err := strconv.Atoi(f[i])
		if err != nil || n > 1000000 || (i == 5 && n < 1) {
			return nil, errors.New("invalid FEN move counter")
		}
	}
	if f[2] != "-" {
		if len(f[2]) > 4 {
			return nil, errors.New("too many FEN castling rights")
		}
		seen := map[rune]bool{}
		for _, c := range f[2] {
			if !strings.ContainsRune("KQABCDEFGHkqabcdefgh", c) || seen[c] {
				return nil, errors.New("invalid or duplicate FEN castling right")
			}
			seen[c] = true
		}
	}
	if f[3] != "-" && (len(f[3]) != 2 || f[3][0] < 'a' || f[3][0] > 'h' || (f[3][1] != '3' && f[3][1] != '6')) {
		return nil, errors.New("invalid FEN en-passant square")
	}
	b, err := chess.ParseFen(strings.Join(f, " "))
	if err != nil {
		return nil, errors.New("invalid FEN")
	}
	if f[2] != "-" {
		count := 0
		for _, sq := range b.CastleSq {
			if sq != chess.NoSquare {
				count++
			}
		}
		if count != len(f[2]) {
			return nil, errors.New("castling rights must identify distinct rooks beside a king on its home rank")
		}
	}
	if b.EpSquare != chess.NoSquare {
		rank, delta := 5, -8
		if b.SideToMove == chess.Black {
			rank, delta = 2, 8
		}
		if b.EpSquare.Rank() != rank || b.Piece[b.EpSquare] != chess.NoPiece || b.Piece[int(b.EpSquare)+delta] != chess.Piece((b.SideToMove^1)|chess.Pawn) || b.Piece[int(b.EpSquare)-delta] != chess.NoPiece || b.Rule50 != 0 {
			return nil, errors.New("en-passant square is inconsistent with the preceding double pawn move")
		}
	}
	// The player who just moved cannot have left their own king attacked.
	other := *b
	other.SideToMove ^= 1
	if check, _ := other.IsCheckOrMate(); check {
		return nil, errors.New("the side not to move cannot be in check")
	}
	return b, nil
}

// PositionKey omits counters and irrelevant en-passant targets. Only an actual
// legal en-passant capture distinguishes repetition positions.
func PositionKey(b *chess.Board) string {
	f := strings.Fields(b.Fen())
	f[3] = "-"
	if b.EpSquare != chess.NoSquare {
		for _, m := range b.LegalMoves() {
			if b.Piece[m.From].Type() == chess.Pawn && m.To == b.EpSquare && m.From.File() != m.To.File() {
				f[3] = b.EpSquare.String()
				break
			}
		}
	}
	return strings.Join(f[:4], " ")
}

func Outcome(b *chess.Board, history []*chess.Board) (result, reason string) {
	check, mate := b.IsCheckOrMate()
	if mate {
		if !check {
			return "1/2-1/2", "stalemate"
		}
		if b.SideToMove == chess.White {
			return "0-1", "checkmate"
		}
		return "1-0", "checkmate"
	}
	if b.Rule50 >= 150 {
		return "1/2-1/2", "seventy_five_moves"
	}
	if InsufficientMaterial(b) {
		return "1/2-1/2", "insufficient_material"
	}
	key, count := PositionKey(b), 0
	for _, p := range history {
		if PositionKey(p) == key {
			count++
		}
	}
	if count >= 5 {
		return "1/2-1/2", "fivefold_repetition"
	}
	return "*", ""
}

func InsufficientMaterial(b *chess.Board) bool {
	knights, bishops, shade := 0, 0, -1
	sameShade := true
	for sq, p := range b.Piece {
		switch p.Type() {
		case chess.NoPiece, chess.King:
		case chess.Knight:
			knights++
		case chess.Bishop:
			bishops++
			c := (sq/8 + sq%8) % 2
			if shade != -1 && shade != c {
				sameShade = false
			}
			shade = c
		default:
			return false
		}
	}
	return knights+bishops <= 1 || (knights == 0 && sameShade)
}

// Chess960 enumerates the Scharnagl 0..959 positions, including mirrored armies
// and explicit rook-file rights. A random uniform index gives uniform starts.
func Chess960(index int) (string, error) {
	if index < 0 || index >= 960 {
		return "", errors.New("Chess960 index must be between 0 and 959")
	}
	var row [8]byte
	n := index
	row[(n%4)*2+1] = 'B'
	n /= 4
	row[(n%4)*2] = 'B'
	n /= 4
	empty := func() []int {
		var e []int
		for i, c := range row {
			if c == 0 {
				e = append(e, i)
			}
		}
		return e
	}
	e := empty()
	row[e[n%6]] = 'Q'
	n /= 6
	e = empty()
	pairs := [][2]int{{0, 1}, {0, 2}, {0, 3}, {0, 4}, {1, 2}, {1, 3}, {1, 4}, {2, 3}, {2, 4}, {3, 4}}
	p := pairs[n]
	row[e[p[0]]] = 'N'
	row[e[p[1]]] = 'N'
	e = empty()
	row[e[0]], row[e[1]], row[e[2]] = 'R', 'K', 'R'
	white := string(row[:])
	rights := string([]byte{'A' + byte(e[2]), 'A' + byte(e[0]), 'a' + byte(e[2]), 'a' + byte(e[0])})
	return strings.ToLower(white) + "/pppppppp/8/8/8/8/PPPPPPPP/" + white + " w " + rights + " - 0 1", nil
}

func Random960() (string, error) {
	index, err := Random960Index()
	if err != nil {
		return "", err
	}
	return Chess960(index)
}

func Random960Index() (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(960))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

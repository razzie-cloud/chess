package puzzle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/malbrecht/chess"
	"github.com/razzie-cloud/chess/internal/game"
)

// Validate replays a proof independently of the search. It never trusts tree counts.
func Validate(d Definition) error {
	src := Source{Slot: d.Slot, FEN: d.FEN, Solver: d.Solver, Mate: d.Mate, Title: d.Title, Provenance: d.Provenance, License: d.License}
	if err := CheckSource(src); err != nil {
		return err
	}
	if d.Provenance == "" || d.License == "" {
		return errors.New("publication requires provenance and license metadata")
	}
	b, _ := game.ValidateFEN(d.FEN)
	if b.Fen() != d.FEN {
		return errors.New("initial FEN is not canonical")
	}
	seen := make(map[*Node]bool)
	var walk func(*Node, *chess.Board, []*chess.Board, int) (statistics, error)
	walk = func(n *Node, b *chess.Board, history []*chess.Board, left int) (statistics, error) {
		if n == nil || seen[n] || left < 1 || len(seen) > 16 {
			return statistics{}, errors.New("invalid, shared, cyclic or oversized proof node")
		}
		seen[n] = true
		if b.SideToMove != color(d.Solver) || n.FEN != b.Fen() {
			return statistics{}, errors.New("solver position does not match replay")
		}
		if result, _ := game.Outcome(b, history); result != "*" {
			return statistics{}, errors.New("proof continues after terminal position")
		}
		var move chess.Move
		found := false
		for _, m := range b.LegalMoves() {
			if m.Uci(b) == n.Move {
				move = m
				found = true
				break
			}
		}
		if !found {
			return statistics{}, errors.New("prescribed move is not canonical legal UCI")
		}
		after := b.MakeMove(move)
		if n.After != after.Fen() {
			return statistics{}, errors.New("resulting solver FEN does not match replay")
		}
		h := appendHistory(history, after)
		result, reason := game.Outcome(after, h)
		stats := statistics{decisions: 1, depth: 1}
		if result != "*" {
			if reason != "checkmate" || after.SideToMove == color(d.Solver) || len(n.Replies) != 0 {
				return statistics{}, errors.New("successful leaf must be defender checkmate")
			}
			stats.lines = 1
			return stats, nil
		}
		moves := ordered(after)
		if len(moves) < 1 || len(moves) > 3 || len(n.Replies) != len(moves) {
			return statistics{}, errors.New("proof omits a legal defense or has more than three replies")
		}
		if len(moves) > 1 {
			stats.forks = 1
		}
		for i, m := range moves {
			r := n.Replies[i]
			if r.Move != m.Uci(after) {
				return statistics{}, errors.New("defenses must cover every legal move exactly once in UCI order")
			}
			next := after.MakeMove(m)
			if r.FEN != next.Fen() {
				return statistics{}, errors.New("defense FEN does not match replay")
			}
			child, err := walk(r.Next, next, appendHistory(h, next), left-1)
			if err != nil {
				return statistics{}, err
			}
			stats.forks += child.forks
			stats.lines += child.lines
			stats.decisions += child.decisions
			if child.depth+1 > stats.depth {
				stats.depth = child.depth + 1
			}
		}
		if stats.forks > 1 || stats.lines > 3 {
			return statistics{}, errors.New("proof contains a second fork or more than three complete lines")
		}
		return stats, nil
	}
	stats, err := walk(d.Root, b, []*chess.Board{b}, d.Mate)
	if err != nil {
		return err
	}
	if stats.forks != d.Forks || stats.lines != d.Lines || stats.decisions != d.Decisions {
		return errors.New("proof metadata counts disagree with replay")
	}
	if stats.depth != d.Mate {
		return errors.New("mate length disagrees with proof depth")
	}
	if d.Digest != ContentDigest(d) {
		return errors.New("content digest mismatch")
	}
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(data) > DefaultLimits().MaxBytes {
		return errors.New("proof output too large")
	}
	return nil
}

func Verify(bundle Bundle) error {
	if bundle.Version != FormatVersion || bundle.Compiler != CompilerVersion {
		return errors.New("unsupported puzzle bundle version")
	}
	if len(bundle.Collections) == 0 || len(bundle.Collections) > 100 {
		return errors.New("bundle must contain 1..100 collections")
	}
	ids := make(map[string]bool)
	for _, c := range bundle.Collections {
		if !ValidCollectionID(c.ID) || ids[c.ID] || len(c.Title) < 1 || len(c.Title) > 160 || len(c.Description) > 2000 {
			return fmt.Errorf("invalid or duplicate collection %q", c.ID)
		}
		ids[c.ID] = true
		if len(c.Puzzles) == 0 || len(c.Puzzles) > 256 {
			return fmt.Errorf("collection %s must contain 1..256 puzzles", c.ID)
		}
		slots := make(map[int]bool)
		for _, d := range c.Puzzles {
			if slots[d.Slot] {
				return fmt.Errorf("collection %s repeats slot %d", c.ID, d.Slot)
			}
			slots[d.Slot] = true
			if err := Validate(d); err != nil {
				return fmt.Errorf("collection %s slot %d: %w", c.ID, d.Slot, err)
			}
		}
	}
	return nil
}

// CompileManifest never returns a partial publishable bundle after a selected failure.
func CompileManifest(ctx context.Context, m Manifest, limits Limits) (Bundle, []Report, error) {
	if m.Version != FormatVersion {
		return Bundle{}, nil, errors.New("unsupported source manifest version")
	}
	bundle := Bundle{Version: FormatVersion, Compiler: CompilerVersion}
	var reports []Report
	ids := make(map[string]bool)
	failed := false
	for _, c := range m.Collections {
		if !ValidCollectionID(c.ID) || ids[c.ID] {
			return Bundle{}, reports, errors.New("invalid or duplicate collection identity")
		}
		ids[c.ID] = true
		out := Collection{ID: c.ID, Title: c.Title, Description: c.Description}
		slots := make(map[int]bool)
		entries := append([]Source(nil), c.Entries...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Slot < entries[j].Slot })
		for _, src := range entries {
			if src.Slot < 0 || src.Slot > 255 || slots[src.Slot] {
				return Bundle{}, reports, fmt.Errorf("collection %s has an invalid or duplicate slot %d", c.ID, src.Slot)
			}
			slots[src.Slot] = true
			if !src.Publish {
				reports = append(reports, Report{Collection: c.ID, Slot: src.Slot, Status: "draft", FEN: src.FEN})
				continue
			}
			d, report, err := Compile(ctx, src, limits)
			report.Collection = c.ID
			reports = append(reports, report)
			if err != nil {
				failed = true
				continue
			}
			out.Puzzles = append(out.Puzzles, d)
		}
		if len(out.Puzzles) > 0 {
			bundle.Collections = append(bundle.Collections, out)
		}
	}
	if failed {
		return Bundle{}, reports, errors.New("selected entries failed preparation; previous output must be retained")
	}
	if err := Verify(bundle); err != nil {
		return Bundle{}, reports, err
	}
	return bundle, reports, nil
}

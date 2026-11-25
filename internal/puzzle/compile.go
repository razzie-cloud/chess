package puzzle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/malbrecht/chess"
	"github.com/razzie-cloud/chess/internal/game"
)

type failure struct {
	reason, fen string
	path        []string
}

func (f *failure) Error() string { return f.reason }

type search struct {
	ctx          context.Context
	limit        Limits
	solver       int
	nodes        int
	problem      *failure
	unrestricted bool
}

func (s *search) tick() error {
	s.nodes++
	if s.nodes > s.limit.MaxNodes {
		return &failure{reason: "search node budget exhausted"}
	}
	if err := s.ctx.Err(); err != nil {
		return &failure{reason: "search time budget exhausted"}
	}
	return nil
}

func ordered(b *chess.Board) []chess.Move {
	m := b.LegalMoves()
	sort.Slice(m, func(i, j int) bool { return m[i].Uci(b) < m[j].Uci(b) })
	return m
}

func (s *search) reject(reason string, b *chess.Board, path []string) {
	// Retain the first concrete structural rejection for the per-entry diagnostic.
	if s.problem == nil {
		s.problem = &failure{reason: reason, fen: b.Fen(), path: append([]string(nil), path...)}
	}
}

func (s *search) attacker(b *chess.Board, budget, forks int, history []*chess.Board, path []string) (*Node, int, bool, error) {
	if err := s.tick(); err != nil {
		return nil, 0, false, err
	}
	result, _ := game.Outcome(b, history)
	if result != "*" || budget == 0 {
		return nil, 0, false, nil
	}
	// Local iterative deepening makes each prescribed decision deterministic and shortest.
	for depth := 1; depth <= budget; depth++ {
		for _, m := range ordered(b) {
			if err := s.tick(); err != nil {
				return nil, 0, false, err
			}
			after := b.MakeMove(m)
			uci := m.Uci(b)
			p := appendPath(path, uci)
			h := appendHistory(history, after)
			result, reason := game.Outcome(after, h)
			n := &Node{FEN: b.Fen(), Move: uci, After: after.Fen()}
			if result != "*" {
				if reason == "checkmate" && after.SideToMove != s.solver {
					return n, 0, true, nil
				}
				continue
			}
			if depth == 1 {
				continue
			}
			replies := ordered(after)
			if len(replies) > 3 && !s.unrestricted {
				s.reject("more than three legal replies", after, p)
				continue
			}
			used := 0
			if len(replies) > 1 {
				used = 1
			}
			if used > forks {
				s.reject("second fork", after, p)
				continue
			}
			ok := true
			for _, reply := range replies {
				next := after.MakeMove(reply)
				ruci := reply.Uci(after)
				child, count, won, err := s.attacker(next, depth-1, forks-used, appendHistory(h, next), appendPath(p, ruci))
				if err != nil {
					return nil, 0, false, err
				}
				if !won {
					ok = false
					break
				}
				used += count
				if used > forks {
					s.reject("second fork", next, appendPath(p, ruci))
					ok = false
					break
				}
				n.Replies = append(n.Replies, Reply{Move: ruci, FEN: next.Fen(), Next: child})
			}
			if ok && len(replies) > 0 {
				return n, used, true, nil
			}
		}
	}
	return nil, 0, false, nil
}

func appendPath(path []string, move string) []string {
	p := make([]string, len(path)+1)
	copy(p, path)
	p[len(path)] = move
	return p
}
func appendHistory(history []*chess.Board, b *chess.Board) []*chess.Board {
	h := make([]*chess.Board, len(history)+1)
	copy(h, history)
	h[len(history)] = b
	return h
}

func CheckSource(src Source) error {
	if src.Slot < 0 || src.Slot > 255 {
		return errors.New("slot must be in 0..255")
	}
	if src.Mate < 1 || src.Mate > 5 {
		return errors.New("mate limit must be in 1..5")
	}
	if src.Solver != "w" && src.Solver != "b" {
		return errors.New("solver must be w or b")
	}
	b, err := game.ValidateFEN(src.FEN)
	if err != nil {
		return err
	}
	if b.SideToMove != color(src.Solver) {
		return errors.New("solver must be the side to move")
	}
	if result, _ := game.Outcome(b, []*chess.Board{b}); result != "*" {
		return errors.New("starting position is already terminal")
	}
	if len(src.Title) > 160 || len(src.Provenance) > 2000 || len(src.License) > 500 {
		return errors.New("source metadata is too long")
	}
	return nil
}

func color(s string) int {
	if s == "b" {
		return chess.Black
	}
	return chess.White
}

// FindMate calculates the shortest forced mate without the gameplay fork limits.
func FindMate(ctx context.Context, src Source, limits Limits) (int, error) {
	if err := CheckSource(src); err != nil {
		return 0, err
	}
	if limits.MaxMate < src.Mate || limits.MaxMate > 5 || limits.MaxNodes < 1 || limits.MaxDuration <= 0 || limits.MaxBytes < 1 {
		return 0, errors.New("invalid mate search limits")
	}
	ctx, cancel := context.WithTimeout(ctx, limits.MaxDuration)
	defer cancel()
	b, _ := game.ValidateFEN(src.FEN)
	s := search{ctx: ctx, limit: limits, solver: b.SideToMove}
	return s.shortest(b, src.Mate)
}

func (s *search) shortest(b *chess.Board, budget int) (int, error) {
	s.unrestricted = true
	defer func() { s.unrestricted = false }()
	root, _, ok, err := s.attacker(b, budget, s.limit.MaxNodes, []*chess.Board{b}, nil)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("no proven mate within search limit")
	}
	return measure(root, nil).depth, nil
}

func Compile(ctx context.Context, src Source, limits Limits) (Definition, Report, error) {
	start := time.Now()
	report := Report{Slot: src.Slot, Status: "rejected", FEN: src.FEN}
	fail := func(err error) (Definition, Report, error) {
		report.Reason = err.Error()
		report.ElapsedMS = time.Since(start).Milliseconds()
		var f *failure
		if errors.As(err, &f) {
			if f.fen != "" {
				report.FEN = f.fen
			}
			report.Path = f.path
		}
		return Definition{}, report, err
	}
	if err := CheckSource(src); err != nil {
		return fail(fmt.Errorf("invalid input: %w", err))
	}
	if limits.MaxMate < 1 || limits.MaxMate > 5 || limits.MaxNodes < 1 || limits.MaxDuration <= 0 || limits.MaxBytes < 1 {
		return fail(errors.New("invalid compiler limits"))
	}
	if src.Mate > limits.MaxMate {
		return fail(errors.New("mate depth budget exceeded"))
	}
	b, _ := game.ValidateFEN(src.FEN)
	ctx, cancel := context.WithTimeout(ctx, limits.MaxDuration)
	defer cancel()
	s := search{ctx: ctx, limit: limits, solver: b.SideToMove}
	depth, err := s.shortest(b, src.Mate)
	if err != nil {
		report.Nodes = s.nodes
		return fail(err)
	}
	root, _, ok, err := s.attacker(b, depth, 1, []*chess.Board{b}, nil)
	report.Nodes = s.nodes
	if err != nil {
		return fail(err)
	}
	if !ok {
		if s.problem != nil {
			return fail(s.problem)
		}
		return fail(errors.New("no proven mate within declared budget"))
	}
	d := Definition{Slot: src.Slot, FEN: b.Fen(), Solver: src.Solver, Mate: depth, Title: src.Title, Provenance: src.Provenance, License: src.License, Root: root}
	stats := measure(root, nil)
	d.Forks, d.Lines, d.Decisions = stats.forks, stats.lines, stats.decisions
	d.Digest = ContentDigest(d)
	if err := Validate(d); err != nil {
		return fail(fmt.Errorf("compiled proof verification: %w", err))
	}
	data, _ := json.Marshal(d)
	if len(data) > limits.MaxBytes {
		return fail(errors.New("compiled output budget exhausted"))
	}
	report.Status = "accepted"
	report.Reason = ""
	report.Mate = stats.depth
	report.Forks = d.Forks
	report.Lines = d.Lines
	report.Decisions = d.Decisions
	report.ForkPath = stats.forkPath
	report.ForkReplies = stats.forkReplies
	report.ElapsedMS = time.Since(start).Milliseconds()
	return d, report, nil
}

type statistics struct {
	forks, lines, decisions, depth, forkReplies int
	forkPath                                    []string
}

func measure(n *Node, path []string) statistics {
	s := statistics{decisions: 1, depth: 1}
	if len(n.Replies) == 0 {
		s.lines = 1
		return s
	}
	if len(n.Replies) > 1 {
		s.forks = 1
		s.forkPath = appendPath(path, n.Move)
		s.forkReplies = len(n.Replies)
	}
	for _, r := range n.Replies {
		c := measure(r.Next, appendPath(appendPath(path, n.Move), r.Move))
		s.forks += c.forks
		s.lines += c.lines
		s.decisions += c.decisions
		if c.depth+1 > s.depth {
			s.depth = c.depth + 1
		}
		if c.forkReplies > 0 {
			s.forkPath = c.forkPath
			s.forkReplies = c.forkReplies
		}
	}
	return s
}

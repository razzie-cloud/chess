package puzzle

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/malbrecht/chess"
	"github.com/razzie-cloud/chess/internal/game"
)

func fixture(t *testing.T, slot int) Source {
	t.Helper()
	data, err := os.ReadFile("../../puzzles/source.json")
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, s := range m.Collections[0].Entries {
		if s.Slot == slot {
			return s
		}
	}
	t.Fatalf("missing fixture slot %d", slot)
	return Source{}
}

func proof(t *testing.T, slot int) Definition {
	t.Helper()
	d, _, err := Compile(context.Background(), fixture(t, slot), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCompileForcedAndForks(t *testing.T) {
	for _, tc := range []struct{ slot, forks, lines int }{{0, 0, 1}, {9, 1, 2}, {34, 1, 3}} {
		d := proof(t, tc.slot)
		if d.Forks != tc.forks || d.Lines != tc.lines {
			t.Fatalf("slot %d counts: forks=%d lines=%d", tc.slot, d.Forks, d.Lines)
		}
		if err := Validate(d); err != nil {
			t.Fatal(err)
		}
		again := proof(t, tc.slot)
		a, _ := json.Marshal(d)
		b, _ := json.Marshal(again)
		if string(a) != string(b) {
			t.Fatal("compilation is not deterministic")
		}
	}
}

func TestMateLengthIsCalculatedBeforeApplyingBoardLimits(t *testing.T) {
	src := fixture(t, 0)
	src.Mate = 5
	d, report, err := Compile(t.Context(), src, DefaultLimits())
	if err != nil || d.Mate != 2 || report.Mate != 2 {
		t.Fatalf("mate-in-two was labeled with the search ceiling: %d, %d, %v", d.Mate, report.Mate, err)
	}
	src.FEN = "r2qrb2/p1pn1Qp1/1p4Nk/4PR2/3n4/7N/P5PP/R6K w - - 1 1"
	depth, err := FindMate(t.Context(), src, DefaultLimits())
	if err != nil || depth != 2 {
		t.Fatalf("original source 15 must be mate in two: %d %v", depth, err)
	}
	if _, _, err := Compile(t.Context(), src, DefaultLimits()); err == nil {
		t.Fatal("a longer solution bypassed the shortest mate's board limit")
	}
}

func TestOriginalPositionsAreAllMateInTwo(t *testing.T) {
	data, err := os.ReadFile("../../puzzles/legacy-razchess.fen")
	if err != nil {
		t.Fatal(err)
	}
	positions := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(positions) != 220 {
		t.Fatalf("original position count: %d", len(positions))
	}
	limits := DefaultLimits()
	limits.MaxDuration = 30 * time.Second // Race instrumentation slows the full legacy audit.
	for i, fen := range positions {
		src := Source{Slot: i, FEN: fen, Solver: strings.Fields(fen)[1], Mate: 5}
		depth, err := FindMate(t.Context(), src, limits)
		if err != nil || depth != 2 {
			t.Fatalf("original position %d: mate %d, %v", i, depth, err)
		}
	}
}

func TestSecondForkRejectedAndAlternativeStrategyChosen(t *testing.T) {
	src := Source{Slot: 0, FEN: "8/4Q3/8/8/1K6/8/k7/8 w - - 0 1", Solver: "w", Mate: 3, Provenance: "generated fixture", License: "MIT"}
	b, err := game.ValidateFEN(src.FEN)
	if err != nil {
		t.Fatal(err)
	}
	s := search{ctx: context.Background(), limit: DefaultLimits(), solver: b.SideToMove}
	root, _, ok, err := s.attacker(b, 3, 2, []*chess.Board{b}, nil)
	if err != nil || !ok {
		t.Fatal("fixture did not prove two-fork mate")
	}
	stats := measure(root, nil)
	if stats.forks != 2 || stats.lines != 3 {
		t.Fatal("fixture must fork twice while staying within three lines")
	}
	d := Definition{Slot: src.Slot, FEN: b.Fen(), Solver: src.Solver, Mate: src.Mate, Provenance: src.Provenance, License: src.License, Root: root, Forks: stats.forks, Lines: stats.lines, Decisions: stats.decisions}
	d.Digest = ContentDigest(d)
	if err := Validate(d); err == nil || !strings.Contains(err.Error(), "second fork") {
		t.Fatalf("second fork was not rejected: %v", err)
	}
	valid, _, err := Compile(context.Background(), src, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if valid.Mate != 3 || valid.Forks != 1 || valid.Lines != 2 {
		t.Fatal("compiler did not choose the alternative single-fork strategy")
	}
}

func TestFourLegalRepliesCannotPublish(t *testing.T) {
	b, err := game.ValidateFEN("3k4/8/8/8/8/8/7Q/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	m, err := game.LegalMove(b, "h2h5", false)
	if err != nil {
		t.Fatal(err)
	}
	after := b.MakeMove(m)
	moves := ordered(after)
	if len(moves) != 4 {
		t.Fatalf("fixture has %d replies, want exactly four", len(moves))
	}
	n := &Node{FEN: b.Fen(), Move: m.Uci(b), After: after.Fen()}
	for _, r := range moves {
		n.Replies = append(n.Replies, Reply{Move: r.Uci(after), FEN: after.MakeMove(r).Fen(), Next: &Node{}})
	}
	d := Definition{Slot: 0, FEN: b.Fen(), Solver: "w", Mate: 3, Provenance: "generated fixture", License: "MIT", Root: n, Forks: 1, Lines: 4, Decisions: 5}
	d.Digest = ContentDigest(d)
	if err := Validate(d); err == nil || !strings.Contains(err.Error(), "more than three replies") {
		t.Fatalf("four legal defenses were accepted: %v", err)
	}
}

func TestPromotionAndCanonicalCastlingProofs(t *testing.T) {
	for _, tc := range []struct{ fen, move string }{
		{"7k/5P2/6K1/8/8/8/8/8 w - - 0 1", "f7f8q"},
		{"4rkr1/4p1p1/8/8/8/8/8/4K2R w K - 149 1", "e1h1"},
	} {
		src := Source{Slot: 0, FEN: tc.fen, Solver: "w", Mate: 1, Provenance: "generated fixture", License: "MIT"}
		d, _, err := Compile(context.Background(), src, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if d.Root.Move != tc.move || len(d.Root.Replies) != 0 {
			t.Fatalf("proof chose %s, want %s", d.Root.Move, tc.move)
		}
		if err := Validate(d); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNoMateTerminalDrawsAndBudgets(t *testing.T) {
	for _, fen := range []string{game.StartFEN, "7k/6Q1/6K1/8/8/8/8/8 b - - 0 1", "7k/5Q2/6K1/8/8/8/8/8 b - - 0 1", "7k/8/6K1/8/8/8/8/8 w - - 0 1"} {
		s := Source{Slot: 0, FEN: fen, Solver: strings.Fields(fen)[1], Mate: 1, Provenance: "test", License: "test"}
		if _, _, err := Compile(context.Background(), s, DefaultLimits()); err == nil {
			t.Fatalf("unexpected proof for %s", fen)
		}
	}
	s := fixture(t, 0)
	limit := DefaultLimits()
	limit.MaxNodes = 1
	_, r, err := Compile(context.Background(), s, limit)
	if err == nil || !strings.Contains(r.Reason, "budget") {
		t.Fatal("node budget did not fail closed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Compile(ctx, s, DefaultLimits()); err == nil {
		t.Fatal("canceled proof accepted")
	}
	limit = DefaultLimits()
	limit.MaxBytes = 1
	if _, _, err := Compile(context.Background(), s, limit); err == nil {
		t.Fatal("oversized output accepted")
	}
	limit = DefaultLimits()
	limit.MaxDuration = time.Nanosecond
	if _, _, err := Compile(context.Background(), s, limit); err == nil {
		t.Fatal("expired search accepted")
	}
}

func TestVerifyRejectsMalformedTree(t *testing.T) {
	for _, mutate := range []func(*Definition){
		func(d *Definition) { d.Root.Move = "0000" },
		func(d *Definition) { d.Root.After = game.StartFEN },
		func(d *Definition) { d.Root.Replies = d.Root.Replies[:1] },
		func(d *Definition) { d.Root.Replies[1].Move = d.Root.Replies[0].Move },
		func(d *Definition) { d.Root.Replies[0].Next = d.Root },
		func(d *Definition) { d.Forks = 0 },
		func(d *Definition) { d.Digest = "wrong" },
		func(d *Definition) { d.Mate = 1 },
		func(d *Definition) { d.Mate++; d.Digest = ContentDigest(*d) },
		func(d *Definition) { d.Root.Replies[0].Next.Replies = []Reply{{Move: "a1a2", Next: &Node{}}} },
	} {
		d := proof(t, 9)
		mutate(&d)
		if err := Validate(d); err == nil {
			t.Fatal("malformed tree accepted")
		}
	}
}

func TestLookupRequiresExactCanonicalHistory(t *testing.T) {
	d := proof(t, 9)
	if n, err := Lookup(d, nil); err != nil || n != d.Root {
		t.Fatal("root lookup failed")
	}
	path := []string{d.Root.Move, d.Root.Replies[0].Move}
	if n, err := Lookup(d, path); err != nil || n != d.Root.Replies[0].Next {
		t.Fatal("child lookup failed")
	}
	for _, bad := range [][]string{{d.Root.Move}, {"a1a2", path[1]}, {path[0], "a1a2"}, append(path, "a1a2", "a2a3")} {
		if _, err := Lookup(d, bad); err == nil {
			t.Fatal("forged or skipped path accepted")
		}
	}
}

func TestSourceSlotsAndManifestFailure(t *testing.T) {
	s := fixture(t, 0)
	for _, slot := range []int{-1, 256} {
		s.Slot = slot
		if err := CheckSource(s); err == nil {
			t.Fatal("invalid slot accepted")
		}
	}
	for _, slot := range []int{0, 255} {
		s.Slot = slot
		if err := CheckSource(s); err != nil {
			t.Fatal(err)
		}
	}
	s = fixture(t, 0)
	bad := fixture(t, 1)
	bad.FEN = "broken"
	bad.Publish = true
	m := Manifest{Version: 1, Collections: []CollectionSource{{ID: "test-v1", Title: "Test", Entries: []Source{s, bad}}}}
	b, reports, err := CompileManifest(context.Background(), m, DefaultLimits())
	if err == nil || len(b.Collections) != 0 || len(reports) != 2 {
		t.Fatal("selected failure returned partial bundle")
	}
	m.Collections[0].Entries = []Source{s, s}
	if _, _, err := CompileManifest(context.Background(), m, DefaultLimits()); err == nil {
		t.Fatal("duplicate slot accepted")
	}
}

func TestBundledSeedAndDeterministicRecompilation(t *testing.T) {
	data, err := os.ReadFile("../db/seeds/puzzles.json")
	if err != nil {
		t.Fatal(err)
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	if err := Verify(b); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile("../../puzzles/source.json")
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	compiled, _, err := CompileManifest(context.Background(), m, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(compiled)
	y, _ := json.Marshal(b)
	if string(x) != string(y) {
		t.Fatal("bundled seed drift")
	}
	if got := len(b.Collections[0].Puzzles); got != 206 {
		t.Fatalf("published count changed: %d", got)
	}
	for i, d := range b.Collections[0].Puzzles {
		if d.Slot != i || d.Mate != 2 {
			t.Fatalf("puzzle %d has slot %d, mate %d", i+1, d.Slot, d.Mate)
		}
	}
}

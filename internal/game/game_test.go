package game

import (
	"strings"
	"testing"

	"github.com/malbrecht/chess"
)

func TestStrictFEN(t *testing.T) {
	valid := []string{StartFEN, "8/8/8/8/8/4k3/8/4K3 w - - 0 1", "4k3/8/8/8/3pP3/8/8/4K3 b - e3 0 3", "4k3/8/8/8/8/8/8/RK5R w HA - 0 1"}
	for _, fen := range valid {
		if _, err := ValidateFEN(fen); err != nil {
			t.Errorf("%s: %v", fen, err)
		}
	}
	invalid := []string{"", strings.TrimSuffix(StartFEN, " 0 1"), "8/8/8/8/8/8/8/8 w - - 0 1", "4k3/8/8/8/8/8/8/3KK3 w - - 0 1", "4k3/8/8/8/8/8/8/4K2P w - - 0 1", "4k3/8/8/8/8/8/8/4K3 w K - 0 1", "4k3/8/8/8/8/8/8/4K3 w Z - 0 1", "4k3/8/8/8/8/8/8/4K3 w - a6 0 1", "4k3/8/8/8/8/8/8/4K3 w - - -1 1", "4k3/8/8/8/8/8/8/4K3 w - - 0 0", "4k3/8/8/8/8/8/8/4K3 w - - 0 1 extra", "4k3/8/8/8/8/8/8/4K3 w KK - 0 1", "8/8/8/8/8/8/4k3/4K3 w - - 0 1", "4k3/8/8/8/8/8/4R3/4K3 w - - 0 1", "4k3/8/8/8/8/8/8/4K12 w - - 0 1"}
	for _, fen := range invalid {
		if _, err := ValidateFEN(fen); err == nil {
			t.Errorf("accepted invalid FEN: %q", fen)
		}
	}
}

func TestStrictUCIAndReconstruction(t *testing.T) {
	b, _ := ValidateFEN(StartFEN)
	for _, bad := range []string{"e4", "e2-e4", "e2e4 ", "e2e4x", "e2e4q", "0000", "e2e2", "E2E4", "e7e5", "e2e4\n"} {
		if _, err := LegalMove(b, bad, false); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	g, err := Replay(StartFEN, []string{"e2e4", "e7e5", "g1f3", "b8c6", "f1b5"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if g.Moves[4].SAN != "Bb5" || g.Board.SideToMove != chess.Black {
		t.Fatalf("unexpected reconstruction: %+v", g)
	}
}

func TestCustomFENRetainsChess960Castling(t *testing.T) {
	g, err := New("4k3/8/8/8/8/8/8/R3KR2 w FA - 0 1", false)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Chess960 {
		t.Fatal("custom Chess960 rights lost their variant")
	}
	if _, err = g.Apply("e1g1"); err == nil {
		t.Fatal("Chess960 accepted king-destination castling")
	}
	if _, err = g.Apply("e1f1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ExportPGN(g), `[Variant "Chess960"]`) {
		t.Fatal("export lost custom Chess960 variant")
	}
}

func TestCastlingCanonicalAndStationaryChess960(t *testing.T) {
	for _, tc := range []struct {
		fen, uci, want string
		variant        bool
	}{
		{"4k3/8/8/8/8/8/8/R3K2R w KQ - 0 1", "e1g1", "e1h1", false},
		{"4k3/8/8/8/8/8/8/R3K2R w KQ - 0 1", "e1c1", "e1a1", false},
		{"4k3/8/8/8/8/8/8/R5KR w HA - 0 1", "g1h1", "g1h1", true},
		{"4k3/8/8/8/8/8/8/R3KR2 w FA - 0 1", "e1f1", "e1f1", true},
		{"4k3/8/8/8/8/8/8/R1K4R w HA - 0 1", "c1a1", "c1a1", true},
	} {
		b, err := ValidateFEN(tc.fen)
		if err != nil {
			t.Fatal(err)
		}
		m, err := LegalMove(b, tc.uci, tc.variant)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.fen, tc.uci, err)
		}
		if m.Uci(b) != tc.want {
			t.Errorf("canonical %s != %s", m.Uci(b), tc.want)
		}
		next := b.MakeMove(m)
		king, rook := chess.G1, chess.F1
		if m.To < m.From {
			king, rook = chess.C1, chess.D1
		}
		if next.Piece[king] != chess.WK || next.Piece[rook] != chess.WR {
			t.Errorf("wrong castle placement: %s", next.Fen())
		}
	}
	// In Chess960 c1e1 is an ordinary king gesture, never inferred as castling.
	b, _ := ValidateFEN("4k3/8/8/8/8/8/8/R1K4R w HA - 0 1")
	if _, err := LegalMove(b, "c1e1", true); err == nil {
		t.Error("inferred ambiguous Chess960 castle")
	}
}

func TestAllChess960Starts(t *testing.T) {
	seen := map[string]bool{}
	for i := range 960 {
		fen, err := Chess960(i)
		if err != nil {
			t.Fatal(err)
		}
		if seen[fen] {
			t.Fatalf("duplicate at %d", i)
		}
		seen[fen] = true
		b, err := ValidateFEN(fen)
		if err != nil {
			t.Fatalf("index %d: %v", i, err)
		}
		bishops := []int{}
		rooks := []int{}
		king := -1
		for file := range 8 {
			p := b.Piece[file]
			if b.Piece[56+file] != p|1 {
				t.Fatalf("not mirrored at %d", i)
			}
			switch p {
			case chess.WB:
				bishops = append(bishops, file)
			case chess.WR:
				rooks = append(rooks, file)
			case chess.WK:
				king = file
			}
		}
		if len(bishops) != 2 || bishops[0]%2 == bishops[1]%2 || len(rooks) != 2 || rooks[0] >= king || rooks[1] <= king {
			t.Fatalf("invalid start %d: %s", i, fen)
		}
	}
	if fen, _ := Chess960(518); strings.Split(fen, " ")[0] != strings.Split(StartFEN, " ")[0] {
		t.Errorf("index 518 should be standard: %s", fen)
	}
	if _, err := Chess960(960); err == nil {
		t.Error("accepted index 960")
	}
}

func TestPromotionEnPassantPinnedAndOutcomes(t *testing.T) {
	g, err := New("7k/P7/8/8/8/8/8/4K3 w - - 0 1", false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := g.Apply("a7a8n")
	if err != nil || g.Board.Piece[chess.A8] != chess.WN || m.SAN != "a8=N" {
		t.Fatalf("underpromotion: %+v %v", m, err)
	}
	g, err = New("4k3/8/8/3pP3/8/8/8/4K3 w - d6 0 5", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.Apply("e5d6"); err != nil || g.Board.Piece[chess.D5] != chess.NoPiece {
		t.Fatal("en passant capture failed", err)
	}
	b, _ := ValidateFEN("k3r3/8/8/8/8/8/4R3/4K3 w - - 0 1")
	if _, err := LegalMove(b, "e2d2", false); err == nil {
		t.Error("pinned rook moved")
	}
	for _, tc := range []struct{ fen, result, reason string }{
		{"7k/6Q1/5K2/8/8/8/8/8 b - - 150 1", "1-0", "checkmate"},
		{"7k/5K2/6Q1/8/8/8/8/8 b - - 0 1", "1/2-1/2", "stalemate"},
		{"7k/8/5K2/8/8/8/8/R7 w - - 150 1", "1/2-1/2", "seventy_five_moves"},
		{"7k/8/5K2/8/8/8/8/B7 w - - 0 1", "1/2-1/2", "insufficient_material"},
	} {
		b, err := ValidateFEN(tc.fen)
		if err != nil {
			t.Fatal(err)
		}
		r, why := Outcome(b, []*chess.Board{b})
		if r != tc.result || why != tc.reason {
			t.Errorf("%s: %s %s", tc.fen, r, why)
		}
	}
}

func TestRepetitionUsesLegalEPAndFullHistory(t *testing.T) {
	b, _ := ValidateFEN("4k3/8/8/4p3/8/8/8/4K2R w - e6 0 2")
	c := *b
	c.EpSquare = chess.NoSquare
	c.Rule50 = 20
	if PositionKey(b) != PositionKey(&c) {
		t.Error("irrelevant EP prevented repetition")
	}
	b, _ = ValidateFEN("4k3/8/8/3Pp3/8/8/8/4K2R w - e6 0 2")
	c = *b
	c.EpSquare = chess.NoSquare
	if PositionKey(b) == PositionKey(&c) {
		t.Error("legal EP omitted from key")
	}
	b, _ = ValidateFEN("3rk3/8/8/3Pp3/8/8/8/3K4 w - e6 0 2")
	c = *b
	c.EpSquare = chess.NoSquare
	if PositionKey(b) != PositionKey(&c) {
		t.Error("pinned illegal EP included in key")
	}
	moves := []string{}
	for range 4 {
		moves = append(moves, "g1f3", "g8f6", "f3g1", "f6g8")
	}
	g, err := Replay(StartFEN, moves, false)
	if err != nil {
		t.Fatal(err)
	}
	if g.Result != "1/2-1/2" || g.Reason != "fivefold_repetition" {
		t.Fatalf("fivefold not adjudicated: %+v", g)
	}
}

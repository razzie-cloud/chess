package game

import (
	"strings"
	"testing"
)

func TestPGNImportAndRoundTrip(t *testing.T) {
	for _, input := range []string{
		`[Event "Cloud \"test\""]` + "\n[Result \"*\"]\n\n1.e4 {comment} e5 2.Nf3 (2.Bc4 Nf6 (2...Bc5)) Nc6 $1 3.Bb5 a6 *",
		"1. f3 e5 2. g4 Qh4# 0-1",
		"[SetUp \"1\"]\n[FEN \"4k3/8/8/8/8/8/8/R3K3 b - - 0 17\"]\n\n17... Kf7 18. Ra7+ *",
		"1. e4 e5 1-0",
	} {
		g, err := ImportPGN(input)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		pgn := ExportPGN(g)
		next, err := ImportPGN(pgn)
		if err != nil {
			t.Fatalf("export %q: %v", pgn, err)
		}
		if next.Board.Fen() != g.Board.Fen() || next.Result != g.Result || next.Reason != g.Reason {
			t.Errorf("roundtrip changed state")
		}
		if strings.Contains(input, "17...") && !strings.Contains(pgn, "17... Kf7") {
			t.Errorf("lost Black-to-move numbering: %s", pgn)
		}
		if strings.Contains(input, "e4 e5 1-0") && g.Reason != "imported_result" {
			t.Errorf("reported result reason: %s", g.Reason)
		}
	}
}

func TestPGNRejectsAmbiguousResultsAndMalformedInput(t *testing.T) {
	for _, input := range []string{
		"1. e4 e5", "1. e4 e5 * *", "1. e4 * e5", "[Result \"1-0\"]\n1. e4 0-1", "1. f3 e5 2. g4 Qh4# *", "1. f3 e5 2. g4 Qh4# 0-1 3. e3", "1. f3 e5 2. g4 Qh4# 3. e3 0-1", "1. e4xyz *", "1. e4 (1... e5 *", "1. e4 {missing *", "1. e4 ) *", "1. e4 *\n[Event \"second\"] 1.d4 *", "1. e4 " + strings.Repeat("(", 17) + "1.d4" + strings.Repeat(")", 17) + " *", "[SetUp \"1\"]\n1.e4 *", "[Variant \"Atomic\"]\n1.e4 *", "[Result \"*\"]\n[Result \"*\"]\n*", "1. -- *", strings.Repeat(" ", MaxPGNBytes+1),
	} {
		if _, err := ImportPGN(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestPGNTerminalSetupAndEscaping(t *testing.T) {
	input := "[FEN \"7k/6Q1/5K2/8/8/8/8/8 b - - 150 1\"]\n[SetUp \"1\"]\n\n1-0"
	g, err := ImportPGN(input)
	if err != nil {
		t.Fatal(err)
	}
	if g.Result != "1-0" || g.Reason != "checkmate" {
		t.Fatalf("wrong terminal result: %+v", g)
	}
	g.Tags["Event"] = "quoted \"\\\nvalue"
	if _, err := ImportPGN(ExportPGN(g)); err != nil {
		t.Fatal(err)
	}
}

func TestPGNRejectsFalseCheckAndConflictingSetup(t *testing.T) {
	for _, input := range []string{
		"1. e4# *", "1. e4+ *",
		"[SetUp \"0\"]\n[FEN \"4k3/8/8/8/8/8/8/R3K3 b - - 0 1\"]\n*",
	} {
		if _, err := ImportPGN(input); err == nil {
			t.Fatalf("accepted inconsistent notation: %q", input)
		}
	}
}

package opening

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/razzie-cloud/chess/internal/game"
)

func TestBundledSourceReplaysAndReproducesArtifact(t *testing.T) {
	f, err := os.Open("source/eco.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	prepared, count, err := Prepare(f)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3397 {
		t.Fatalf("expected 3397 legacy entries, got %d", count)
	}
	if !bytes.Equal(prepared, data) {
		t.Fatal("opening artifact drift: run go generate ./internal/opening")
	}
	if !bytes.Contains(prepared, []byte("e1h1")) || !bytes.Contains(prepared, []byte("e8a8")) {
		t.Fatal("castles were not normalized to king-to-rook")
	}
}

func TestDeepestPrefixAndDeparture(t *testing.T) {
	moves := strings.Fields("e2e4 e7e5 g1f3 b8c6 f1b5")
	e := Lookup(game.StartFEN, moves, "standard")
	if e == nil || e.Code != "C60" || e.Title != "Ruy Lopez" {
		t.Fatalf("recognized opening: %+v", e)
	}
	moves = append(moves, "a7a6")
	deep := Lookup(game.StartFEN, moves, "standard")
	if deep == nil || deep.Title == e.Title {
		t.Fatalf("expected deeper label: %+v", deep)
	}
	departed := Lookup(game.StartFEN, append(moves, "b5f1"), "standard")
	if departed == nil || *departed != *deep {
		t.Fatalf("lost deepest label after departure: %+v", departed)
	}
	// Reconstructing only from durable UCI yields the same label after restart.
	g, err := game.Replay(game.StartFEN, moves, false)
	if err != nil {
		t.Fatal(err)
	}
	var recovered []string
	for _, m := range g.Moves {
		recovered = append(recovered, m.UCI)
	}
	if got := Lookup(g.InitialFEN, recovered, "standard"); got == nil || *got != *deep {
		t.Fatalf("reconstruction: %+v", got)
	}
	g, err = game.ImportPGN("1. e4 e5 2. Nf3 Nc6 3. Bb5 a6 *")
	if err != nil {
		t.Fatal(err)
	}
	recovered = nil
	for _, m := range g.Moves {
		recovered = append(recovered, m.UCI)
	}
	if got := Lookup(g.InitialFEN, recovered, "standard"); got == nil || *got != *deep {
		t.Fatalf("imported opening: %+v", got)
	}
}

func TestSuppressionAndPrecedence(t *testing.T) {
	for _, mode := range []string{"custom", "chess960"} {
		if Lookup(game.StartFEN, []string{"e2e4"}, mode) != nil {
			t.Fatalf("false title for %s", mode)
		}
	}
	if Lookup("other", []string{"e2e4"}, "standard") != nil || Lookup(game.StartFEN, nil, "standard") != nil {
		t.Fatal("false no-match title")
	}
	b, err := NewBook(strings.NewReader("eco\tname\tuci\nA00\tFirst\te2e4\nB00\tLast\te2e4\n"))
	if err != nil {
		t.Fatal(err)
	}
	if e := b.Find([]string{"e2e4", "a7a6"}); e == nil || e.Title != "Last" {
		t.Fatalf("last-row precedence: %+v", e)
	}
	e := b.Find([]string{"e2e4"})
	e.Title = "modified"
	if b.Find([]string{"e2e4"}).Title != "Last" {
		t.Fatal("caller mutated immutable book")
	}
}

func TestPreparationRejectsMalformedSource(t *testing.T) {
	for _, source := range []string{"", "eco\tname\tpgn\tuci\tepd\nA00\tInvalid\t\te2e5\t\n", "eco\tname\tpgn\tuci\tepd\nX00\tInvalid\t\te2e4\t\n"} {
		if _, _, err := Prepare(strings.NewReader(source)); err == nil {
			t.Errorf("accepted invalid source: %q", source)
		}
	}
}

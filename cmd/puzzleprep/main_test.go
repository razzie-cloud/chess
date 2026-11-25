package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/razzie-cloud/chess/internal/puzzle"
)

func TestFailedCompilationPreservesOutputAndDrafts(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.json")
	output := filepath.Join(dir, "seed.json")
	report := filepath.Join(dir, "report.json")
	m := puzzle.Manifest{Version: 1, Collections: []puzzle.CollectionSource{{ID: "test-v1", Title: "Test", Entries: []puzzle.Source{{Slot: 0, FEN: "broken", Solver: "w", Mate: 2, Publish: true}}}}}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("prior reviewed output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"compile", "-input", input, "-output", output, "-report", report}); err == nil {
		t.Fatal("invalid selected entry compiled")
	}
	got, _ := os.ReadFile(output)
	if string(got) != "prior reviewed output" {
		t.Fatal("failed compile replaced output")
	}
	got, _ = os.ReadFile(input)
	if string(got) != string(data) {
		t.Fatal("failed compile rewrote source draft")
	}
	if _, err := os.Stat(report); err != nil {
		t.Fatal("failed compile did not write report")
	}
}

func TestAddCreatesDraftAndPreservesSlotAssignments(t *testing.T) {
	name := filepath.Join(t.TempDir(), "source.json")
	args := []string{"add", "-manifest", name, "-collection", "test-v1", "-slot", "255", "-mate", "2", "-fen", "7k/8/6K1/8/8/8/8/R7 w - - 0 1"}
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	var m puzzle.Manifest
	if err := readJSON(name, &m); err != nil {
		t.Fatal(err)
	}
	s := m.Collections[0].Entries[0]
	if s.Slot != 255 || s.Publish || s.Mate != 1 {
		t.Fatal("add did not calculate mate in one while preserving the draft slot")
	}
	if err := run(args); err == nil {
		t.Fatal("add reassigned an existing slot")
	}
}

func TestImportCalculatesMateLengthsWithoutExplicitDepth(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "positions.fen")
	manifest := filepath.Join(dir, "source.json")
	const fen = "r2qrb2/p1pn1Qp1/1p4Nk/4PR2/3n4/7N/P5PP/R6K w - - 1 1"
	if err := os.WriteFile(input, []byte(fen+"\n"+fen+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"import", "-manifest", manifest, "-input", input, "-collection", "test-v1", "-slot", "0"}); err != nil {
		t.Fatal(err)
	}
	var m puzzle.Manifest
	if err := readJSON(manifest, &m); err != nil {
		t.Fatal(err)
	}
	for i, src := range m.Collections[0].Entries {
		if src.Slot != i || src.Mate != 2 || src.Publish {
			t.Fatalf("imported entry did not retain a calculated mate-in-two draft: %+v", src)
		}
	}
}

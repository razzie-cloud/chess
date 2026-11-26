package puzzle

import (
	"encoding/json"
	"testing"

	"github.com/razzie-cloud/chess/internal/db"
)

func playDefinition(t *testing.T, slot int) Definition {
	t.Helper()
	var b Bundle
	if err := json.Unmarshal(db.PuzzleSeeds(), &b); err != nil {
		t.Fatal(err)
	}
	for _, d := range b.Collections[0].Puzzles {
		if d.Slot == slot {
			return d
		}
	}
	t.Fatal("missing fixture")
	return Definition{}
}
func cloneState(t *testing.T, s PlayState) PlayState {
	t.Helper()
	data, _ := json.Marshal(s)
	var c PlayState
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestVerifyPuzzleStateRejectsForgedAndOmittedBoards(t *testing.T) {
	d := playDefinition(t, 9)
	state, err := Advance(d, InitialState(d), Decision{Branch: "root", UCI: d.Root.Move})
	if err != nil || len(state.Boards) != 2 {
		t.Fatalf("fork: %+v %v", state, err)
	}
	cases := map[string]func(*PlayState){
		"omitted defense": func(s *PlayState) { s.Boards = s.Boards[:1] },
		"duplicate board": func(s *PlayState) { s.Boards[1] = s.Boards[0] },
		"forged board":    func(s *PlayState) { s.Boards[0].FEN = d.FEN },
		"forged mate":     func(s *PlayState) { s.Boards[0].Done = true },
		"forged history":  func(s *PlayState) { s.Boards[0].Path[0] = "a1a2" },
		"missing history": func(s *PlayState) { s.Moves = nil },
		"unknown branch":  func(s *PlayState) { s.Moves[0].Branch = "root-1" },
		"wrong move":      func(s *PlayState) { s.Moves[0].UCI = "a1a2" },
		"budget":          func(s *PlayState) { s.Boards[0].Remaining = 5 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			forged := cloneState(t, state)
			mutate(&forged)
			if _, err := VerifyState(d, forged); err == nil {
				t.Fatal("forged state accepted")
			}
		})
	}
	if _, err := VerifyState(d, state); err != nil {
		t.Fatal(err)
	}
}
func TestPuzzleAlternateImmediateMatesAndLockedBoards(t *testing.T) {
	d := playDefinition(t, 14)
	root, err := Advance(d, InitialState(d), Decision{Branch: "root", UCI: d.Root.Move})
	if err != nil {
		t.Fatal(err)
	}
	for _, uci := range []string{"d3g6", "h5g6"} {
		t.Run(uci, func(t *testing.T) {
			state, err := Advance(d, root, Decision{Branch: root.Boards[0].ID, UCI: uci})
			if err != nil {
				t.Fatal(err)
			}
			if !state.Boards[0].Done || state.Done() {
				t.Fatal("one board solved every defense")
			}
			if _, err := VerifyState(d, state); err != nil {
				t.Fatal(err)
			}
			if _, err := Advance(d, state, Decision{Branch: state.Boards[0].ID, UCI: uci}); err == nil {
				t.Fatal("solved board was movable")
			}
		})
	}
}
func TestEveryPublishedPuzzleStateReplaysToAllMates(t *testing.T) {
	var bundle Bundle
	if err := json.Unmarshal(db.PuzzleSeeds(), &bundle); err != nil {
		t.Fatal(err)
	}
	for _, d := range bundle.Collections[0].Puzzles {
		state := InitialState(d)
		for !state.Done() {
			var board Board
			for _, b := range state.Boards {
				if !b.Done {
					board = b
					break
				}
			}
			node, err := Lookup(d, board.Path)
			if err != nil {
				t.Fatal(err)
			}
			state, err = Advance(d, state, Decision{Branch: board.ID, UCI: node.Move})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = VerifyState(d, state); err != nil {
				t.Fatalf("slot %d: %v", d.Slot, err)
			}
		}
		if len(state.Boards) != d.Lines {
			t.Fatalf("slot %d: lost defense", d.Slot)
		}
	}
}

package store

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/game"
)

func TestChess960CreationUsesSelectedPosition(t *testing.T) {
	s := isolatedStore(t, 1)
	owner, request := uuid.NewString(), uuid.NewString()
	setup := Setup{Name: "Guest", Color: "b", Mode: "chess960", Chess960Index: "0", FEN: game.StartFEN}
	created, err := s.Create(t.Context(), owner, request, setup)
	if err != nil {
		t.Fatal(err)
	}
	fen, _ := game.Chess960(0)
	initial, err := game.New(fen, true)
	if err != nil {
		t.Fatal(err)
	}
	want := initial.InitialFEN
	snapshot := requireSnapshot(t, s, created.ID, Auth{Owner: owner})
	if snapshot.InitialFEN != want || snapshot.CurrentFEN != want || snapshot.Mode != "chess960" {
		t.Fatalf("selected position was replaced: %+v", snapshot)
	}
	retry, err := s.Create(t.Context(), owner, request, setup)
	if err != nil || !retry.Replay || retry.ID != created.ID {
		t.Fatalf("selection retry changed the match: %+v %v", retry, err)
	}
	setup.Chess960Index = "959"
	if _, err := s.Create(t.Context(), owner, request, setup); !errors.Is(err, ErrPayload) {
		t.Fatalf("changed selection replayed original receipt: %v", err)
	}
	for _, index := range []string{"-1", "960", "bad", "9999999999999999999999999999"} {
		setup.Chess960Index = index
		if _, err := s.Create(t.Context(), owner, uuid.NewString(), setup); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid index %q: %v", index, err)
		}
	}
	setup.Chess960Index = ""
	if _, err := s.Create(t.Context(), owner, uuid.NewString(), setup); err != nil {
		t.Fatalf("legacy random creation stopped working: %v", err)
	}
}

func TestChess960SelectionPreservesLegacyCreationDigests(t *testing.T) {
	setup := Setup{Name: "Guest", Color: "w", Mode: "chess960"}
	old := `{"Name":"Guest","CreatorName":"","Color":"w","Mode":"chess960","FEN":"","PGN":""}`
	if canonical(setup) != Digest(old) {
		t.Fatal("adding an absent position index invalidated older creation receipts")
	}
}

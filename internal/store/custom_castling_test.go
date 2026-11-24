package store

import (
	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/game"
	"strings"
	"testing"
)

func TestCustomChess960PersistsCastlingMode(t *testing.T) {
	s := isolatedStore(t, 1)
	owner := uuid.NewString()
	c, err := s.Create(t.Context(), owner, uuid.NewString(), Setup{Name: "Custom castling", Color: "w", Mode: "custom", FEN: "4k3/8/8/8/8/8/8/R3KR2 w FA - 0 1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Exchange(t.Context(), c.PlayingToken); err != nil {
		t.Fatal(err)
	}
	auth := Auth{Owner: owner}
	m := requireSnapshot(t, s, c.ID, auth)
	if m.Mode != "chess960" {
		t.Fatal("custom Chess960 mode was not persisted")
	}
	if _, _, err = s.Mutate(t.Context(), c.ID, auth, Mutation{RequestID: uuid.NewString(), Revision: m.Revision, Action: "move", Value: "e1g1"}); err == nil {
		t.Fatal("accepted noncanonical Chess960 castle")
	}
	if _, _, err = s.Mutate(t.Context(), c.ID, auth, Mutation{RequestID: uuid.NewString(), Revision: m.Revision, Action: "move", Value: "e1f1"}); err != nil {
		t.Fatal(err)
	}
	m = requireSnapshot(t, New(s.DB), c.ID, auth)
	g, err := game.Replay(m.InitialFEN, []string{m.Moves[0].UCI}, m.Mode == "chess960")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(game.ExportPGN(g), `[Variant "Chess960"]`) {
		t.Fatal("reconstructed export lost Chess960 variant")
	}
}

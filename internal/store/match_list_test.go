package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestListsIncludeLastKnownPosition(t *testing.T) {
	s := isolatedStore(t, 1)
	owner := uuid.NewString()
	for _, result := range []string{"*", "1-0"} {
		created, err := s.Create(t.Context(), owner, uuid.NewString(), Setup{Name: "Position fixture", Color: "b", Mode: "custom", PGN: "1. e4 e5 " + result})
		if err != nil {
			t.Fatal(err)
		}
		snapshot := requireSnapshot(t, s, created.ID, Auth{Owner: owner})
		for _, search := range []string{"", "Position"} {
			matches, err := s.List(t.Context(), owner, Filter{Archive: result != "*", Search: search})
			if err != nil || len(matches) != 1 {
				t.Fatalf("list %q: %+v %v", search, matches, err)
			}
			if matches[0].CurrentFEN != snapshot.CurrentFEN || matches[0].InitialFEN != snapshot.InitialFEN || matches[0].CreatorColor != snapshot.CreatorColor {
				t.Fatalf("list lost board state: %+v", matches[0])
			}
		}
	}
}

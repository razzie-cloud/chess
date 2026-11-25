package app

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/internal/store"
	"github.com/razzie-cloud/chess/internal/web"
)

func TestMatchTurnStatusFollowsPositionAndAvailability(t *testing.T) {
	afterWhite, err := game.Replay(game.StartFEN, []string{"e2e4"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, status, color, fen, want string
		ownTurn                        bool
	}{
		{"white owner", "active", "w", game.StartFEN, "White to move", true},
		{"black opponent", "active", "w", afterWhite.Board.Fen(), "Black to move", false},
		{"black guest", "active", "b", afterWhite.Board.Fen(), "Black to move", true},
		{"viewer", "active", "", game.StartFEN, "White to move", false},
		{"waiting", "pending", "w", game.StartFEN, "Waiting for opponent", false},
		{"finished", "completed", "w", game.StartFEN, "1-0 · resignation", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{}
			page := server.matchData(httptest.NewRequest("GET", "https://chess.example/matches/test", nil), store.Snapshot{
				Match: store.Match{ID: "test", CurrentFEN: tc.fen, InitialFEN: game.StartFEN, Mode: "standard", Status: tc.status, Result: "1-0", Reason: "resignation"}, Color: tc.color,
			})
			var body bytes.Buffer
			if err := web.Render(&body, "state", page); err != nil {
				t.Fatal(err)
			}
			got := body.String()
			if !strings.Contains(got, tc.want) || strings.Contains(got, "Your turn") != tc.ownTurn {
				t.Fatalf("incorrect turn status: %s", got)
			}
			active := tc.status == "active"
			if strings.Contains(got, " to move") != active || strings.Contains(got, `class="turn-indicator"`) != active {
				t.Fatalf("move cue does not reflect availability: %s", got)
			}
			if active {
				piece := "wN"
				if strings.HasPrefix(tc.want, "Black") {
					piece = "bN"
				}
				if !strings.Contains(got, "/static/pieces/"+piece+".png") {
					t.Fatal("turn icon has wrong color")
				}
			}
		})
	}
}

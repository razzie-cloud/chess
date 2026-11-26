package app

import (
	"net/url"
	"strings"
	"testing"

	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/internal/store"
)

func TestSetupSourceKeepsInactiveDraftOutOfImport(t *testing.T) {
	for _, source := range []string{"", "board", "fen", "pgn"} {
		r := formRequest("POST", "https://chess.example/create/validate", "https://chess.example", url.Values{"source": {source}})
		setup := store.Setup{Mode: "custom", FEN: game.StartFEN, PGN: "1. e4 e5 *"}
		input := selectedSetup(r, setup)
		wantPGN := source == "" || source == "pgn"
		if (input.PGN != "") != wantPGN || input.FEN != setup.FEN || setup.PGN == "" {
			t.Fatalf("source %q selected wrong input or discarded draft: %+v", source, input)
		}
	}
}

func TestSetupValidationUsesSelectedSourceAndRetainsDrafts(t *testing.T) {
	f := newHTTPFixture(t, nil)
	for _, test := range []struct {
		name, source, fen, pgn, wantFEN string
		status                          int
	}{
		{"FEN ignores PGN draft", "fen", game.StartFEN, "bad PGN draft", game.StartFEN, 200},
		{"board ignores PGN draft", "board", game.StartFEN, "bad PGN draft", game.StartFEN, 200},
		{"invalid FEN stays editable", "fen", "bad FEN draft", "1. e4 *", "bad FEN draft", 422},
		{"invalid PGN stays editable", "pgn", game.StartFEN, "bad PGN draft", game.StartFEN, 422},
		{"empty PGN does not use FEN", "pgn", game.StartFEN, "", game.StartFEN, 422},
		{"PGN replays final position", "pgn", "bad FEN draft", "1. e4 e5 *", "rnbqkbnr/pppp1ppp/8/4p3/4P3/8/PPPP1PPP/RNBQKBNR w KQkq e6 0 2", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := formRequest("POST", f.cfg.Origin+"/create/validate", f.cfg.Origin, url.Values{
				"source": {test.source}, "mode": {"custom"}, "color": {"b"}, "name": {"Alex & friend"}, "fen": {test.fen}, "pgn": {test.pgn},
			})
			f.sign(r, f.owner, false)
			w := response(f.handler, r)
			body := w.Body.String()
			if w.Code != test.status || !strings.Contains(body, test.wantFEN) || !strings.Contains(body, test.pgn) || !strings.Contains(body, `value="Alex &amp; friend"`) || !strings.Contains(body, `value="`+test.source+`" checked`) || !strings.Contains(body, `value="b" checked`) {
				t.Fatalf("validation lost setup: %d %s", w.Code, body)
			}
		})
	}
}

func TestCreateEmptySelectedPGNDoesNotFallBackToFEN(t *testing.T) {
	f := newHTTPFixture(t, nil)
	r := formRequest("POST", f.cfg.Origin+"/matches", f.cfg.Origin, url.Values{
		"source": {"pgn"}, "mode": {"custom"}, "color": {"b"}, "name": {"Alex"}, "fen": {game.StartFEN},
	})
	f.sign(r, f.owner, false)
	w := response(f.handler, r)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Paste one PGN game") || !strings.Contains(w.Body.String(), `value="pgn" checked`) {
		t.Fatalf("empty PGN created a FEN match or lost its source: %d %s", w.Code, w.Body.String())
	}
}

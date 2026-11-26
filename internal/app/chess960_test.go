package app

import (
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/razzie-cloud/chess/internal/game"
)

func chess960Selection(t *testing.T, body string) (string, string) {
	t.Helper()
	index := regexp.MustCompile(`name="chess960_index" value="([0-9]+)"`).FindStringSubmatch(body)
	fen := regexp.MustCompile(`id="chess960-position" data-fen="([^"]+)"`).FindStringSubmatch(body)
	if len(index) != 2 || len(fen) != 2 {
		t.Fatalf("missing Chess960 selection: %s", body)
	}
	n, err := strconv.Atoi(index[1])
	if err != nil {
		t.Fatal(err)
	}
	want, err := game.Chess960(n)
	if err != nil || want != fen[1] {
		t.Fatalf("invalid Chess960 selection %q: %s (%v)", index[1], fen[1], err)
	}
	return index[1], fen[1]
}

func TestChess960RefreshReturnsDifferentValidPosition(t *testing.T) {
	f := newHTTPFixture(t, nil)
	r := httptest.NewRequest("GET", f.cfg.Origin+"/matches/new?mode=chess960", nil)
	f.sign(r, f.owner, false)
	w := response(f.handler, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `id="refresh-chess960"`) {
		t.Fatalf("missing refresh control: %d %s", w.Code, w.Body.String())
	}
	previous, _ := chess960Selection(t, w.Body.String())
	for range 12 {
		r = httptest.NewRequest("GET", f.cfg.Origin+"/matches/new/chess960?chess960_index="+previous, nil)
		r.Header.Set("HX-Request", "true")
		f.sign(r, f.owner, false)
		w = response(f.handler, r)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "<html") || strings.Contains(w.Body.String(), "<form") {
			t.Fatalf("refresh did not return an uncached position fragment: %d %s", w.Code, w.Body.String())
		}
		next, _ := chess960Selection(t, w.Body.String())
		if next == previous {
			t.Fatal("refresh repeated the current layout")
		}
		previous = next
	}
	for _, target := range []string{f.cfg.Origin, f.cfg.ShareOrigin} {
		r = httptest.NewRequest("GET", target+"/matches/new/chess960", nil)
		w = response(f.handler, r)
		want := 401
		if target == f.cfg.ShareOrigin {
			want = 404
		}
		if w.Code != want {
			t.Fatalf("unprotected Chess960 refresh on %s: %d", target, w.Code)
		}
	}
}

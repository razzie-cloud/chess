package app

import (
	"encoding/json"
	"html"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/puzzle"
	"github.com/razzie-cloud/chess/internal/store"
)

func TestPuzzleCollectionsContainNumbersPreviewsAndImmediatePlay(t *testing.T) {
	s := httpStore(t)
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := newHTTPFixture(t, s)
	for _, path := range []string{"/puzzles", "/puzzles/razchess-mates-v1"} {
		r := httptest.NewRequest("GET", f.cfg.Origin+path, nil)
		f.sign(r, f.owner, false)
		w := response(f.handler, r)
		links := regexp.MustCompile(`href="/puzzles/razchess-mates-v1/(\d+)" aria-label="Mate challenge (\d+) · Undone" data-preview-fen="[^"]+" data-preview-color="[wb]">(\d+)</a>`).FindAllStringSubmatch(w.Body.String(), -1)
		if w.Code != 200 || len(links) != 206 {
			t.Fatalf("collection status %d, %d puzzles", w.Code, len(links))
		}
		for i, link := range links {
			if link[1] != strconv.Itoa(i) || link[2] != strconv.Itoa(i+1) || link[3] != strconv.Itoa(i+1) {
				t.Fatal("nonconsecutive numbering")
			}
		}
	}
	r := httptest.NewRequest("GET", f.cfg.Origin+"/puzzles/razchess-mates-v1/50", nil)
	f.sign(r, f.owner, false)
	w := response(f.handler, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<h1>Mate challenge 51</h1>") || !strings.Contains(w.Body.String(), "Mate in 2, playing Black") || !strings.Contains(w.Body.String(), `class="puzzle-move sidebar-section"`) || strings.Contains(w.Body.String(), "/attempts") || strings.Contains(w.Body.String(), "puzzle-menu") {
		t.Fatal("puzzle did not open ready to play")
	}
}

func puzzleResponseState(t *testing.T, body string) puzzle.PlayState {
	t.Helper()
	match := regexp.MustCompile(`id="puzzle-state" data-state="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("missing complete puzzle state")
	}
	var state puzzle.PlayState
	if err := json.Unmarshal([]byte(html.UnescapeString(match[1])), &state); err != nil {
		t.Fatal(err)
	}
	return state
}
func TestPuzzleSignedFullStateAcrossReplicasAndPrivateProgress(t *testing.T) {
	s := httpStore(t)
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := newHTTPFixture(t, s)
	second := newHTTPFixture(t, store.New(s.DB))
	second.owner = f.owner
	collection := "razchess-mates-v1"
	d, err := s.Definition(t.Context(), collection, 9)
	if err != nil {
		t.Fatal(err)
	}
	state := puzzle.InitialState(d)
	target := "/puzzles/" + collection + "/9/moves"
	var last store.PuzzleMove
	for !state.Done() {
		var branch puzzle.Board
		for _, b := range state.Boards {
			if !b.Done {
				branch = b
				break
			}
		}
		node, _ := puzzle.Lookup(d, branch.Path)
		in := store.PuzzleMove{ContentDigest: d.Digest, State: state, Decision: puzzle.Decision{Branch: branch.ID, UCI: node.Move}}
		body, _ := json.Marshal(in)
		replica := f
		if len(state.Moves)%2 == 1 {
			replica = second
		}
		r := httptest.NewRequest("POST", replica.cfg.Origin+target, strings.NewReader(string(body)))
		r.Header.Set("Origin", replica.cfg.Origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("HX-Request", "true")
		replica.sign(r, f.owner, false)
		w := response(replica.handler, r)
		if w.Code != 200 || strings.Contains(w.Body.String(), `"replies"`) || strings.Contains(w.Body.String(), `"root":`) {
			t.Fatalf("move response: %d %s", w.Code, w.Body.String())
		}
		state = puzzleResponseState(t, w.Body.String())
		last = in
	}
	// Final retry carries its complete prior state and stays idempotent on another replica.
	body, _ := json.Marshal(last)
	r := httptest.NewRequest("POST", second.cfg.Origin+target, strings.NewReader(string(body)))
	r.Header.Set("Origin", second.cfg.Origin)
	second.sign(r, f.owner, false)
	w := response(second.handler, r)
	if w.Code != 200 || !puzzleResponseState(t, w.Body.String()).Done() {
		t.Fatalf("final retry: %d", w.Code)
	}
	cards, err := s.Collections(t.Context(), uuid.NewString())
	if err != nil || cards[0].Solved != 0 {
		t.Fatal("completion crossed owners")
	}
	for _, path := range []string{"/puzzles", "/puzzles/" + collection + "/9", target} {
		r := httptest.NewRequest("GET", f.cfg.ShareOrigin+path, nil)
		f.sign(r, f.owner, true)
		if w := response(f.handler, r); w.Code != 404 {
			t.Fatal("personal puzzle reachable through public surface")
		}
	}
	// Forged completion and omitted defenses are rejected at the signed HTTP boundary.
	forged := last
	forged.State.Boards = forged.State.Boards[:1]
	body, _ = json.Marshal(forged)
	r = httptest.NewRequest("POST", f.cfg.Origin+target, strings.NewReader(string(body)))
	r.Header.Set("Origin", f.cfg.Origin)
	f.sign(r, f.owner, false)
	if w := response(f.handler, r); w.Code != 422 {
		t.Fatalf("forged state: %d", w.Code)
	}
	for _, body := range []string{`{}`, `{"state":{},"unknown":true}`, `{} {}`} {
		r = httptest.NewRequest("POST", f.cfg.Origin+target, strings.NewReader(body))
		r.Header.Set("Origin", f.cfg.Origin)
		f.sign(r, f.owner, false)
		if w := response(f.handler, r); w.Code != 422 && w.Code != 409 {
			t.Fatalf("invalid JSON accepted: %d", w.Code)
		}
	}
}

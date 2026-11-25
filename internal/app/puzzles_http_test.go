package app

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/puzzle"
	"github.com/razzie-cloud/chess/internal/store"
)

func TestPuzzleNumbersAndIDsAreSequential(t *testing.T) {
	s := httpStore(t)
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := newHTTPFixture(t, s)
	r := httptest.NewRequest("GET", f.cfg.Origin+"/puzzles/razchess-mates-v1", nil)
	f.sign(r, f.owner, false)
	w := response(f.handler, r)
	links := regexp.MustCompile(`<a class="" href="/puzzles/razchess-mates-v1/(\d+)" title="([^"]+)">(\d+)</a>`).FindAllStringSubmatch(w.Body.String(), -1)
	if w.Code != 200 || len(links) != 206 {
		t.Fatalf("puzzle collection: status %d, %d links", w.Code, len(links))
	}
	for i, link := range links {
		number := strconv.Itoa(i + 1)
		if link[1] != strconv.Itoa(i) || link[3] != number || link[2] != "Mate challenge "+number {
			t.Fatalf("puzzle %s displayed as %q, %q", number, link[2], link[3])
		}
	}
	r = httptest.NewRequest("GET", f.cfg.Origin+"/puzzles/razchess-mates-v1/50", nil)
	f.sign(r, f.owner, false)
	w = response(f.handler, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<h1>Mate challenge 51</h1>") || !strings.Contains(w.Body.String(), "Mate in 2, playing Black") {
		t.Fatal("detail heading does not match the collection number")
	}
}

func TestPuzzleSignedHTTPAlternatesReplicasAndKeepsSolutionsPrivate(t *testing.T) {
	s := httpStore(t)
	if err := s.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := newHTTPFixture(t, s)
	second := newHTTPFixture(t, store.New(s.DB))
	second.owner = f.owner
	start := formRequest("POST", f.cfg.Origin+"/puzzles/razchess-mates-v1/9/attempts", f.cfg.Origin, url.Values{"request_id": {uuid.NewString()}})
	f.sign(start, f.owner, false)
	created := response(f.handler, start)
	if created.Code != 303 {
		t.Fatalf("native start: %d %s", created.Code, created.Body.String())
	}
	id := strings.TrimPrefix(created.Header().Get("Location"), "/puzzle-attempts/")
	a, err := s.Attempt(t.Context(), id, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Definition(t.Context(), a.CollectionID, a.Slot)
	if err != nil {
		t.Fatal(err)
	}
	var original url.Values
	var originalTarget string
	for turn := 0; turn < 10 && a.Status == "active"; turn++ {
		var branch store.PuzzleBranch
		for _, b := range a.Branches {
			if b.Status == "pending" {
				branch = b
				break
			}
		}
		node, err := puzzle.Lookup(d, branch.Path)
		if err != nil {
			t.Fatal(err)
		}
		path, _ := json.Marshal(branch.Path)
		if branch.Path == nil {
			path = []byte("[]")
		}
		values := url.Values{"content_digest": {a.Digest}, "revision": {strconv.FormatInt(a.Revision, 10)}, "path": {string(path)}, "uci": {node.Move}}
		target := "/puzzle-attempts/" + id + "/branches/" + branch.ID + "/moves"
		if turn == 0 {
			original = values
			originalTarget = target
		}
		replica := f
		if turn%2 == 1 {
			replica = second
		}
		r := formRequest("POST", replica.cfg.Origin+target, replica.cfg.Origin, values)
		replica.sign(r, f.owner, false)
		r.Header.Set("HX-Request", "true")
		w := response(replica.handler, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `id="attempt-state"`) || strings.Contains(w.Body.String(), `"replies"`) || strings.Contains(w.Body.String(), `"root":`) {
			t.Fatalf("mutation response lost state or leaked solution: %d", w.Code)
		}
		a, err = s.Attempt(t.Context(), id, f.owner)
		if err != nil {
			t.Fatal(err)
		}
	}
	if a.Status != "solved" {
		t.Fatal("alternating replicas lost branching completion")
	}
	// A retired root transition replays successfully after final completion on another handler.
	r := formRequest("POST", second.cfg.Origin+originalTarget, second.cfg.Origin, original)
	second.sign(r, f.owner, false)
	r.Header.Set("HX-Request", "true")
	if w := response(second.handler, r); w.Code != 200 || !strings.Contains(w.Body.String(), "Puzzle solved") {
		t.Fatalf("completed transition retry: %d", w.Code)
	}
	for _, path := range []string{"/puzzle-attempts/" + id, "/puzzle-attempts/" + id + "/state", "/puzzle-attempts/" + id + "/branches/root-1/export/pgn"} {
		r := httptest.NewRequest("GET", f.cfg.Origin+path, nil)
		f.sign(r, uuid.NewString(), true)
		if w := response(f.handler, r); w.Code != 404 {
			t.Fatalf("administrator crossed puzzle ownership: %d", w.Code)
		}
		r = httptest.NewRequest("GET", f.cfg.ShareOrigin+path, nil)
		f.sign(r, f.owner, true)
		if w := response(f.handler, r); w.Code != 404 {
			t.Fatal("personal puzzle reachable through public surface")
		}
	}
	for _, branch := range []string{"root", "root-1", "root-2"} {
		r := httptest.NewRequest("GET", f.cfg.Origin+"/puzzle-attempts/"+id+"/branches/"+branch+"/export/pgn", nil)
		second.sign(r, f.owner, false)
		w := response(second.handler, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `[SetUp "1"]`) || strings.Contains(w.Body.String(), `replies`) {
			t.Fatalf("played branch export failed: %d", w.Code)
		}
	}
}

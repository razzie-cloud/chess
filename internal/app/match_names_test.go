package app

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/store"
)

func TestMatchNamesUseVerifiedCreatorAndPublicOpponents(t *testing.T) {
	storage := httpStore(t)
	f := newHTTPFixture(t, storage)
	r := formRequest("POST", f.cfg.Origin+"/matches", f.cfg.Origin, url.Values{
		"request_id": {uuid.NewString()}, "name": {"test"}, "color": {"w"}, "mode": {"standard"}, "creator_name": {"forged host"},
	})
	f.sign(r, f.owner, false)
	w := response(f.handler, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<h1>alice vs test</h1>") {
		t.Fatalf("verified creator name missing: %d %s", w.Code, w.Body.String())
	}
	matches, err := storage.List(t.Context(), f.owner, store.Filter{})
	if err != nil || len(matches) != 1 || matches[0].CreatorName != "alice" {
		t.Fatalf("creator name was not stored: %+v %v", matches, err)
	}
	m, err := storage.Snapshot(t.Context(), matches[0].ID, store.Auth{Owner: f.owner})
	if err != nil {
		t.Fatal(err)
	}
	// A public viewing capability must use the persisted name, ignoring headers.
	r = httptest.NewRequest("GET", f.cfg.ShareOrigin+"/watch/"+m.ViewToken, nil)
	r.Header.Set("X-Auth-Request-Name", "forged host")
	w = response(f.handler, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<h1>alice vs test</h1>") || strings.Contains(w.Body.String(), "forged host") {
		t.Fatalf("public opponents incorrect: %d %s", w.Code, w.Body.String())
	}
}

func TestLegacyMatchNameFilledOnlyByVerifiedOwner(t *testing.T) {
	storage := httpStore(t)
	f := newHTTPFixture(t, storage)
	created, err := storage.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "test", Color: "w", Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	id, session, err := storage.Exchange(t.Context(), created.PlayingToken)
	if err != nil {
		t.Fatal(err)
	}
	guest := func() string {
		r := httptest.NewRequest("GET", f.cfg.ShareOrigin+"/play/"+id, nil)
		r.Header.Set("Cookie", cookieName(id)+"="+session)
		r.Header.Set("X-Auth-Request-Name", "forged host")
		w := response(f.handler, r)
		if w.Code != 200 {
			t.Fatalf("guest match: %d", w.Code)
		}
		return w.Body.String()
	}
	if body := guest(); !strings.Contains(body, "<h1>Host vs test</h1>") || strings.Contains(body, "forged host") {
		t.Fatal("legacy guest matched against itself or trusted unsigned name")
	}
	r := httptest.NewRequest("GET", f.cfg.Origin+"/matches/"+id, nil)
	f.sign(r, f.owner, false)
	w := response(f.handler, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<h1>alice vs test</h1>") {
		t.Fatal("owner read did not fill legacy name")
	}
	if !strings.Contains(guest(), "<h1>alice vs test</h1>") {
		t.Fatal("guest did not receive stored owner name")
	}
	m, err := storage.Snapshot(t.Context(), id, store.Auth{Owner: f.owner})
	if err != nil || m.CreatorName != "alice" || m.Revision != 1 {
		t.Fatalf("name backfill changed game revision: %+v %v", m, err)
	}
}

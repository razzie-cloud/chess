package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/bot"
	"github.com/razzie-cloud/chess/internal/gatewayidentity"
	"github.com/razzie-cloud/chess/internal/health"
	"github.com/razzie-cloud/chess/internal/store"
)

func enableBot(t *testing.T, f *httpFixture, storage *store.Store) {
	t.Helper()
	f.cfg.BotURL = "http://stockfish:8080/jobs"
	v, err := gatewayidentity.New(t.Context(), f.cfg.AccessGatewayURL, f.cfg.Origin)
	if err != nil {
		t.Fatal(err)
	}
	f.handler, err = New(f.cfg, storage, v, health.New())
	if err != nil {
		t.Fatal(err)
	}
}

func TestBotSetupAndDisabledService(t *testing.T) {
	f := newHTTPFixture(t, nil)
	r := formRequest("POST", f.cfg.Origin+"/matches", f.cfg.Origin, url.Values{"opponent": {"bot"}, "bot_level": {"3"}, "mode": {"standard"}, "color": {"b"}, "request_id": {uuid.NewString()}})
	f.sign(r, f.owner, false)
	w := response(f.handler, r)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Bot play is currently unavailable") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/bot/moves", "/jobs"} {
		for _, origin := range []string{f.cfg.Origin, f.cfg.ShareOrigin} {
			r := formRequest("POST", origin+path, origin, nil)
			f.sign(r, f.owner, true)
			if w := response(f.handler, r); w.Code != 404 {
				t.Fatalf("private bot route exposed: %s%s %d", origin, path, w.Code)
			}
		}
	}
	enableBot(t, f, nil)
	for _, path := range []string{"/matches/new", "/matches/new?mode=chess960", "/create"} {
		r := httptest.NewRequest("GET", f.cfg.Origin+path, nil)
		f.sign(r, f.owner, false)
		w := response(f.handler, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "5 · Expert") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
}

func TestBotCallbackAuthentication(t *testing.T) {
	h := BotHandler(nil, strings.Repeat("x", 32))
	for _, tc := range []struct {
		auth, body string
		status     int
	}{
		{"", `{}`, 401},
		{"Bearer " + strings.Repeat("x", 32), `{"id":"bad","uci":"e2e4","extra":true}`, 400},
		{"Bearer " + strings.Repeat("x", 32), `{} {}`, 400},
	} {
		r := httptest.NewRequest("POST", "http://chess:8082/bot/moves", strings.NewReader(tc.body))
		r.Header.Set("Authorization", tc.auth)
		if w := response(h, r); w.Code != tc.status {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
}

func TestBotEndToEndWithStockfish(t *testing.T) {
	binary := os.Getenv("STOCKFISH_TEST_PATH")
	if binary == "" {
		t.Skip("set STOCKFISH_TEST_PATH for real engine flow")
	}
	storage := httpStore(t)
	owner := uuid.NewString()
	created, err := storage.Create(t.Context(), owner, uuid.NewString(), store.Setup{Opponent: "bot", BotLevel: 2, Color: "b", Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("x", 32)
	callback := httptest.NewServer(BotHandler(storage, token))
	defer callback.Close()
	w := bot.NewWorker(token, callback.URL+"/bot/moves", binary, 2)
	engine := httptest.NewServer(w.Handler())
	defer engine.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	workerDone, dispatchDone := make(chan struct{}), make(chan struct{})
	go func() { defer close(workerDone); w.Run(ctx) }()
	go func() {
		defer close(dispatchDone)
		DispatchBots(ctx, storage, engine.URL+"/jobs", callback.URL+"/bot/moves", token)
	}()
	defer func() { cancel(); <-workerDone; <-dispatchDone }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m, err := storage.Snapshot(t.Context(), created.ID, store.Auth{Owner: owner})
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Moves) == 1 {
			if m.Moves[0].Actor != "bot" || m.Revision != 1 {
				t.Fatal(m)
			}
			if _, err = storage.ClaimBotJob(t.Context()); err != sql.ErrNoRows {
				t.Fatalf("job not acknowledged: %v", err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("real Stockfish move never reached Chess")
}

func TestBotResultDoesNotRedirect(t *testing.T) {
	// A redirect must never forward the shared credential to another service.
	var leaked bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer redirect.Close()
	payload, _ := json.Marshal(bot.Result{ID: uuid.NewString(), UCI: "e2e4"})
	r, _ := http.NewRequest("POST", redirect.URL, bytes.NewReader(payload))
	r.Header.Set("Authorization", "Bearer secret")
	client := bot.Client()
	defer client.CloseIdleConnections()
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 307 || leaked {
		t.Fatal("followed webhook redirect")
	}
}

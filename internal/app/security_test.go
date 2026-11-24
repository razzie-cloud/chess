package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/razzie-cloud/chess/internal/config"
	"github.com/razzie-cloud/chess/internal/db"
	"github.com/razzie-cloud/chess/internal/gatewayidentity"
	"github.com/razzie-cloud/chess/internal/health"
	"github.com/razzie-cloud/chess/internal/store"
)

type httpFixture struct {
	handler http.Handler
	key     paseto.V4AsymmetricSecretKey
	kid     string
	owner   string
	cfg     config.Config
}

func newHTTPFixture(t *testing.T, storage *store.Store) *httpFixture {
	t.Helper()
	f := &httpFixture{key: paseto.NewV4AsymmetricSecretKey(), owner: uuid.NewString(), cfg: config.Config{
		Origin: "https://chess.example", ShareOrigin: "https://shared-chess.example", LandingOrigin: "https://home.example",
	}}
	sum := sha256.Sum256(f.key.Public().ExportBytes())
	f.kid = hex.EncodeToString(sum[:16])
	now := time.Now().UTC().Truncate(time.Second)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/paseto-keys" {
			t.Error("unexpected key discovery route")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": "https://auth.example", "expires_at": now.Add(5 * time.Minute),
			"keys": []map[string]any{{"kid": f.kid, "public_key": f.key.Public().ExportHex(), "not_before": now.Add(-time.Hour), "expires_at": now.Add(time.Hour)}},
		})
	}))
	t.Cleanup(gateway.Close)
	f.cfg.AccessGatewayURL = gateway.URL
	verifier, err := gatewayidentity.New(t.Context(), gateway.URL, f.cfg.Origin)
	if err != nil {
		t.Fatal(err)
	}
	f.handler, err = New(f.cfg, storage, verifier, health.New())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *httpFixture) sign(r *http.Request, user string, admin bool) {
	now := time.Now().UTC().Truncate(time.Second)
	token := paseto.NewToken()
	token.SetIssuer("https://auth.example")
	token.SetAudience(f.cfg.Origin)
	token.SetSubject(user)
	token.SetIssuedAt(now)
	token.SetNotBefore(now)
	token.SetExpiration(now.Add(time.Minute))
	token.SetString("name", "alice")
	token.SetString("method", r.Method)
	token.SetString("uri", r.URL.RequestURI())
	_ = token.Set("admin", admin)
	token.SetFooter([]byte(f.kid))
	r.Header.Set(gatewayidentity.Header, token.V4Sign(f.key, nil))
	r.Header.Set("X-Auth-Request-User", user)
	r.Header.Set("X-Auth-Request-Name", "alice")
}

func response(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	return w
}

func TestHostIdentityOriginAndCSPBoundaries(t *testing.T) {
	f := newHTTPFixture(t, nil)
	if _, err := New(f.cfg, nil, nil, nil); err == nil {
		t.Fatal("unsigned proxy fallback accepted")
	}
	for _, target := range []string{"https://shared-chess.example/matches/new", "https://shared-chess.example/puzzles", "https://shared-chess.example/create", "https://evil.example/g", "https://shared-chess.example/healthz"} {
		r := httptest.NewRequest("GET", target, nil)
		f.sign(r, f.owner, true)
		if w := response(f.handler, r); w.Code != 404 {
			t.Fatalf("cross-surface route %s: %d", target, w.Code)
		}
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) {},
		func(r *http.Request) {
			r.Header.Set("X-Auth-Request-User", f.owner)
			r.Header.Set("X-Auth-Request-Name", "alice")
		},
		func(r *http.Request) { f.sign(r, f.owner, false); r.Header.Add("X-Auth-Request-User", f.owner) },
		func(r *http.Request) { f.sign(r, f.owner, false); r.URL.RawQuery = "tampered=1" },
	} {
		r := httptest.NewRequest("GET", f.cfg.Origin+"/matches/new", nil)
		mutate(r)
		w := response(f.handler, r)
		if w.Code != 401 || w.Header().Get("HX-Refresh") != "true" {
			t.Fatalf("spoofed identity accepted: %d", w.Code)
		}
	}
	for _, origin := range []string{"", "null", "https://evil.example", f.cfg.Origin} {
		r := httptest.NewRequest("POST", f.cfg.ShareOrigin+"/guest-sessions", strings.NewReader("token=invalid"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if w := response(f.handler, r); w.Code != 403 {
			t.Fatalf("cross-origin exchange accepted for %q: %d", origin, w.Code)
		}
	}
	r := httptest.NewRequest("POST", f.cfg.ShareOrigin+"/guest-sessions", strings.NewReader("token=invalid"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Add("Origin", f.cfg.ShareOrigin)
	r.Header.Add("Origin", f.cfg.ShareOrigin)
	if w := response(f.handler, r); w.Code != 403 {
		t.Fatal("duplicate Origin accepted")
	}
	r = httptest.NewRequest("POST", f.cfg.ShareOrigin+"/guest-sessions", strings.NewReader("token="+strings.Repeat("x", 301<<10)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", f.cfg.ShareOrigin)
	if w := response(f.handler, r); w.Code != 422 {
		t.Fatalf("oversized body accepted: %d", w.Code)
	}
	r = httptest.NewRequest("GET", f.cfg.ShareOrigin+"/g", nil)
	f.sign(r, f.owner, true)
	w := response(f.handler, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "alice") || strings.Contains(w.Body.String(), "Administrator") || !strings.Contains(w.Body.String(), "Continue as opponent") {
		t.Fatalf("public bootstrap disclosed identity or failed: %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatal("capability page cache/referrer policy missing")
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "form-action 'self'") || strings.Contains(csp, "script-src 'self' 'unsafe") {
		t.Fatalf("unsafe script policy: %s", csp)
	}
	probe := response(f.handler, httptest.NewRequest("HEAD", "http://127.0.0.1:8080/healthz", nil))
	if probe.Code != 200 || probe.Body.Len() != 0 {
		t.Fatal("private HEAD liveness unavailable")
	}
}

func httpStore(t *testing.T) *store.Store {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL unset; disposable PostgreSQL HTTP integration test")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("TEST_DATABASE_URL must use disposable loopback PostgreSQL")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	schema := "chess_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	database, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	return store.New(database)
}

func formRequest(method, target, origin string, values url.Values) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", origin)
	return r
}

func TestGuestExchangeHTTPScopesAndReplicaRestart(t *testing.T) {
	storage := httpStore(t)
	f := newHTTPFixture(t, storage)
	created, err := storage.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "<img src=x onerror=alert(1)>", Color: "w", Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", f.cfg.ShareOrigin+"/g", nil)
	if w := response(f.handler, r); w.Code != 200 {
		t.Fatal("bootstrap preview failed")
	}
	pending, err := storage.Snapshot(t.Context(), created.ID, store.Auth{Owner: f.owner})
	if err != nil || pending.Status != "pending" {
		t.Fatal("preview activated match")
	}
	r = formRequest("POST", f.cfg.ShareOrigin+"/guest-sessions", f.cfg.ShareOrigin, url.Values{"token": {created.PlayingToken}})
	r.Header.Set("Accept", "application/json")
	w := response(f.handler, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), created.PlayingToken) {
		t.Fatalf("exchange failed or leaked playing token: %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != cookieName(created.ID) || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].Domain != "" || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 30*24*3600 {
		t.Fatal("guest cookie is not match-scoped/host-only/protected")
	}
	guestCookie := cookies[0]
	state, err := storage.Snapshot(t.Context(), created.ID, store.Auth{Session: guestCookie.Value})
	if err != nil || state.Status != "active" {
		t.Fatal("explicit exchange did not activate match")
	}
	for _, path := range []string{"/play/" + created.ID, "/play/" + created.ID + "/export/pgn"} {
		r := httptest.NewRequest("GET", f.cfg.ShareOrigin+path, nil)
		f.sign(r, f.owner, true)
		if w := response(f.handler, r); w.Code != 404 {
			t.Fatalf("UUID or signed headers authorized public read: %d", w.Code)
		}
		r.AddCookie(guestCookie)
		if w := response(f.handler, r); w.Code != 200 || strings.Contains(w.Body.String(), "<img src=x onerror=") {
			t.Fatalf("guest read/escaping failed: %d", w.Code)
		}
	}
	move := url.Values{"request_id": {uuid.NewString()}, "revision": {strconv.FormatInt(state.Revision, 10)}, "uci": {"e2e4"}}
	r = formRequest("POST", f.cfg.Origin+"/matches/"+created.ID+"/moves", f.cfg.Origin, move)
	f.sign(r, f.owner, false)
	r.Header.Set("HX-Request", "true")
	if w := response(f.handler, r); w.Code != 200 || !strings.Contains(w.Body.String(), "e4") {
		t.Fatalf("signed creator move failed: %d %s", w.Code, w.Body.String())
	}
	// Construct an independent handler/verifier to model another replica/restart.
	restarted := newHTTPFixture(t, store.New(storage.DB))
	restarted.owner = f.owner
	r = httptest.NewRequest("GET", restarted.cfg.ShareOrigin+"/play/"+created.ID+"/state", nil)
	r.AddCookie(guestCookie)
	w = response(restarted.handler, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "e2e4") || !strings.Contains(w.Body.String(), "e4") {
		t.Fatalf("replica lost moves or opening: %d", w.Code)
	}
	r = httptest.NewRequest("GET", f.cfg.ShareOrigin+"/watch/"+created.ViewToken+"/export/pgn", nil)
	w = response(restarted.handler, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "1. e4") {
		t.Fatalf("viewer export lost persisted history: %d", w.Code)
	}
	r = httptest.NewRequest("GET", f.cfg.Origin+"/matches/"+created.ID, nil)
	f.sign(r, uuid.NewString(), true)
	if w := response(f.handler, r); w.Code != 404 {
		t.Fatal("cloud admin crossed match ownership boundary")
	}
	// An unchanged, fully authorized poll is quiet and leaves the mounted board.
	latest, err := storage.Snapshot(t.Context(), created.ID, store.Auth{Owner: f.owner})
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", f.cfg.ShareOrigin+"/play/"+created.ID+"/state?revision="+strconv.FormatInt(latest.Revision, 10)+"&viewers=0", nil)
	r.AddCookie(guestCookie)
	w = response(f.handler, r)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("unchanged poll returned content: %d", w.Code)
	}
	_, _, err = storage.Mutate(t.Context(), created.ID, store.Auth{Owner: f.owner}, store.Mutation{RequestID: uuid.NewString(), Revision: latest.Revision, Action: "guest-revoke"})
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", f.cfg.ShareOrigin+"/play/"+created.ID+"/state", nil)
	r.AddCookie(guestCookie)
	if w := response(restarted.handler, r); w.Code != 404 {
		t.Fatal("replica accepted revoked guest cookie")
	}
}

func TestNativeAndHTMXImportErrorsPreserveDraft(t *testing.T) {
	storage := httpStore(t)
	f := newHTTPFixture(t, storage)
	for _, htmx := range []bool{false, true} {
		values := url.Values{"request_id": {uuid.NewString()}, "name": {"Opponent"}, "color": {"w"}, "mode": {"custom"}, "fen": {"saved invalid FEN draft"}, "pgn": {"[Result \"1-0\"]\n\n1. e4 0-1"}}
		r := formRequest("POST", f.cfg.Origin+"/matches", f.cfg.Origin, values)
		f.sign(r, f.owner, false)
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		w := response(f.handler, r)
		body, _ := io.ReadAll(w.Result().Body)
		if w.Code != 422 || !strings.Contains(string(body), "saved invalid FEN draft") || !strings.Contains(string(body), "1. e4 0-1") {
			t.Fatalf("import draft lost: status=%d body=%s", w.Code, body)
		}
		var count int
		if err := storage.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM matches`).Scan(&count); err != nil || count != 0 {
			t.Fatal("invalid import created partial match")
		}
	}
}

func TestPGNImportRetainsOpeningAndCompletedArchiveState(t *testing.T) {
	storage := httpStore(t)
	f := newHTTPFixture(t, storage)
	for _, result := range []string{"*", "1-0"} {
		created, err := storage.Create(t.Context(), f.owner, uuid.NewString(), store.Setup{Name: "Imported opponent", Color: "w", Mode: "custom", PGN: "1. e4 e5 2. Nf3 Nc6 3. Bb5 " + result})
		if err != nil {
			t.Fatal(err)
		}
		before, err := storage.Snapshot(t.Context(), created.ID, store.Auth{Owner: f.owner})
		if err != nil || before.Mode != "standard" {
			t.Fatalf("standard PGN lost opening eligibility: %+v %v", before, err)
		}
		id, guest, err := storage.Exchange(t.Context(), created.PlayingToken)
		if err != nil {
			t.Fatal(err)
		}
		after, err := storage.Snapshot(t.Context(), id, store.Auth{Session: guest})
		if err != nil {
			t.Fatal(err)
		}
		if result == "1-0" && (after.Status != "completed" || after.Reason != "imported_result" || after.Revision != before.Revision) {
			t.Fatal("completed import reactivated on guest exchange")
		}
		for _, target := range []string{f.cfg.Origin + "/matches/" + id, f.cfg.ShareOrigin + "/watch/" + created.ViewToken + "/state"} {
			r := httptest.NewRequest("GET", target, nil)
			f.sign(r, f.owner, false)
			w := response(f.handler, r)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Ruy Lopez") {
				t.Fatalf("imported opening missing from player/view page: %d", w.Code)
			}
		}
	}
}

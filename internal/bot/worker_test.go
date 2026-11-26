package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/game"
)

const testToken = "test-only-012345678901234567890123456789"

func enqueue(t *testing.T, handler http.Handler, request Request, token string) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(request)
	r := httptest.NewRequest("POST", "/jobs", bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestWorkerCapacityDeduplicationValidationAndDelivery(t *testing.T) {
	results := make(chan Result, 2)
	var attempts atomic.Int32
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Authorized(r, testToken) {
			t.Error("callback unauthenticated")
		}
		var result Result
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			t.Error(err)
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		results <- result
		w.WriteHeader(204)
	}))
	defer webhook.Close()
	w := NewWorker(testToken, webhook.URL, "unused", 1)
	h := w.Handler()
	request := Request{uuid.NewString(), "1. e4 *", 3, webhook.URL}
	for _, tc := range []struct {
		token  string
		change func(*Request)
		status int
	}{
		{"bad", func(*Request) {}, 401},
		{testToken, func(r *Request) { r.Level = 0 }, 422},
		{testToken, func(r *Request) { r.Webhook = "http://untrusted.example" }, 422},
		{testToken, func(r *Request) { r.PGN = "invalid" }, 422},
		{testToken, func(r *Request) { r.PGN = `[Result "1-0"] 1. e4 1-0` }, 422},
		{testToken, func(*Request) {}, 202},
		{testToken, func(*Request) {}, 202},
		{testToken, func(r *Request) { r.Level = 4 }, 409},
		{testToken, func(r *Request) { r.ID = uuid.NewString() }, 429},
	} {
		r := request
		tc.change(&r)
		if response := enqueue(t, h, r, tc.token); response.Code != tc.status {
			t.Fatalf("want %d got %d: %s", tc.status, response.Code, response.Body)
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	w.search = func(ctx context.Context, _ string, g *game.Game, level int) (string, error) {
		if g.Board.Fen() == game.StartFEN || level != 3 {
			t.Error("search lost PGN or difficulty")
		}
		close(started)
		<-release
		return "e7e5", nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	<-started
	next := request
	next.ID = uuid.NewString()
	if response := enqueue(t, h, next, testToken); response.Code != 429 {
		t.Fatalf("active search excluded from capacity: %d", response.Code)
	}
	close(release)
	select {
	case result := <-results:
		if result.ID != request.ID || result.UCI != "e7e5" {
			t.Fatal(result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("callback missing")
	}
	cancel()
	<-done
	if attempts.Load() != 2 {
		t.Fatalf("callback retry count: %d", attempts.Load())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.jobs) != 0 {
		t.Fatal("capacity leaked")
	}
}

func TestSearchUCIOptionsAndCanonicalCastling(t *testing.T) {
	// A local protocol double tests command framing without needing Stockfish.
	log := filepath.Join(t.TempDir(), "commands")
	script := filepath.Join(t.TempDir(), "engine")
	content := `#!/bin/sh
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$ENGINE_TEST_LOG"
  case "$line" in
    uci) echo uciok ;;
    isready) echo readyok ;;
    go*) echo 'bestmove e1h1' ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENGINE_TEST_LOG", log)
	g, err := game.New("4k3/8/8/8/8/8/8/4K2R w K - 0 1", false)
	if err != nil {
		t.Fatal(err)
	}
	uci, err := Search(t.Context(), script, g, 5)
	if err != nil || uci != "e1h1" {
		t.Fatalf("%s %v", uci, err)
	}
	data, _ := os.ReadFile(log)
	for _, command := range []string{"Threads value 1", "Hash value 32", "Ponder value false", "UCI_Chess960 value true", "Skill Level value 20", "position fen " + g.InitialFEN, "go movetime 1000"} {
		if !strings.Contains(string(data), command) {
			t.Fatalf("missing %q in %s", command, data)
		}
	}
}

func TestStockfishRealSearch(t *testing.T) {
	binary := os.Getenv("STOCKFISH_TEST_PATH")
	if binary == "" {
		if p, err := exec.LookPath("stockfish"); err == nil {
			binary = p
		}
	}
	if binary == "" {
		t.Skip("set STOCKFISH_TEST_PATH for real engine acceptance")
	}
	for _, fen := range []string{game.StartFEN, "4k3/8/8/8/8/8/8/R3K2R w KQ - 0 1"} {
		g, err := game.New(fen, false)
		if err != nil {
			t.Fatal(err)
		}
		for level := 1; level <= 5; level++ {
			move, err := Search(t.Context(), binary, g, level)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := game.LegalMove(g.Board, move, g.Chess960); err != nil {
				t.Fatal(err)
			}
		}
	}
	fen, _ := game.Chess960(42)
	g, _ := game.New(fen, true)
	if _, err := Search(t.Context(), binary, g, 3); err != nil {
		t.Fatal(err)
	}
}

func TestSearchDeadlineKillsHungEngine(t *testing.T) {
	script := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	g, _ := game.New(game.StartFEN, false)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Search(ctx, script, g, 1); err == nil {
		t.Fatal("hung engine succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("engine was not killed promptly")
	}
}

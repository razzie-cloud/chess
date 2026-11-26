package bot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/razzie-cloud/chess/internal/game"
)

type job struct {
	Request
	game *game.Game
}

type Worker struct {
	ingress                chan struct{}
	Token, Webhook, Binary string
	queue                  chan job
	mu                     sync.Mutex
	jobs                   map[string][32]byte
	client                 *http.Client
	search                 func(context.Context, string, *game.Game, int) (string, error)
}

// Capacity includes the running search and all queued/delivering jobs.
func NewWorker(token, webhook, binary string, capacity int) *Worker {
	return &Worker{ingress: make(chan struct{}, 1), Token: token, Webhook: webhook, Binary: binary, queue: make(chan job, capacity), jobs: make(map[string][32]byte), client: Client(), search: Search}
}

func (s *Worker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs", s.enqueue)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	return mux
}

func (s *Worker) enqueue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !Authorized(r, s.Token) {
		http.Error(w, "Unauthorized", 401)
		return
	}
	select {
	case s.ingress <- struct{}{}:
		defer func() { <-s.ingress }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "Busy validating a job", 429)
		return
	}
	var request Request
	if err := Decode(w, r, &request, 300<<10); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}
	if _, err := uuid.Parse(request.ID); err != nil || request.Level < 1 || request.Level > 5 || request.Webhook != s.Webhook || len(request.PGN) > game.MaxPGNBytes {
		http.Error(w, "Invalid job", 422)
		return
	}
	payload, _ := json.Marshal(request)
	digest := sha256.Sum256(payload)
	s.mu.Lock()
	if prior, ok := s.jobs[request.ID]; ok {
		s.mu.Unlock()
		if prior != digest {
			http.Error(w, "Job ID already used", 409)
			return
		}
		w.WriteHeader(202)
		return
	}
	if len(s.jobs) >= cap(s.queue) {
		s.mu.Unlock()
		w.Header().Set("Retry-After", "5")
		http.Error(w, "Queue full", 429)
		return
	}
	s.jobs[request.ID] = digest
	s.mu.Unlock()
	g, err := game.ImportPGN(request.PGN)
	if err != nil || g.Result != "*" {
		s.remove(request.ID)
		http.Error(w, "PGN must describe an unfinished legal game", 422)
		return
	}
	s.queue <- job{request, g}
	w.WriteHeader(202)
}

func (s *Worker) remove(id string) { s.mu.Lock(); delete(s.jobs, id); s.mu.Unlock() }

func (s *Worker) Run(ctx context.Context) {
	defer s.client.CloseIdleConnections()
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-s.queue:
			uci, err := s.search(ctx, s.Binary, j.game, j.Level)
			if err == nil {
				payload, _ := json.Marshal(Result{j.ID, uci})
				for attempt := 0; attempt < 3 && ctx.Err() == nil; attempt++ {
					call, cancel := context.WithTimeout(ctx, 2*time.Second)
					r, _ := http.NewRequestWithContext(call, "POST", s.Webhook, bytes.NewReader(payload))
					r.Header.Set("Authorization", "Bearer "+s.Token)
					r.Header.Set("Content-Type", "application/json")
					resp, er := s.client.Do(r)
					status := 0
					if er == nil {
						status = resp.StatusCode
						resp.Body.Close()
					}
					cancel()
					if status >= 200 && status < 300 {
						break
					}
				}
			} else if ctx.Err() == nil {
				slog.Warn("Bot search failed; Chess will retry", "event", "bot.search_failed")
			}
			s.remove(j.ID)
		}
	}
}

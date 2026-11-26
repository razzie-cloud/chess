package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/razzie-cloud/chess/internal/bot"
	"github.com/razzie-cloud/chess/internal/store"
)

// BotHandler is mounted only on a separate private listener, never on either
// browser host or the proxy. A bot credential cannot call human/guest handlers.
func BotHandler(storage *store.Store, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /bot/moves", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !bot.Authorized(r, token) {
			http.Error(w, "Unauthorized", 401)
			return
		}
		var result bot.Result
		if err := bot.Decode(w, r, &result, 1024); err != nil {
			http.Error(w, "Invalid result", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := storage.ApplyBotMove(ctx, result.ID, result.UCI); err != nil {
			status := 503
			if errors.Is(err, store.ErrInvalid) {
				status = 422
			}
			if errors.Is(err, store.ErrDenied) || errors.Is(err, store.ErrConflict) {
				status = 409
			}
			http.Error(w, "Bot move could not be applied", status)
			return
		}
		w.WriteHeader(204)
	})
	return mux
}

func DispatchBots(ctx context.Context, storage *store.Store, endpoint, webhook, token string) {
	client := bot.Client()
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	unavailable := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Dispatch at most one job per tick per replica, including on queue full.
			call, cancel := context.WithTimeout(ctx, 3*time.Second)
			job, err := storage.ClaimBotJob(call)
			if err == nil {
				payload, _ := json.Marshal(bot.Request{ID: job.ID, PGN: job.PGN, Level: job.Level, Webhook: webhook})
				r, _ := http.NewRequestWithContext(call, "POST", endpoint, bytes.NewReader(payload))
				r.Header.Set("Authorization", "Bearer "+token)
				r.Header.Set("Content-Type", "application/json")
				resp, er := client.Do(r)
				accepted := false
				if er == nil {
					accepted = resp.StatusCode == 202
					resp.Body.Close()
				}
				if !accepted {
					// The lease also recovers if this update fails or the call expires.
					_ = storage.RetryBotJob(call, job.ID)
				}
			}
			failed := err != nil && !errors.Is(err, sql.ErrNoRows)
			if failed && !unavailable && ctx.Err() == nil {
				slog.Warn("Bot dispatch unavailable", "event", "bot.dispatch_failed")
			} else if !failed && unavailable {
				slog.Info("Bot dispatch recovered", "event", "bot.dispatch_recovered")
			}
			unavailable = failed
			cancel()
		}
	}
}

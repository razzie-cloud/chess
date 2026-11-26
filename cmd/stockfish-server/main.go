package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/razzie-cloud/chess/internal/bot"
	"github.com/razzie-cloud/chess/internal/game"
	"github.com/razzie-cloud/chess/internal/healthcheck"
	"github.com/razzie-cloud/chess/internal/logging"
)

func run() error {
	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		level = "INFO"
	}
	if err := logging.SetupLevel("stockfish-server", level); err != nil {
		return errors.New("invalid LOG_LEVEL")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	token := os.Getenv("BOT_TOKEN")
	if file := os.Getenv("BOT_TOKEN_FILE"); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return errors.New("cannot read BOT_TOKEN_FILE")
		}
		token = strings.TrimSpace(string(data))
	}
	webhook := os.Getenv("BOT_WEBHOOK_URL")
	if len(token) < 32 || strings.ContainsAny(token, "\r\n") || !bot.ValidURL(webhook) {
		return errors.New("BOT_TOKEN (32+ characters) and BOT_WEBHOOK_URL are required")
	}
	capacity := 8
	if raw := os.Getenv("BOT_QUEUE_SIZE"); raw != "" {
		var err error
		capacity, err = strconv.Atoi(raw)
		if err != nil || capacity < 1 || capacity > 32 {
			return errors.New("BOT_QUEUE_SIZE must be 1..32")
		}
	}
	binary := os.Getenv("STOCKFISH_PATH")
	if binary == "" {
		binary = "/usr/games/stockfish"
	}
	port := os.Getenv("SERVICE_PORT")
	if port == "" {
		port = "8080"
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return errors.New("invalid SERVICE_PORT")
	}
	// Do a real bounded search before advertising readiness.
	g, _ := game.New(game.StartFEN, false)
	if _, err := bot.Search(ctx, binary, g, 1); err != nil {
		return errors.New("Stockfish startup search failed")
	}
	worker := bot.NewWorker(token, webhook, binary, capacity)
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	server := &http.Server{Addr: ":" + port, Handler: worker.Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, ErrorLog: logging.ServerLogger()}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("Stockfish worker ready", "event", "server.listening", "queue_capacity", capacity)
	err := server.ListenAndServe()
	stop()
	<-shutdownDone
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return errors.New("Stockfish listener failed")
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck.Run())
	}
	_ = logging.Setup("stockfish-server")
	if err := run(); err != nil {
		slog.Error(err.Error(), "event", "server.failed")
		os.Exit(1)
	}
}

package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ai-bot/backend/internal/app"
	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/db"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err = db.Migrate(ctx, pool); err != nil {
		logger.Error("migrate database", "error", err)
		os.Exit(1)
	}
	application, err := app.New(ctx, cfg, pool, logger)
	if err != nil {
		logger.Error("initialize app", "error", err)
		os.Exit(1)
	}
	// Runtime model timeout can be configured up to 10 minutes. Keep the HTTP
	// response window slightly larger so model test requests can finish.
	server := &http.Server{Addr: cfg.Addr, Handler: application.Router(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 11 * time.Minute, IdleTimeout: 2 * time.Minute}
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		application.RunWorkers(ctx)
	}()
	go func() {
		logger.Info("server started", "addr", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped unexpectedly", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown", "error", err)
	}
	select {
	case <-workerDone:
		logger.Info("workers stopped")
	case <-time.After(15 * time.Second):
		logger.Error("worker shutdown timed out")
	}
}

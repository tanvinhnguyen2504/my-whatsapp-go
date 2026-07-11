package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/vinhnguyentan99/my-whatsapp/internal/api"
	"github.com/vinhnguyentan99/my-whatsapp/internal/config"
	"github.com/vinhnguyentan99/my-whatsapp/internal/scheduler"
	"github.com/vinhnguyentan99/my-whatsapp/internal/websocket"
	"github.com/vinhnguyentan99/my-whatsapp/internal/whatsapp"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	// new service, we conve
	whatsappService, err := whatsapp.NewWhatsAppService(cfg)
	if err != nil {
		slog.Error("build provider", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("starting whatsapp provider", "provider", whatsappService.Name())
	if err := whatsappService.Connect(ctx); err != nil {
		slog.Error("connect provider", "error", err)
		os.Exit(1)
	}
	defer whatsappService.Disconnect()

	sched := scheduler.New(whatsappService)

	// Real-time layer: in-memory client hub + Postgres history, behind websocket.Service.
	db, err := sqlx.Connect("pgx", cfg.Database.DSN())
	if err != nil {
		slog.Error("connect postgres", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	wsRepo := websocket.NewMessageRepo(db)
	if err := wsRepo.EnsureSchema(ctx); err != nil {
		slog.Error("ensure ws schema", "error", err)
		os.Exit(1)
	}
	wsSvc := websocket.NewService(websocket.NewHub(), wsRepo)

	router := api.NewRouter(cfg, whatsappService, sched, wsSvc)

	srv := &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: router,
	}

	go func() {
		slog.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	// Gracefull shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown", "error", err)
	}
}

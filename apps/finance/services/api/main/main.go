package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"homelab/pkg/logger"
	"homelab/pkg/mongo"
	"homelab/pkg/setup"
	"homelab/pkg/trace"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Init()
	slog.Info("starting finance-api")

	shutdown := trace.Init(ctx, "finance-api")
	defer shutdown()

	db, err := mongo.Connect(ctx)
	if err != nil {
		slog.Error("mongo connect", "err", err)
		os.Exit(1)
	}
	defer db.Close(ctx)

	store := NewStore(db)

	go SeedAdmin(ctx, store)

	handler := NewHandler(store)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	srv := setup.Default("finance-api", mux)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
	}()

	if err := srv.Run(ctx); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

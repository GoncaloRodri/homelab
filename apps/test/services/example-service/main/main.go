package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"homelab/pkg/logger"
	"homelab/pkg/mongo"
	"homelab/pkg/setup"
	"homelab/pkg/trace"
)

func main() {
	logger.Init()
	shutdown := trace.Init(context.Background(), "example-service")
	defer shutdown()

	db, err := mongo.Connect(context.Background())
	if err != nil {
		slog.Error("mongo connect", "err", err)
		return
	}
	defer db.Close(context.Background())

	startWorker(db, 3*time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("hello\n"))
	})

	srv := setup.Default("example-service", mux)
	if err := srv.Run(context.Background()); err != nil {
		slog.Error("server exited", "err", err)
	}
}

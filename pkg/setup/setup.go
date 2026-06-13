package setup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"homelab/pkg/metrics"
	"homelab/pkg/trace"
)

type Server struct {
	Name         string
	Port         string
	Handler      http.Handler
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	ShutdownWait time.Duration
}

func Default(name string, handler http.Handler) *Server {
	return &Server{
		Name:         name,
		Port:         env("PORT", "8080"),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		ShutdownWait: 10 * time.Second,
	}
}

func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", ok)
	mux.HandleFunc("/readyz", ok)
	mux.Handle("/metrics", metrics.Handler())
	if s.Handler != nil {
		mux.Handle("/", s.Handler)
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", s.Port),
		Handler:      trace.Middleware(metrics.Middleware(mux)),
		ReadTimeout:  s.ReadTimeout,
		WriteTimeout: s.WriteTimeout,
		IdleTimeout:  s.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("listening", "addr", srv.Addr, "service", s.Name)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down", "service", s.Name)

	shutdown, cancel := context.WithTimeout(context.Background(), s.ShutdownWait)
	defer cancel()
	return srv.Shutdown(shutdown)
}

func ok(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

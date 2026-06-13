package main

import (
	"context"
	"log/slog"
	"net/http"

	"homelab/pkg/logger"
	"homelab/pkg/setup"
	"homelab/pkg/trace"
)

func main() {
	logger.Init()
	defer trace.Init(context.Background(), "gateway")()

	h := &Handler{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Root)
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("GET /register", h.RegisterPage)
	mux.HandleFunc("POST /register", h.Register)
	mux.HandleFunc("GET /dashboard", h.Dashboard)
	mux.HandleFunc("POST /api/login", h.LoginAPI)
	mux.HandleFunc("GET /api/logout", h.Logout)
	mux.HandleFunc("GET /verify", h.Verify)
	mux.HandleFunc("GET /api/me", h.Me)
	mux.HandleFunc("POST /api/register", h.RegisterProxy)
	mux.Handle("/api/admin/", http.HandlerFunc(h.AdminProxy))

	srv := setup.Default("gateway", mux)
	if err := srv.Run(context.Background()); err != nil {
		slog.Error("server exited", "err", err)
	}
}

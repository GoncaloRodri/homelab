package main

import (
	"context"
	"log/slog"
	"net/http"

	"homelab/pkg/logger"
	"homelab/pkg/mongo"
	"homelab/pkg/setup"
	"homelab/pkg/trace"
)

func main() {
	logger.Init()
	defer trace.Init(context.Background(), "users")()

	db, err := mongo.Connect(context.Background())
	if err != nil {
		slog.Error("mongo connect", "err", err)
		return
	}
	defer db.Close(context.Background())

	h := NewHandler(db)
	if err := h.SeedAdmin(context.Background()); err != nil {
		slog.Error("seed admin", "err", err)
	}
	if err := h.SeedRoles(context.Background()); err != nil {
		slog.Error("seed roles", "err", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", h.Register)
	mux.HandleFunc("GET /users/{id}", h.GetUser)
	mux.HandleFunc("GET /users/email/{email}", h.GetUserByEmail)
	mux.HandleFunc("POST /invites", h.CreateInvite)
	mux.HandleFunc("POST /verify-password", h.VerifyPassword)

	mux.HandleFunc("GET /admin/roles", h.AdminListRoles)
	mux.HandleFunc("POST /admin/roles", h.AdminCreateRole)
	mux.HandleFunc("GET /admin/roles/{id}", h.AdminGetRole)
	mux.HandleFunc("PUT /admin/roles/{id}", h.AdminUpdateRole)
	mux.HandleFunc("DELETE /admin/roles/{id}", h.AdminDeleteRole)
	mux.HandleFunc("GET /admin/users", h.AdminListUsers)
	mux.HandleFunc("POST /admin/users", h.AdminCreateUser)
	mux.HandleFunc("PUT /admin/users/{id}", h.AdminUpdateUser)
	mux.HandleFunc("POST /admin/invites", h.CreateInvite)

	srv := setup.Default("users", mux)
	if err := srv.Run(context.Background()); err != nil {
		slog.Error("server exited", "err", err)
	}
}

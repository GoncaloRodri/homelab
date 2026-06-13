package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"homelab/pkg/auth"
	"homelab/pkg/mongo"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.mongodb.org/mongo-driver/v2/bson"
	mg "go.mongodb.org/mongo-driver/v2/mongo"
)

var tracer = otel.Tracer("users")

type User struct {
	ID       string   `bson:"_id" json:"id"`
	Email    string   `bson:"email" json:"email"`
	Password string   `bson:"password" json:"-"`
	Roles    []string `bson:"roles" json:"roles"`
}

type Invite struct {
	Code   string `bson:"_id" json:"code"`
	UsedBy string `bson:"used_by,omitempty" json:"used_by,omitempty"`
}

type Role struct {
	ID          string   `bson:"_id" json:"id"`
	Name        string   `bson:"name" json:"name"`
	Description string   `bson:"description" json:"description"`
	Permissions []string `bson:"permissions" json:"permissions"`
}

type Handler struct {
	users   *mg.Collection
	invites *mg.Collection
	roles   *mg.Collection
}

func NewHandler(db *mongo.DB) *Handler {
	return &Handler{
		users:   db.Collection("users"),
		invites: db.Collection("invites"),
		roles:   db.Collection("roles"),
	}
}

func (h *Handler) SeedAdmin(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "SeedAdmin")
	defer span.End()

	count, err := h.users.CountDocuments(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		slog.InfoContext(ctx, "users exist, skipping admin seed")
		return nil
	}

	email := os.Getenv("ADMIN_EMAIL")
	password := os.Getenv("ADMIN_PASSWORD")
	if email == "" || password == "" {
		return fmt.Errorf("ADMIN_EMAIL and ADMIN_PASSWORD must be set when no users exist")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	user := User{
		ID:       bson.NewObjectID().Hex(),
		Email:    email,
		Password: hash,
		Roles:    []string{"admin", "user"},
	}
	if _, err := h.users.InsertOne(ctx, user); err != nil {
		return fmt.Errorf("insert admin: %w", err)
	}

	span.SetAttributes(attribute.String("admin.email", email))
	slog.InfoContext(ctx, "seeded admin user", "email", email)
	return nil
}

func (h *Handler) SeedRoles(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "SeedRoles")
	defer span.End()

	count, err := h.roles.CountDocuments(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("count roles: %w", err)
	}
	if count > 0 {
		slog.InfoContext(ctx, "roles exist, skipping seed")
		return nil
	}

	defaults := []Role{
		{
			ID:          bson.NewObjectID().Hex(),
			Name:        "admin",
			Description: "Full access to all services and management",
			Permissions: []string{"service:*", "users:manage", "roles:manage"},
		},
		{
			ID:          bson.NewObjectID().Hex(),
			Name:        "user",
			Description: "Default user with basic access",
			Permissions: []string{"service:home:access"},
		},
		{
			ID:          bson.NewObjectID().Hex(),
			Name:        "grafana-reader",
			Description: "Access to Grafana dashboards",
			Permissions: []string{"service:grafana:access"},
		},
		{
			ID:          bson.NewObjectID().Hex(),
			Name:        "jaeger-reader",
			Description: "Access to Jaeger tracing UI",
			Permissions: []string{"service:jaeger:access"},
		},
	}

	for _, r := range defaults {
		if _, err := h.roles.InsertOne(ctx, r); err != nil {
			return fmt.Errorf("insert role %s: %w", r.Name, err)
		}
	}

	span.SetAttributes(attribute.Int("roles.count", len(defaults)))
	slog.InfoContext(ctx, "seeded default roles", "count", len(defaults))
	return nil
}

func (h *Handler) ResolvePermissions(ctx context.Context, roleNames []string) ([]string, error) {
	ctx, span := tracer.Start(ctx, "ResolvePermissions",
		trace.WithAttributes(attribute.StringSlice("roles", roleNames)),
	)
	defer span.End()

	if len(roleNames) == 0 {
		return nil, nil
	}

	cursor, err := h.roles.Find(ctx, bson.M{"name": bson.M{"$in": roleNames}})
	if err != nil {
		return nil, fmt.Errorf("find roles: %w", err)
	}
	defer cursor.Close(ctx)

	seen := map[string]bool{}
	var result []string
	for cursor.Next(ctx) {
		var r Role
		if err := cursor.Decode(&r); err != nil {
			return nil, fmt.Errorf("decode role: %w", err)
		}
		for _, p := range r.Permissions {
			if !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		}
	}

	if r := cursor.Err(); r != nil {
		return nil, fmt.Errorf("cursor: %w", r)
	}

	span.SetAttributes(attribute.Int("permissions.count", len(result)))
	return result, nil
}

// --- User-facing handlers ---

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "Register")
	defer span.End()

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		span.RecordError(err)
		slog.WarnContext(ctx, "register: invalid body")
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	span.SetAttributes(attribute.String("register.email", body.Email))

	var inv Invite
	if err := h.invites.FindOne(ctx, bson.M{"_id": body.Code}).Decode(&inv); err != nil {
		span.SetAttributes(attribute.String("register.result", "invalid_invite"))
		slog.WarnContext(ctx, "register: invalid invite code", "email", body.Email)
		http.Error(w, "invalid invite code", http.StatusForbidden)
		return
	}
	if inv.UsedBy != "" {
		span.SetAttributes(attribute.String("register.result", "invite_used"))
		slog.WarnContext(ctx, "register: invite already used", "code", body.Code)
		http.Error(w, "invite code already used", http.StatusForbidden)
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "register: hash password", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user := User{
		ID:       bson.NewObjectID().Hex(),
		Email:    body.Email,
		Password: hash,
		Roles:    []string{"user"},
	}
	if _, err := h.users.InsertOne(ctx, user); err != nil {
		span.SetAttributes(attribute.String("register.result", "email_conflict"))
		slog.WarnContext(ctx, "register: email already exists", "email", body.Email)
		http.Error(w, "email already registered", http.StatusConflict)
		return
	}
	h.invites.UpdateOne(ctx, bson.M{"_id": body.Code}, bson.M{"$set": bson.M{"used_by": user.ID}})

	span.SetAttributes(attribute.String("register.result", "success"), attribute.String("user.id", user.ID))
	slog.InfoContext(ctx, "user registered", "email", body.Email, "id", user.ID)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "GetUser")
	defer span.End()

	id := r.PathValue("id")
	span.SetAttributes(attribute.String("user.id", id))

	var user User
	if err := h.users.FindOne(ctx, bson.M{"_id": id}).Decode(&user); err != nil {
		span.SetAttributes(attribute.String("get.result", "not_found"))
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(user)
}

func (h *Handler) GetUserByEmail(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "GetUserByEmail")
	defer span.End()

	email := r.PathValue("email")
	span.SetAttributes(attribute.String("user.email", email))

	var user User
	if err := h.users.FindOne(ctx, bson.M{"email": email}).Decode(&user); err != nil {
		span.SetAttributes(attribute.String("get.result", "not_found"))
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(user)
}

func (h *Handler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "CreateInvite")
	defer span.End()

	code := auth.GenerateCode()
	_, err := h.invites.InsertOne(ctx, Invite{Code: code, UsedBy: ""})
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "create invite", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	span.SetAttributes(attribute.String("invite.code", code))
	slog.InfoContext(ctx, "invite created", "code", code)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func (h *Handler) VerifyPassword(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "VerifyPassword")
	defer span.End()

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		span.RecordError(err)
		slog.WarnContext(ctx, "verify: invalid body")
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	span.SetAttributes(attribute.String("verify.email", body.Email))

	var user User
	if err := h.users.FindOne(ctx, bson.M{"email": body.Email}).Decode(&user); err != nil {
		span.SetAttributes(attribute.String("verify.result", "user_not_found"))
		slog.WarnContext(ctx, "verify: user not found", "email", body.Email)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if !auth.CheckPassword(body.Password, user.Password) {
		span.SetAttributes(attribute.String("verify.result", "wrong_password"))
		slog.WarnContext(ctx, "verify: wrong password", "email", body.Email)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	perms, err := h.ResolvePermissions(ctx, user.Roles)
	if err != nil {
		slog.ErrorContext(ctx, "verify: resolve permissions", "err", err)
	}

	span.SetAttributes(
		attribute.String("verify.result", "success"),
		attribute.String("user.id", user.ID),
		attribute.StringSlice("user.roles", user.Roles),
		attribute.Int("permissions.count", len(perms)),
	)
	slog.InfoContext(ctx, "password verified", "email", body.Email, "id", user.ID, "roles", user.Roles)

	json.NewEncoder(w).Encode(struct {
		ID          string   `json:"id"`
		Email       string   `json:"email"`
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}{user.ID, user.Email, user.Roles, perms})
}

// --- Admin handlers ---

func (h *Handler) AdminListRoles(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "AdminListRoles")
	defer span.End()

	cur, err := h.roles.Find(ctx, bson.M{})
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "admin: list roles", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer cur.Close(ctx)

	var roles []Role
	cur.All(ctx, &roles)
	if roles == nil {
		roles = []Role{}
	}
	span.SetAttributes(attribute.Int("roles.count", len(roles)))
	json.NewEncoder(w).Encode(roles)
}

func (h *Handler) AdminCreateRole(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "AdminCreateRole")
	defer span.End()

	var role Role
	if err := json.NewDecoder(r.Body).Decode(&role); err != nil {
		span.RecordError(err)
		slog.WarnContext(ctx, "admin: create role invalid body")
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if role.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	role.ID = bson.NewObjectID().Hex()
	if _, err := h.roles.InsertOne(ctx, role); err != nil {
		span.SetAttributes(attribute.String("role.name", role.Name))
		http.Error(w, "role already exists", http.StatusConflict)
		return
	}

	span.SetAttributes(attribute.String("role.name", role.Name), attribute.String("role.id", role.ID))
	slog.InfoContext(ctx, "role created", "name", role.Name, "id", role.ID)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(role)
}

func (h *Handler) AdminGetRole(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "AdminGetRole")
	defer span.End()

	id := r.PathValue("id")
	span.SetAttributes(attribute.String("role.id", id))

	var role Role
	if err := h.roles.FindOne(ctx, bson.M{"_id": id}).Decode(&role); err != nil {
		span.SetAttributes(attribute.String("get.result", "not_found"))
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(role)
}

func (h *Handler) AdminUpdateRole(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "AdminUpdateRole")
	defer span.End()

	id := r.PathValue("id")
	span.SetAttributes(attribute.String("role.id", id))

	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		span.RecordError(err)
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	update := bson.M{}
	if body.Name != "" {
		update["name"] = body.Name
	}
	if body.Description != "" {
		update["description"] = body.Description
	}
	if body.Permissions != nil {
		update["permissions"] = body.Permissions
	}

	result, err := h.roles.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": update})
	if err != nil {
		span.RecordError(err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if result.MatchedCount == 0 {
		span.SetAttributes(attribute.String("update.result", "not_found"))
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	span.SetAttributes(attribute.String("update.result", "success"))
	slog.InfoContext(ctx, "role updated", "id", id)

	h.AdminGetRole(w, r)
}

func (h *Handler) AdminDeleteRole(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "AdminDeleteRole")
	defer span.End()

	id := r.PathValue("id")
	span.SetAttributes(attribute.String("role.id", id))

	result, err := h.roles.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		span.RecordError(err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if result.DeletedCount == 0 {
		span.SetAttributes(attribute.String("delete.result", "not_found"))
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	span.SetAttributes(attribute.String("delete.result", "success"))
	slog.InfoContext(ctx, "role deleted", "id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "AdminCreateUser")
	defer span.End()

	var body struct {
		Email    string   `json:"email"`
		Password string   `json:"password"`
		Roles    []string `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		span.RecordError(err)
		slog.WarnContext(ctx, "admin: create user invalid body")
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	span.SetAttributes(attribute.String("user.email", body.Email))

	if body.Email == "" || body.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "admin: hash password", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user := User{
		ID:       bson.NewObjectID().Hex(),
		Email:    body.Email,
		Password: hash,
		Roles:    body.Roles,
	}
	if user.Roles == nil {
		user.Roles = []string{"user"}
	}

	if _, err := h.users.InsertOne(ctx, user); err != nil {
		span.SetAttributes(attribute.String("create.result", "email_conflict"))
		slog.WarnContext(ctx, "admin: email already exists", "email", body.Email)
		http.Error(w, "email already registered", http.StatusConflict)
		return
	}

	span.SetAttributes(attribute.String("create.result", "success"), attribute.String("user.id", user.ID))
	slog.InfoContext(ctx, "admin created user", "email", body.Email, "id", user.ID, "roles", user.Roles)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "AdminListUsers")
	defer span.End()

	cur, err := h.users.Find(ctx, bson.M{})
	if err != nil {
		span.RecordError(err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer cur.Close(ctx)

	var users []User
	cur.All(ctx, &users)
	if users == nil {
		users = []User{}
	}

	type userView struct {
		ID    string   `json:"id"`
		Email string   `json:"email"`
		Roles []string `json:"roles"`
	}
	view := make([]userView, len(users))
	for i, u := range users {
		view[i] = userView{ID: u.ID, Email: u.Email, Roles: u.Roles}
	}
	span.SetAttributes(attribute.Int("users.count", len(view)))
	json.NewEncoder(w).Encode(view)
}

func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "AdminUpdateUser")
	defer span.End()

	id := r.PathValue("id")
	span.SetAttributes(attribute.String("user.id", id))

	var body struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		span.RecordError(err)
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	result, err := h.users.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"roles": body.Roles}})
	if err != nil {
		span.RecordError(err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if result.MatchedCount == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	span.SetAttributes(attribute.StringSlice("new_roles", body.Roles))
	slog.InfoContext(ctx, "user roles updated", "id", id, "roles", body.Roles)
	h.GetUser(w, r)
}

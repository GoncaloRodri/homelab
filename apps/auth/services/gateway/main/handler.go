package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"homelab/pkg/auth"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

//go:embed templates/*.html
var templateFS embed.FS

func parseTmpl(files ...string) *template.Template {
	return template.Must(template.New("").Funcs(template.FuncMap{
		"join": strings.Join,
		"has":  hasString,
	}).ParseFS(templateFS, files...))
}

var loginTmpl = parseTmpl("templates/base.html", "templates/login.html")
var registerTmpl = parseTmpl("templates/base.html", "templates/register.html")
var dashboardTmpl = parseTmpl("templates/base.html", "templates/dashboard.html")
var homeTmpl = parseTmpl("templates/base.html", "templates/home.html")
var forbiddenTmpl = parseTmpl("templates/base.html", "templates/forbidden.html")

var tracer = otel.Tracer("gateway")

type Handler struct{}

// --- Template data types ---

type LoginData struct {
	Error string
}

type RegisterData struct {
	Error   string
	Success string
}

type DashboardData struct {
	UserID      string
	Email       string
	Roles       []string
	Permissions []string
	IsAdmin     bool
	Users       []UserView
	RoleDefs    []RoleView
}

type HomeData struct {
	Services []ServiceCard
}

type UserView struct {
	ID    string
	Email string
	Roles []string
}

type RoleView struct {
	ID          string
	Name        string
	Description string
	Permissions []string
}

type ServiceCard struct {
	Name        string
	Description string
	URL         string
	Icon        string
	Delay       float64
}

// --- Helpers ---

func usersSvc() string {
	if v := os.Getenv("USERS_SERVICE"); v != "" {
		return v
	}
	return "http://users"
}

func spanAttrs(r *http.Request) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("http.method", r.Method),
		attribute.String("http.target", r.URL.String()),
	}
}

func hasString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

func originalHost(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		return h
	}
	return r.Host
}

// --- Page handlers ---

func (h *Handler) Root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	host := originalHost(r)
	if strings.Contains(host, "auth.") {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	h.Home(w, r)
}

func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "Home", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	services := []ServiceCard{
		{Name: "Auth", Description: "Login and account management", URL: "http://auth.homelab.local/dashboard", Icon: "🔑", Delay: 0},
		{Name: "Finance", Description: "Track your finances", URL: "http://finance.homelab.local", Icon: "💰", Delay: 0.2},
		{Name: "Test App", Description: "Example Go service", URL: "http://test.homelab.local", Icon: "🧪", Delay: 0.4},
		{Name: "Monitoring", Description: "Use Grafana to monitor services", URL: "http://grafana.homelab.local", Icon: "📊", Delay: 0.6},
		{Name: "Jaeger", Description: "Trace service requests", URL: "http://jaeger.homelab.local", Icon: "🔍", Delay: 0.8},
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	homeTmpl.ExecuteTemplate(w, "home.html", HomeData{Services: services})
}

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "LoginPage", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	errMsg := r.URL.Query().Get("error")
	if errMsg != "" {
		span.SetAttributes(attribute.String("login.error", errMsg))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	loginTmpl.ExecuteTemplate(w, "login.html", LoginData{Error: errMsg})
	slog.DebugContext(ctx, "login page served")
}

func (h *Handler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "RegisterPage", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	success := r.URL.Query().Get("success")
	errMsg := r.URL.Query().Get("error")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	registerTmpl.ExecuteTemplate(w, "register.html", RegisterData{Error: errMsg, Success: success})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "Register", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	r.ParseForm()
	email := r.FormValue("email")
	password := r.FormValue("password")
	code := r.FormValue("code")

	body, _ := json.Marshal(map[string]string{"email": email, "password": password, "code": code})
	resp, err := httpClient.Post(fmt.Sprintf("%s/register", usersSvc()), "application/json", bytes.NewReader(body))
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "register: users service error", "err", err)
		http.Redirect(w, r, "/register?error=upstream+error", http.StatusSeeOther)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		span.SetAttributes(attribute.Int("register.status", resp.StatusCode))
		slog.WarnContext(ctx, "register failed", "email", email, "status", resp.StatusCode)
		http.Redirect(w, r, "/register?error=registration+failed", http.StatusSeeOther)
		return
	}

	span.SetAttributes(attribute.String("register.email", email))
	slog.InfoContext(ctx, "user registered", "email", email)
	http.Redirect(w, r, "/login?success=registered", http.StatusSeeOther)
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "Dashboard", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	cookie, err := r.Cookie("auth_token")
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	claims, err := auth.ValidateToken(cookie.Value)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	data := DashboardData{
		UserID:      claims.UserID,
		Email:       claims.Email,
		Roles:       claims.Roles,
		Permissions: claims.Permissions,
		IsAdmin:     hasString(claims.Roles, "admin"),
	}

	// Fetch users and roles for admin view
	if data.IsAdmin {
		users, err := fetchAdminUsers(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "dashboard: fetch users", "err", err)
		} else {
			data.Users = users
		}

		roles, err := fetchAdminRoles(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "dashboard: fetch roles", "err", err)
		} else {
			data.RoleDefs = roles
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	dashboardTmpl.ExecuteTemplate(w, "dashboard.html", data)
}

// --- API handlers ---

func (h *Handler) LoginAPI(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "LoginAPI", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Redirect string `json:"redirect"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		slog.WarnContext(ctx, "invalid login api body", "err", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	span.SetAttributes(attribute.String("login.email", req.Email))

	user, err := verifyCredentials(ctx, req.Email, req.Password)
	if err != nil {
		span.SetAttributes(attribute.String("auth.result", "failure"))
		slog.InfoContext(ctx, "login failed", "email", req.Email)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	span.SetAttributes(
		attribute.String("auth.result", "success"),
		attribute.String("user.id", user.ID),
		attribute.String("user.email", user.Email),
		attribute.StringSlice("user.roles", user.Roles),
	)

	token, err := auth.GenerateToken(user.ID, user.Email, user.Roles, user.Permissions)
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "token generation failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		Domain:   ".homelab.local",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
	slog.InfoContext(ctx, "login success", "email", user.Email, "roles", user.Roles)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "LoginForm", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	r.ParseForm()
	email := r.FormValue("email")
	password := r.FormValue("password")
	span.SetAttributes(attribute.String("login.email", email))

	user, err := verifyCredentials(ctx, email, password)
	if err != nil {
		span.SetAttributes(attribute.String("auth.result", "failure"))
		slog.InfoContext(ctx, "form login failed", "email", email)
		http.Redirect(w, r, "/login?error=invalid+credentials", http.StatusSeeOther)
		return
	}
	span.SetAttributes(
		attribute.String("auth.result", "success"),
		attribute.String("user.id", user.ID),
		attribute.String("user.email", user.Email),
	)

	token, err := auth.GenerateToken(user.ID, user.Email, user.Roles, user.Permissions)
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "token generation failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		Domain:   ".homelab.local",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	redirect := r.URL.Query().Get("redirect")
	if redirect == "" {
		redirect = "/dashboard"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
	slog.InfoContext(ctx, "form login success", "email", user.Email, "redirect", redirect)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "Logout", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		Domain:   ".homelab.local",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "http://homelab.local/", http.StatusSeeOther)
	slog.InfoContext(ctx, "user logged out")
}

func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "Verify", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	cookie, err := r.Cookie("auth_token")
	if err != nil {
		host := originalHost(r)
		redirect := r.URL.Query().Get("redirect")
		if redirect == "" {
			redirect = fmt.Sprintf("http://%s/", host)
		}
		loginURL := fmt.Sprintf("http://auth.homelab.local/login?redirect=%s", url.QueryEscape(redirect))
		span.SetAttributes(attribute.String("verify.result", "no_cookie"))
		slog.DebugContext(ctx, "verify: no cookie, redirecting", "to", loginURL)
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}

	claims, err := auth.ValidateToken(cookie.Value)
	if err != nil {
		span.SetAttributes(attribute.String("verify.result", "invalid_token"))
		slog.WarnContext(ctx, "verify: invalid token")
		http.Redirect(w, r, "http://auth.homelab.local/login", http.StatusFound)
		return
	}
	span.SetAttributes(
		attribute.String("verify.result", "valid"),
		attribute.String("user.id", claims.UserID),
		attribute.String("user.email", claims.Email),
		attribute.StringSlice("user.roles", claims.Roles),
	)

	// Check service-specific permission
	host := originalHost(r)
	if reqPerm, ok := servicePermissions[host]; ok {
		span.SetAttributes(attribute.String("verify.target_host", host), attribute.String("verify.required_perm", reqPerm))
		if !hasPermission(claims.Permissions, reqPerm) {
			span.SetAttributes(attribute.String("verify.result", "forbidden"))
			slog.WarnContext(ctx, "verify: access denied", "email", claims.Email, "host", host, "required", reqPerm)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			forbiddenTmpl.ExecuteTemplate(w, "forbidden.html", nil)
			return
		}
	}

	w.Header().Set("X-Auth-User-Id", claims.UserID)
	w.Header().Set("X-Auth-Email", claims.Email)
	w.Header().Set("X-Auth-Roles", strings.Join(claims.Roles, ","))
	w.Header().Set("X-Auth-Permissions", strings.Join(claims.Permissions, ","))
	w.WriteHeader(http.StatusOK)
	slog.DebugContext(ctx, "verify: allowed", "email", claims.Email, "host", host)
}

// --- Admin proxy ---

var usersURL *url.URL

func init() {
	u, _ := url.Parse(usersSvc())
	usersURL = u
}

func (h *Handler) AdminProxy(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "AdminProxy", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	cookie, err := r.Cookie("auth_token")
	if err != nil {
		span.SetAttributes(attribute.String("proxy.result", "no_cookie"))
		slog.WarnContext(ctx, "admin proxy: no cookie")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	claims, err := auth.ValidateToken(cookie.Value)
	if err != nil {
		span.SetAttributes(attribute.String("proxy.result", "invalid_token"))
		slog.WarnContext(ctx, "admin proxy: invalid token")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}
	span.SetAttributes(
		attribute.String("user.id", claims.UserID),
		attribute.String("user.email", claims.Email),
		attribute.StringSlice("user.roles", claims.Roles),
	)

	if !hasRole(claims.Roles, "admin") {
		span.SetAttributes(attribute.String("proxy.result", "forbidden"))
		slog.WarnContext(ctx, "admin proxy: not admin", "email", claims.Email, "roles", claims.Roles)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin access required"})
		return
	}

	span.SetAttributes(attribute.String("proxy.result", "proxied"))
	r2 := r.Clone(ctx)
	r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
	r2.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, "/api")
	r2.RequestURI = r2.URL.RequestURI()

	proxy := httputil.NewSingleHostReverseProxy(usersURL)
	proxy.ServeHTTP(w, r2)
	slog.InfoContext(ctx, "admin proxy", "path", r.URL.Path, "email", claims.Email)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "Me", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	cookie, err := r.Cookie("auth_token")
	if err != nil {
		span.SetAttributes(attribute.String("me.result", "no_cookie"))
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	claims, err := auth.ValidateToken(cookie.Value)
	if err != nil {
		span.SetAttributes(attribute.String("me.result", "invalid_token"))
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}

	span.SetAttributes(
		attribute.String("user.id", claims.UserID),
		attribute.String("user.email", claims.Email),
		attribute.StringSlice("user.roles", claims.Roles),
		attribute.StringSlice("user.permissions", claims.Permissions),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          claims.UserID,
		"email":       claims.Email,
		"roles":       claims.Roles,
		"permissions": claims.Permissions,
	})
	slog.DebugContext(ctx, "me: user info returned", "email", claims.Email)
}

func (h *Handler) RegisterProxy(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "RegisterProxy", trace.WithAttributes(spanAttrs(r)...))
	defer span.End()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		span.RecordError(err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read body"})
		return
	}

	resp, err := httpClient.Post(fmt.Sprintf("%s/register", usersSvc()), "application/json", bytes.NewReader(body))
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "register proxy: users service error", "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream error"})
		return
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("upstream_status", resp.StatusCode))

	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

var servicePermissions = map[string]string{
	"grafana.homelab.local": "service:grafana:access",
	"jaeger.homelab.local":  "service:jaeger:access",
	"homelab.local":         "service:home:access",
}

func hasPermission(perms []string, target string) bool {
	for _, p := range perms {
		if p == target || p == "service:*" {
			return true
		}
	}
	return false
}

func hasRole(roles []string, target string) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}

// --- Shared types ---

type userResponse struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

var httpClient = &http.Client{Timeout: 5 * time.Second}

func verifyCredentials(ctx context.Context, email, password string) (*userResponse, error) {
	ctx, span := tracer.Start(ctx, "verifyCredentials",
		trace.WithAttributes(attribute.String("login.email", email)),
	)
	defer span.End()

	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := httpClient.Post(fmt.Sprintf("%s/verify-password", usersSvc()), "application/json", bytes.NewReader(body))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("users service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		span.SetAttributes(attribute.String("verify.result", "invalid_credentials"))
		return nil, fmt.Errorf("invalid credentials")
	}

	var user userResponse
	json.NewDecoder(resp.Body).Decode(&user)
	span.SetAttributes(
		attribute.String("verify.result", "valid"),
		attribute.String("user.id", user.ID),
		attribute.String("user.email", user.Email),
		attribute.StringSlice("user.roles", user.Roles),
	)
	return &user, nil
}

// --- Admin data fetching ---

func fetchAdminUsers(ctx context.Context) ([]UserView, error) {
	ctx, span := tracer.Start(ctx, "fetchAdminUsers")
	defer span.End()

	resp, err := httpClient.Get(fmt.Sprintf("%s/admin/users", usersSvc()))
	if err != nil {
		return nil, fmt.Errorf("fetch users: %w", err)
	}
	defer resp.Body.Close()

	var views []UserView
	if err := json.NewDecoder(resp.Body).Decode(&views); err != nil {
		return nil, fmt.Errorf("decode users: %w", err)
	}
	return views, nil
}

func fetchAdminRoles(ctx context.Context) ([]RoleView, error) {
	ctx, span := tracer.Start(ctx, "fetchAdminRoles")
	defer span.End()

	resp, err := httpClient.Get(fmt.Sprintf("%s/admin/roles", usersSvc()))
	if err != nil {
		return nil, fmt.Errorf("fetch roles: %w", err)
	}
	defer resp.Body.Close()

	var roles []RoleView
	if err := json.NewDecoder(resp.Body).Decode(&roles); err != nil {
		return nil, fmt.Errorf("decode roles: %w", err)
	}
	return roles, nil
}

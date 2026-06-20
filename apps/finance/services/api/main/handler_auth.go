package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName  = "finsession"
	sessionTTL  = 30 * 24 * time.Hour
	bcryptCost  = 12 // OWASP minimum for cloud deployments
)

// ── rate limiter ──────────────────────────────────────────────────────────────

const (
	rlMaxFailures = 10
	rlWindow      = 15 * time.Minute
	rlLockout     = 15 * time.Minute
)

type rlEntry struct {
	mu          sync.Mutex
	failures    int
	windowStart time.Time
	lockedUntil time.Time
}

type loginRateLimiter struct {
	entries sync.Map
}

func newLoginRateLimiter() *loginRateLimiter {
	rl := &loginRateLimiter{}
	go func() {
		for range time.Tick(10 * time.Minute) {
			rl.cleanup()
		}
	}()
	return rl
}

func (l *loginRateLimiter) entry(ip string) *rlEntry {
	v, _ := l.entries.LoadOrStore(ip, &rlEntry{windowStart: time.Now()})
	return v.(*rlEntry)
}

// allow returns true if the IP may attempt a login right now.
func (l *loginRateLimiter) allow(ip string) bool {
	e := l.entry(ip)
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if now.Before(e.lockedUntil) {
		return false
	}
	if now.After(e.windowStart.Add(rlWindow)) {
		e.failures = 0
		e.windowStart = now
	}
	return true
}

func (l *loginRateLimiter) failure(ip string) {
	e := l.entry(ip)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.failures++
	if e.failures >= rlMaxFailures {
		e.lockedUntil = time.Now().Add(rlLockout)
		slog.Warn("auth: IP locked out after repeated failures", "ip", ip, "failures", e.failures)
	}
}

func (l *loginRateLimiter) success(ip string) {
	l.entries.Delete(ip)
}

func (l *loginRateLimiter) cleanup() {
	now := time.Now()
	l.entries.Range(func(k, v any) bool {
		e := v.(*rlEntry)
		e.mu.Lock()
		stale := now.After(e.lockedUntil) && now.After(e.windowStart.Add(rlWindow))
		e.mu.Unlock()
		if stale {
			l.entries.Delete(k)
		}
		return true
	})
}

// clientIP extracts the real client IP, honouring X-Forwarded-For from a
// trusted proxy (Traefik / cloud load balancer).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if ip, _, _ := strings.Cut(r.RemoteAddr, ":"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

// ── session helpers ───────────────────────────────────────────────────────────

// isSecure reports whether the deployment is behind HTTPS, which controls the
// Secure cookie flag and HSTS header.
func (h *Handler) isSecure() bool {
	return strings.HasPrefix(h.baseURL, "https://")
}

func (h *Handler) signSessionID(id string) string {
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write([]byte(id))
	return id + "." + hex.EncodeToString(mac.Sum(nil))
}

func (h *Handler) verifySessionToken(token string) (string, bool) {
	i := strings.LastIndex(token, ".")
	if i < 0 {
		return "", false
	}
	id := token[:i]
	expected := h.signSessionID(id)
	if !hmac.Equal([]byte(token), []byte(expected)) {
		return "", false
	}
	return id, true
}

func (h *Handler) authFromSession(r *http.Request) (authInfo, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return authInfo{}, false
	}
	id, ok := h.verifySessionToken(cookie.Value)
	if !ok {
		return authInfo{}, false
	}
	sess, err := h.store.getAuthSession(r.Context(), id)
	if err != nil || sess == nil || time.Now().After(sess.ExpiresAt) {
		return authInfo{}, false
	}
	return authInfo{UserID: sess.UserID.Hex(), Email: sess.Email}, true
}

func deviceHint(ua string) string {
	lower := strings.ToLower(ua)
	browser := "Unknown browser"
	switch {
	case strings.Contains(lower, "edg"):
		browser = "Edge"
	case strings.Contains(lower, "chrome"):
		browser = "Chrome"
	case strings.Contains(lower, "firefox"):
		browser = "Firefox"
	case strings.Contains(lower, "safari"):
		browser = "Safari"
	}
	os := ""
	switch {
	case strings.Contains(lower, "iphone"):
		os = "iPhone"
	case strings.Contains(lower, "android"):
		os = "Android"
	case strings.Contains(lower, "windows"):
		os = "Windows"
	case strings.Contains(lower, "mac os"):
		os = "macOS"
	case strings.Contains(lower, "linux"):
		os = "Linux"
	}
	if os != "" {
		return browser + " on " + os
	}
	return browser
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, userID bson.ObjectID, email string) error {
	// Rotate: delete any existing session to prevent session fixation.
	if cookie, err := r.Cookie(cookieName); err == nil {
		if id, ok := h.verifySessionToken(cookie.Value); ok {
			_ = h.store.deleteAuthSession(r.Context(), id)
		}
	}
	sess := &AuthSession{
		UserID:    userID,
		Email:     email,
		ExpiresAt: time.Now().Add(sessionTTL),
		IPAddress: clientIP(r),
		Device:    deviceHint(r.Header.Get("User-Agent")),
	}
	if err := h.store.createAuthSession(r.Context(), sess); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    h.signSessionID(sess.ID.Hex()),
		Path:     "/",
		HttpOnly: true,
		Secure:   h.isSecure(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return nil
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// ── login ─────────────────────────────────────────────────────────────────────

func (h *Handler) AuthLogin(w http.ResponseWriter, r *http.Request) {
	if a, ok := h.authFromSession(r); ok && a.UserID != "" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		h.authLoginPost(w, r)
		return
	}
	errMsg := ""
	if r.URL.Query().Get("error") == "oauth" {
		errMsg = "Google sign-in failed. Please try again or use email and password."
	}
	successMsg := ""
	if r.URL.Query().Get("deleted") == "1" {
		successMsg = h.t(r).Get("account.delete.success_login")
	}
	renderRaw(w, authLoginTmpl, map[string]any{
		"GoogleEnabled": h.googleID != "",
		"Error":         errMsg,
		"Success":       successMsg,
		"T":             h.t(r),
	})
}

func (h *Handler) authLoginPost(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	email := strings.TrimSpace(r.FormValue("email"))

	fail := func(msg string) {
		h.loginRL.failure(ip)
		slog.Warn("auth: login failed", "ip", ip, "email", email)
		renderRaw(w, authLoginTmpl, map[string]any{
			"Error":         msg,
			"Email":         email,
			"GoogleEnabled": h.googleID != "",
			"T":             h.t(r),
		})
	}

	if !h.loginRL.allow(ip) {
		slog.Warn("auth: login blocked by rate limiter", "ip", ip)
		http.Error(w, "Too many failed attempts. Try again in 15 minutes.", http.StatusTooManyRequests)
		return
	}

	password := r.FormValue("password")
	if email == "" || password == "" {
		fail("Email and password are required.")
		return
	}
	user, err := h.store.findAuthUserByEmail(r.Context(), email)
	if err != nil || user == nil || user.PasswordHash == "" {
		fail("Invalid email or password.")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		fail("Invalid email or password.")
		return
	}

	h.loginRL.success(ip)
	if err := h.startSession(w, r, user.ID, user.Email); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	slog.Info("auth: login successful", "user_id", user.ID.Hex(), "email", user.Email, "ip", ip)

	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") {
		next = "/dashboard"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// ── register ──────────────────────────────────────────────────────────────────

func (h *Handler) AuthRegister(w http.ResponseWriter, r *http.Request) {
	if a, ok := h.authFromSession(r); ok && a.UserID != "" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		h.authRegisterPost(w, r)
		return
	}
	renderRaw(w, authRegisterTmpl, map[string]any{"GoogleEnabled": h.googleID != "", "T": h.t(r)})
}

func (h *Handler) authRegisterPost(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	name := strings.TrimSpace(r.FormValue("name"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	fail := func(msg string) {
		renderRaw(w, authRegisterTmpl, map[string]any{
			"Error":         msg,
			"Email":         email,
			"Name":          name,
			"GoogleEnabled": h.googleID != "",
			"T":             h.t(r),
		})
	}

	if email == "" || password == "" {
		fail("Email and password are required.")
		return
	}
	if password != confirm {
		fail("Passwords do not match.")
		return
	}
	if len(password) < 8 {
		fail("Password must be at least 8 characters.")
		return
	}
	existing, err := h.store.findAuthUserByEmail(r.Context(), email)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		fail("An account with that email already exists.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	user := &AuthUser{Email: email, Name: name, PasswordHash: string(hash)}
	if err := h.store.createAuthUser(r.Context(), user); err != nil {
		fail("Could not create account. Please try again.")
		return
	}
	if err := h.store.seedCategories(r.Context(), user.ID.Hex()); err != nil {
		slog.Warn("seed categories on register", "err", err)
	}
	if err := h.startSession(w, r, user.ID, user.Email); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	slog.Info("auth: new account registered", "user_id", user.ID.Hex(), "email", user.Email)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// ── logout ────────────────────────────────────────────────────────────────────

func (h *Handler) AuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		if id, ok := h.verifySessionToken(cookie.Value); ok {
			_ = h.store.deleteAuthSession(r.Context(), id)
		}
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}

// ── Google OAuth ──────────────────────────────────────────────────────────────

const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
	googleUserURL  = "https://www.googleapis.com/oauth2/v3/userinfo"
)

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Handler) googleRedirectURL() string {
	base := strings.TrimRight(h.baseURL, "/")
	if base == "" {
		base = "http://localhost:8080"
	}
	return base + "/auth/oauth/google/callback"
}

func (h *Handler) AuthGoogleStart(w http.ResponseWriter, r *http.Request) {
	if h.googleID == "" {
		http.NotFound(w, r)
		return
	}
	state := randomHex(16)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauthstate",
		Value:    state,
		Path:     "/auth",
		HttpOnly: true,
		Secure:   h.isSecure(),
		SameSite: http.SameSiteLaxMode, // Lax required — the OAuth redirect is cross-site
		MaxAge:   600,
	})
	params := url.Values{
		"client_id":     {h.googleID},
		"redirect_uri":  {h.googleRedirectURL()},
		"response_type": {"code"},
		"scope":         {"openid email profile"},
		"state":         {state},
		"access_type":   {"offline"},
		"prompt":        {"select_account"},
	}
	http.Redirect(w, r, googleAuthURL+"?"+params.Encode(), http.StatusFound)
}

func (h *Handler) AuthGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if h.googleID == "" {
		http.NotFound(w, r)
		return
	}
	stateCookie, err := r.Cookie("oauthstate")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	token, err := h.googleExchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("auth: google token exchange failed", "err", err)
		http.Redirect(w, r, "/auth/login?error=oauth", http.StatusFound)
		return
	}
	gUser, err := h.googleUserInfo(r.Context(), token)
	if err != nil {
		slog.Error("auth: google userinfo failed", "err", err)
		http.Redirect(w, r, "/auth/login?error=oauth", http.StatusFound)
		return
	}

	user, err := h.store.findAuthUserByProvider(r.Context(), "google", gUser.Sub)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		user, err = h.store.findAuthUserByEmail(r.Context(), gUser.Email)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if user == nil {
		user = &AuthUser{Email: gUser.Email, Name: gUser.Name, Provider: "google", ProviderID: gUser.Sub}
		if err := h.store.createAuthUser(r.Context(), user); err != nil {
			slog.Error("auth: create oauth user", "err", err)
			http.Redirect(w, r, "/auth/login?error=oauth", http.StatusFound)
			return
		}
		_ = h.store.seedCategories(r.Context(), user.ID.Hex())
		slog.Info("auth: new OAuth account", "provider", "google", "user_id", user.ID.Hex(), "email", user.Email)
	}

	if err := h.startSession(w, r, user.ID, user.Email); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

type googleTokenResp struct {
	AccessToken string `json:"access_token"`
}

type googleUserResp struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (h *Handler) googleExchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {h.googleID},
		"client_secret": {h.googleSecret},
		"redirect_uri":  {h.googleRedirectURL()},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google token error %d: %s", resp.StatusCode, body)
	}
	var tr googleTokenResp
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", err
	}
	return tr.AccessToken, nil
}

func (h *Handler) googleUserInfo(ctx context.Context, accessToken string) (*googleUserResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google userinfo error %d: %s", resp.StatusCode, body)
	}
	var u googleUserResp
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// ── Account / security page ───────────────────────────────────────────────────

func (h *Handler) AccountPage(w http.ResponseWriter, r *http.Request) {
	a := h.getAuth(r)
	if a.UserID == "" {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	t := h.t(r)

	// Current session ID (to highlight it in the list)
	currentID := ""
	if cookie, err := r.Cookie(cookieName); err == nil {
		currentID, _ = h.verifySessionToken(cookie.Value)
	}

	sessions, _ := h.store.getSessionsByUserID(r.Context(), a.UserID)
	var views []SessionView
	for _, s := range sessions {
		views = append(views, SessionView{
			ID:        s.ID.Hex(),
			CreatedAt: s.CreatedAt,
			IPAddress: s.IPAddress,
			Device:    s.Device,
			IsCurrent: s.ID.Hex() == currentID,
		})
	}

	user, _ := h.store.findAuthUserByID(r.Context(), a.UserID)

	render(w, accountTmpl, AccountData{
		T:           t,
		UserID:      a.UserID,
		Email:       a.Email,
		Title:       t.Get("account.title"),
		Route:       "account",
		Sessions:    views,
		HasPassword: user != nil && user.PasswordHash != "",
		Success:     r.URL.Query().Get("success"),
	})
}

func (h *Handler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	a := h.getAuth(r)
	if a.UserID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sessionID := r.PathValue("id")
	// Prevent revoking your own current session via this endpoint (use logout instead)
	if cookie, err := r.Cookie(cookieName); err == nil {
		if cur, ok := h.verifySessionToken(cookie.Value); ok && cur == sessionID {
			http.Error(w, "use /auth/logout to end your current session", http.StatusBadRequest)
			return
		}
	}
	_ = h.store.deleteSessionForUser(r.Context(), sessionID, a.UserID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	a := h.getAuth(r)
	if a.UserID == "" {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	t := h.t(r)

	fail := func(msg string) {
		sessions, _ := h.store.getSessionsByUserID(r.Context(), a.UserID)
		var views []SessionView
		for _, s := range sessions {
			views = append(views, SessionView{
				ID:        s.ID.Hex(),
				CreatedAt: s.CreatedAt,
				IPAddress: s.IPAddress,
				Device:    s.Device,
			})
		}
		user, _ := h.store.findAuthUserByID(r.Context(), a.UserID)
		render(w, accountTmpl, AccountData{
			T:           t,
			UserID:      a.UserID,
			Email:       a.Email,
			Title:       t.Get("account.title"),
			Route:       "account",
			Sessions:    views,
			HasPassword: user != nil && user.PasswordHash != "",
			Error:       msg,
		})
	}

	user, err := h.store.findAuthUserByID(r.Context(), a.UserID)
	if err != nil || user == nil {
		fail(t.Get("account.delete.error_generic"))
		return
	}

	// Password accounts require password confirmation; OAuth accounts require typing email.
	if user.PasswordHash != "" {
		password := r.FormValue("password")
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			fail(t.Get("account.delete.error_wrong_password"))
			return
		}
	} else {
		confirm := r.FormValue("confirm_email")
		if !strings.EqualFold(confirm, user.Email) {
			fail(t.Get("account.delete.error_wrong_email"))
			return
		}
	}

	if err := h.store.deleteAllUserData(r.Context(), a.UserID); err != nil {
		slog.Error("deleteAllUserData", "user", a.UserID, "err", err)
		fail(t.Get("account.delete.error_generic"))
		return
	}

	// Clear the session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.isSecure(),
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/auth/login?deleted=1", http.StatusSeeOther)
}

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
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName    = "finsession"
	sessionTTL    = 30 * 24 * time.Hour
)

// ── session token ─────────────────────────────────────────────────────────────

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
	return authInfo{
		UserID: sess.UserID.Hex(),
		Email:  sess.Email,
	}, true
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, userID bson.ObjectID, email string) error {
	sess := &AuthSession{
		UserID:    userID,
		Email:     email,
		ExpiresAt: time.Now().Add(sessionTTL),
	}
	if err := h.store.createAuthSession(r.Context(), sess); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    h.signSessionID(sess.ID.Hex()),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
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
	// Already signed in → go to dashboard
	if a, ok := h.authFromSession(r); ok && a.UserID != "" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		h.authLoginPost(w, r)
		return
	}
	renderRaw(w, authLoginTmpl, map[string]any{
		"GoogleEnabled": h.googleID != "",
	})
}

func (h *Handler) authLoginPost(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	fail := func(msg string) {
		renderRaw(w, authLoginTmpl, map[string]any{
			"Error":         msg,
			"Email":         email,
			"GoogleEnabled": h.googleID != "",
		})
	}

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
	if err := h.startSession(w, r, user.ID, user.Email); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
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
	renderRaw(w, authRegisterTmpl, map[string]any{
		"GoogleEnabled": h.googleID != "",
	})
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
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
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
		SameSite: http.SameSiteLaxMode,
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
		slog.Error("google token exchange", "err", err)
		http.Redirect(w, r, "/auth/login?error=oauth", http.StatusFound)
		return
	}
	gUser, err := h.googleUserInfo(r.Context(), token)
	if err != nil {
		slog.Error("google userinfo", "err", err)
		http.Redirect(w, r, "/auth/login?error=oauth", http.StatusFound)
		return
	}

	// Find by OAuth provider first, then fall back to matching email
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
		user = &AuthUser{
			Email:      gUser.Email,
			Name:       gUser.Name,
			Provider:   "google",
			ProviderID: gUser.Sub,
		}
		if err := h.store.createAuthUser(r.Context(), user); err != nil {
			slog.Error("create oauth user", "err", err)
			http.Redirect(w, r, "/auth/login?error=oauth", http.StatusFound)
			return
		}
		_ = h.store.seedCategories(r.Context(), user.ID.Hex())
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

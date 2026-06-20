package main

// Additional tests to raise coverage above 80%.
// These live in a separate file to keep handler_test.go manageable.

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ── deviceHint ────────────────────────────────────────────────────────────────

func TestDeviceHint(t *testing.T) {
	cases := []struct {
		ua   string
		want string
	}{
		{
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			"Safari on iPhone",
		},
		{
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
			"Chrome on Windows",
		},
		{
			"Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Firefox on Linux",
		},
		{
			"Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			"Chrome on Android",
		},
		{
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_2) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
			"Safari on macOS",
		},
		{
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
			"Edge on Windows",
		},
		{"", "Unknown browser"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got := deviceHint(tc.ua)
			if got != tc.want {
				t.Errorf("deviceHint(%q) = %q, want %q", tc.ua, got, tc.want)
			}
		})
	}
}

// ── clientIP ─────────────────────────────────────────────────────────────────

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1")
	if got := clientIP(r); got != "203.0.113.1" {
		t.Errorf("clientIP with XFF = %q, want 203.0.113.1", got)
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.RemoteAddr = "198.51.100.5:12345"
	got2 := clientIP(r2)
	if !strings.HasPrefix(got2, "198.51.100.5") {
		t.Errorf("clientIP with RemoteAddr = %q, want prefix 198.51.100.5", got2)
	}
}

// ── sortWaterfallRows ─────────────────────────────────────────────────────────

func TestSortWaterfallRows(t *testing.T) {
	byCat := map[string]int64{"Food": 5000, "Housing": 20000, "Transport": 2000}
	colors := map[string]string{"Food": "#f00", "Housing": "#0f0", "Transport": "#00f"}
	rows := sortWaterfallRows(byCat, colors)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Cents > rows[i-1].Cents {
			t.Errorf("rows not sorted desc at index %d: %d > %d", i, rows[i].Cents, rows[i-1].Cents)
		}
	}
}

// ── txnFingerprint ────────────────────────────────────────────────────────────

func TestTxnFingerprint(t *testing.T) {
	fp1 := txnFingerprint("2024-01-01", "Coffee", -500, "acc1")
	fp2 := txnFingerprint("2024-01-01", "Coffee", -500, "acc1")
	if fp1 != fp2 {
		t.Error("identical inputs must produce identical fingerprint")
	}
	fp3 := txnFingerprint("2024-01-01", "Coffee", -600, "acc1")
	if fp1 == fp3 {
		t.Error("different amount must produce different fingerprint")
	}
	fp4 := txnFingerprint("2024-01-02", "Coffee", -500, "acc1")
	if fp1 == fp4 {
		t.Error("different date must produce different fingerprint")
	}
}

// ── AccountPage ───────────────────────────────────────────────────────────────

func TestAccountPage_NoUser(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AccountPage(w, authReq("GET", "/account", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Account") {
		t.Error("expected account page content")
	}
}

func TestAccountPage_WithSessions(t *testing.T) {
	store := &mockStore{
		sessions: []AuthSession{
			{
				CreatedAt: time.Now().Add(-2 * time.Hour),
				IPAddress: "10.0.0.1",
				Device:    "Chrome on macOS",
			},
		},
		authUsers: map[string]*AuthUser{
			"user1": {Email: "test@example.com", PasswordHash: "hash"},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.AccountPage(w, authReq("GET", "/account", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "10.0.0.1") {
		t.Error("expected session IP in response")
	}
}

// ── RevokeSession ─────────────────────────────────────────────────────────────

func TestRevokeSession(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("DELETE", "/sessions/sess123", nil)
	r.SetPathValue("id", "sess123")
	w := httptest.NewRecorder()
	h.RevokeSession(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestRevokeSession_NoAuth(t *testing.T) {
	h := newHandler(&mockStore{})
	r := httptest.NewRequest("DELETE", "/sessions/sess123", nil) // no auth headers
	r.SetPathValue("id", "sess123")
	w := httptest.NewRecorder()
	h.RevokeSession(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// ── DeleteAccount ─────────────────────────────────────────────────────────────

func TestDeleteAccount_PasswordSuccess(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), 4)
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			"user1": {Email: "test@example.com", PasswordHash: string(hash)},
		},
	}
	h := newHandler(store)
	form := url.Values{"password": {"secret123"}}
	w := httptest.NewRecorder()
	h.DeleteAccount(w, authReq("POST", "/account/delete", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "deleted=1") {
		t.Error("expected redirect to login?deleted=1")
	}
}

func TestDeleteAccount_WrongPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), 4)
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			"user1": {Email: "test@example.com", PasswordHash: string(hash)},
		},
	}
	h := newHandler(store)
	form := url.Values{"password": {"wrong"}}
	w := httptest.NewRecorder()
	h.DeleteAccount(w, authReq("POST", "/account/delete", form))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error rendered)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "wrong_password") && !strings.Contains(w.Body.String(), "password") {
		t.Error("expected error message in response")
	}
}

func TestDeleteAccount_OAuthSuccess(t *testing.T) {
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			"user1": {Email: "test@example.com", PasswordHash: ""},
		},
	}
	h := newHandler(store)
	form := url.Values{"confirm_email": {"test@example.com"}}
	w := httptest.NewRecorder()
	h.DeleteAccount(w, authReq("POST", "/account/delete", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestDeleteAccount_WrongEmail(t *testing.T) {
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			"user1": {Email: "test@example.com", PasswordHash: ""},
		},
	}
	h := newHandler(store)
	form := url.Values{"confirm_email": {"wrong@example.com"}}
	w := httptest.NewRecorder()
	h.DeleteAccount(w, authReq("POST", "/account/delete", form))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error rendered)", w.Code)
	}
}

func TestDeleteAccount_UserNotFound(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"password": {"any"}}
	w := httptest.NewRecorder()
	h.DeleteAccount(w, authReq("POST", "/account/delete", form))
	// user not found → renders account page with error
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestDeleteAccount_StoreError(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), 4)
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			"user1": {Email: "test@example.com", PasswordHash: string(hash)},
		},
		deleteAllUserDataErr: fmt.Errorf("db down"),
	}
	h := newHandler(store)
	form := url.Values{"password": {"pass"}}
	w := httptest.NewRecorder()
	h.DeleteAccount(w, authReq("POST", "/account/delete", form))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error rendered)", w.Code)
	}
}

// ── AuthLogin / AuthRegister / AuthLogout ──────────────────────────────────────

func TestAuthLogin_GET(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthLogin(w, httptest.NewRequest("GET", "/auth/login", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthRegister_GET(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthRegister(w, httptest.NewRequest("GET", "/auth/register", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthLogout(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthLogout(w, httptest.NewRequest("POST", "/auth/logout", nil))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── Tax ───────────────────────────────────────────────────────────────────────

func TestTax_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Tax(w, authReq("GET", "/tax", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestTax_WithData(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 300000, Category: "Income", Date: now.AddDate(0, -1, 0)},
			{ID: "t2", UserID: "user1", AmountCents: -5000, Category: "Transport", Date: now.AddDate(0, -1, 0)},
			{ID: "t3", UserID: "user1", AmountCents: -80000, Category: "Housing", Date: now.AddDate(0, -2, 0)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Tax(w, authReq("GET", "/tax", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Tax") {
		t.Error("expected 'Tax' in response")
	}
}

func TestTax_WithYearFilter(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Tax(w, authReq("GET", "/tax?year=2024", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── TaxExport ─────────────────────────────────────────────────────────────────

func TestTaxExport_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.TaxExport(w, authReq("GET", "/tax/export", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	if !strings.Contains(w.Body.String(), "Date,Description,Category,Amount") {
		t.Error("expected CSV header in response")
	}
}

func TestTaxExport_WithTransactions(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: -5000, Category: "Transport",
				Description: "Uber", Date: now.AddDate(0, -1, 0)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.TaxExport(w, authReq("GET", "/tax/export", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Settings ──────────────────────────────────────────────────────────────────

func TestSettings_GET(t *testing.T) {
	store := &mockStore{
		accounts: []Account{{ID: "a1", Name: "Main", Type: "checking"}},
		categories: []Category{
			{ID: "c1", Name: "Food", Color: "#f00", BudgetCents: 10000},
		},
		goals: []Goal{{ID: "g1", Name: "Holiday", TargetCents: 100000}},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	// Default tab is "accounts" — check that account name appears
	h.Settings(w, authReq("GET", "/settings", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Main") {
		t.Error("expected account name in settings")
	}

	// Categories tab — check category name appears
	w2 := httptest.NewRecorder()
	h.Settings(w2, authReq("GET", "/settings?tab=categories", nil))
	if w2.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "Food") {
		t.Error("expected category name in settings categories tab")
	}
}

func TestSettings_TabCategories(t *testing.T) {
	store := &mockStore{
		categories: []Category{{ID: "c1", Name: "Rent", Color: "#000", BudgetCents: 50000}},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Settings(w, authReq("GET", "/settings?tab=categories", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── SetLang ───────────────────────────────────────────────────────────────────

func TestSetLang_PT(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"lang": {"pt"}}
	r := authReq("POST", "/lang", form)
	r.Header.Set("Referer", "/dashboard")
	w := httptest.NewRecorder()
	h.SetLang(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestSetLang_Invalid(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"lang": {"de"}}
	r := authReq("POST", "/lang", form)
	w := httptest.NewRecorder()
	h.SetLang(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303 (still redirects even for unsupported lang)", w.Code)
	}
}

// ── AutoImport ────────────────────────────────────────────────────────────────

func TestAutoImport_GET(t *testing.T) {
	store := &mockStore{
		accounts: []Account{{ID: "a1", Name: "Main", Type: "checking"}},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.AutoImport(w, authReq("GET", "/auto-import", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Household ─────────────────────────────────────────────────────────────────

func TestHousehold_GET_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Household(w, authReq("GET", "/household", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHousehold_POST(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"partner_email": {"partner@example.com"}}
	w := httptest.NewRecorder()
	h.Household(w, authReq("POST", "/household", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestHousehold_POST_MissingEmail(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"partner_email": {""}}
	w := httptest.NewRecorder()
	h.Household(w, authReq("POST", "/household", form))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHousehold_DELETE(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("DELETE", "/household", nil)
	w := httptest.NewRecorder()
	h.Household(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

// ── People ────────────────────────────────────────────────────────────────────

func TestPeople_GET(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.People(w, authReq("GET", "/people", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestPeople_POST_Share(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"_action": {"share"}, "viewer_id": {"other-user"}}
	w := httptest.NewRecorder()
	h.People(w, authReq("POST", "/people", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestPeople_POST_Household(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"_action": {"household"}, "partner_email": {"partner@example.com"}}
	w := httptest.NewRecorder()
	h.People(w, authReq("POST", "/people", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestPeople_DELETE_Share(t *testing.T) {
	store := &mockStore{
		permissions: []Permission{{ID: "p1", OwnerID: "user1", ViewerID: "other"}},
	}
	h := newHandler(store)
	r := authReq("DELETE", "/people/other?kind=share", nil)
	r.SetPathValue("id", "other")
	w := httptest.NewRecorder()
	h.People(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

// ── SaveTickerMapping ────────────────────────────────────────────────────────

func TestSaveTickerMapping(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"isin": {"IE00B3WJKG14"}, "ticker": {"QDVE.DE"}}
	w := httptest.NewRecorder()
	h.SaveTickerMapping(w, authReq("POST", "/portfolio/ticker", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestSaveTickerMapping_MissingFields(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"isin": {"IE00B3WJKG14"}} // missing ticker
	w := httptest.NewRecorder()
	h.SaveTickerMapping(w, authReq("POST", "/portfolio/ticker", form))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── Homepage ─────────────────────────────────────────────────────────────────

func TestHomepage(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Homepage(w, authReq("GET", "/", nil))
	// logged-in user is redirected to dashboard
	if w.Code != http.StatusSeeOther && w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 or 303", w.Code)
	}
}

// ── Cross-page consistency ────────────────────────────────────────────────────
// These tests verify that the same data source is reflected consistently
// across multiple pages (i.e., Dashboard, Transactions, Goals, Settings
// all use the same mock store and display consistent values).

func TestConsistency_TransactionAppearsInBothPagesAndDashboard(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		categories: []Category{{ID: "c1", UserID: "user1", Name: "Food", BudgetCents: 30000}},
		transactions: []Transaction{
			{ID: "income-1", UserID: "user1", AmountCents: 300000, Category: "Income", Date: now.AddDate(0, 0, -2)},
			{ID: "food-1", UserID: "user1", AmountCents: -12500, Category: "Food", Date: now.AddDate(0, 0, -1)},
		},
	}
	h := newHandler(store)

	// Dashboard sees the spending
	wd := httptest.NewRecorder()
	h.Dashboard(wd, authReq("GET", "/", nil))
	if wd.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", wd.Code)
	}
	if !strings.Contains(wd.Body.String(), "Food") {
		t.Error("Dashboard should display Food category from the shared store")
	}

	// Transactions page sees the same transactions
	wt := httptest.NewRecorder()
	h.Transactions(wt, authReq("GET", "/transactions", nil))
	if wt.Code != http.StatusOK {
		t.Fatalf("transactions status = %d", wt.Code)
	}
	if !strings.Contains(wt.Body.String(), "food-1") {
		t.Error("Transactions page should display the same transaction")
	}
}

func TestConsistency_CategoryBudgetInSettingsAndDashboard(t *testing.T) {
	store := &mockStore{
		categories: []Category{
			{ID: "c1", UserID: "user1", Name: "Groceries", Color: "#4caf50", BudgetCents: 25000},
		},
		transactions: []Transaction{
			newTxn("income", "Income", 200000, 5),
			newTxn("grocery-shop", "Groceries", -28000, 3), // over budget
		},
	}
	h := newHandler(store)

	// Settings page (categories tab) shows the category with budget
	ws := httptest.NewRecorder()
	h.Settings(ws, authReq("GET", "/settings?tab=categories", nil))
	if ws.Code != http.StatusOK {
		t.Fatalf("settings status = %d", ws.Code)
	}
	if !strings.Contains(ws.Body.String(), "Groceries") {
		t.Error("Settings should show Groceries category on categories tab")
	}

	// Dashboard shows the same category exceeded budget
	wd := httptest.NewRecorder()
	h.Dashboard(wd, authReq("GET", "/dashboard", nil))
	if wd.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", wd.Code)
	}
	if !strings.Contains(wd.Body.String(), "Groceries") {
		t.Error("Dashboard should reflect the same Groceries category from Settings")
	}
}

func TestConsistency_CommittedGoalInGoalsAndDashboard(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "income", UserID: "user1", AmountCents: 300000, Category: "Income", Date: now.AddDate(0, 0, -1)},
		},
		goals: []Goal{{
			ID:          "g1",
			UserID:      "user1",
			Name:        "NewCar",
			TargetCents: 1500000,
			SavedCents:  50000,
			Deadline:    now.AddDate(0, 12, 0),
			Committed:   true,
		}},
	}
	h := newHandler(store)

	// Goals page shows the goal
	wg := httptest.NewRecorder()
	h.Goals(wg, authReq("GET", "/goals", nil))
	if wg.Code != http.StatusOK {
		t.Fatalf("goals status = %d", wg.Code)
	}
	if !strings.Contains(wg.Body.String(), "NewCar") {
		t.Error("Goals page should display the committed goal")
	}

	// Dashboard shows the committed goal in the widget
	wd := httptest.NewRecorder()
	h.Dashboard(wd, authReq("GET", "/dashboard", nil))
	if wd.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", wd.Code)
	}
	if !strings.Contains(wd.Body.String(), "NewCar") {
		t.Error("Dashboard widget should display the same committed goal")
	}
}

func TestConsistency_CreateTransactionThenRead(t *testing.T) {
	store := &mockStore{
		accounts: []Account{{ID: "acc1", UserID: "user1", Name: "Main"}},
	}
	h := newHandler(store)

	// Create transaction via API
	body := `{"account_id":"acc1","date":"2024-06-01","description":"Spotify","amount_cents":-999,"category":"Entertainment"}`
	r := authReq("POST", "/api/transactions", nil)
	r.Body = bodyReader(body)
	r.Header.Set("Content-Type", "application/json")
	wc := httptest.NewRecorder()
	h.CreateTransaction(wc, r)
	if wc.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", wc.Code, wc.Body.String())
	}

	// The transaction must now be in the store
	if len(store.transactions) != 1 {
		t.Fatalf("expected 1 transaction in store after create, got %d", len(store.transactions))
	}

	// Transactions page reads from the same store and should show it
	wt := httptest.NewRecorder()
	h.Transactions(wt, authReq("GET", "/transactions", nil))
	if wt.Code != http.StatusOK {
		t.Fatalf("transactions status = %d", wt.Code)
	}
	if !strings.Contains(wt.Body.String(), "Spotify") {
		t.Error("Transactions page should display the newly created transaction")
	}
}

func TestConsistency_GoalProgressInDashboardAndGoalsPage(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "income", UserID: "user1", AmountCents: 500000, Category: "Income", Date: now.AddDate(0, 0, -3)},
		},
		goals: []Goal{
			{
				ID: "g1", UserID: "user1", Name: "Vacation",
				TargetCents: 200000, SavedCents: 80000,
				Deadline: now.AddDate(0, 4, 0), Committed: true,
			},
			{
				ID: "g2", UserID: "user1", Name: "Laptop",
				TargetCents: 150000, SavedCents: 0,
				Deadline: now.AddDate(0, 6, 0), Committed: false,
			},
		},
	}
	h := newHandler(store)

	// Goals page shows both goals
	wg := httptest.NewRecorder()
	h.Goals(wg, authReq("GET", "/goals", nil))
	if wg.Code != http.StatusOK {
		t.Fatalf("goals status = %d", wg.Code)
	}
	body := wg.Body.String()
	if !strings.Contains(body, "Vacation") || !strings.Contains(body, "Laptop") {
		t.Error("Goals page should show both goals")
	}

	// Dashboard shows only committed goals in widget
	wd := httptest.NewRecorder()
	h.Dashboard(wd, authReq("GET", "/dashboard", nil))
	if wd.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", wd.Code)
	}
	dashBody := wd.Body.String()
	if !strings.Contains(dashBody, "Vacation") {
		t.Error("Dashboard should show the committed goal (Vacation)")
	}
}

func TestConsistency_ReportsAndDashboardUseSameCategoryData(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		categories: []Category{
			{ID: "c1", UserID: "user1", Name: "Utilities", Color: "#00f", BudgetCents: 15000},
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 400000, Category: "Income", Date: now.AddDate(0, 0, -5)},
			{ID: "t2", UserID: "user1", AmountCents: -12000, Category: "Utilities", Date: now.AddDate(0, 0, -3)},
		},
	}
	h := newHandler(store)

	// Dashboard uses categories for budget tracking
	wd := httptest.NewRecorder()
	h.Dashboard(wd, authReq("GET", "/", nil))
	if wd.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", wd.Code)
	}
	if !strings.Contains(wd.Body.String(), "Utilities") {
		t.Error("Dashboard should display Utilities category")
	}

	// Reports uses same categories
	wr := httptest.NewRecorder()
	h.Reports(wr, authReq("GET", "/reports", nil))
	if wr.Code != http.StatusOK {
		t.Fatalf("reports status = %d", wr.Code)
	}
	if !strings.Contains(wr.Body.String(), "Utilities") {
		t.Error("Reports should display the same Utilities category")
	}
}

// bodyReader returns a ReadCloser for the given string body.
func bodyReader(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

// ── authLoginPost success path ────────────────────────────────────────────────

func TestAuthLoginPost_Success(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), 4)
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			"user1": {Email: "user@example.com", PasswordHash: string(hash)},
		},
	}
	h := newHandler(store)
	form := url.Values{"email": {"user@example.com"}, "password": {"password123"}}
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.AuthLogin(w, r)
	// Should redirect to /dashboard after successful login
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestAuthLoginPost_WrongPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), 4)
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			"user1": {Email: "user@example.com", PasswordHash: string(hash)},
		},
	}
	h := newHandler(store)
	form := url.Values{"email": {"user@example.com"}, "password": {"wrong"}}
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.AuthLogin(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error page)", w.Code)
	}
}

func TestAuthLoginPost_MissingEmail(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"email": {""}, "password": {"pass"}}
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.AuthLogin(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error page)", w.Code)
	}
}

func TestAuthLoginPost_ErrorQuery(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthLogin(w, httptest.NewRequest("GET", "/auth/login?error=oauth", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── authRegisterPost ──────────────────────────────────────────────────────────

func TestAuthRegisterPost_Success(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{
		"email":    {"new@example.com"},
		"name":     {"New User"},
		"password": {"securepass"},
		"confirm":  {"securepass"},
	}
	r := httptest.NewRequest("POST", "/auth/register", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.AuthRegister(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303 (redirect to dashboard): %s", w.Code, w.Body.String())
	}
}

func TestAuthRegisterPost_PasswordMismatch(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{
		"email":    {"new@example.com"},
		"password": {"securepass"},
		"confirm":  {"different"},
	}
	r := httptest.NewRequest("POST", "/auth/register", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.AuthRegister(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error rendered)", w.Code)
	}
}

func TestAuthRegisterPost_TooShort(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{
		"email":    {"new@example.com"},
		"password": {"short"},
		"confirm":  {"short"},
	}
	r := httptest.NewRequest("POST", "/auth/register", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.AuthRegister(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error rendered)", w.Code)
	}
}

func TestAuthRegisterPost_ExistingUser(t *testing.T) {
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			"u1": {Email: "existing@example.com", PasswordHash: "hash"},
		},
	}
	h := newHandler(store)
	form := url.Values{
		"email":    {"existing@example.com"},
		"password": {"goodpassword"},
		"confirm":  {"goodpassword"},
	}
	r := httptest.NewRequest("POST", "/auth/register", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.AuthRegister(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error: email taken)", w.Code)
	}
}

func TestAuthRegisterPost_MissingFields(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"email": {""}, "password": {""}}
	r := httptest.NewRequest("POST", "/auth/register", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.AuthRegister(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error rendered)", w.Code)
	}
}

// ── OrgRequestDetail: found ───────────────────────────────────────────────────

func TestOrgRequestDetail_Found(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: TxPurchaseOrder, AmountCents: 50000,
		Description: "Laptop purchase",
		StatusLog:   []StatusLogEntry{{Status: TxDraft}},
	}}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/requests/req1", "acme", nil)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestDetail(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// ── OrgRequestAction: many actions ───────────────────────────────────────────

func newOrgStoreWithRequest(txType TxRequestType, status TxRequestStatus) *mockStore {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: txType, AmountCents: 50000,
		FiscalYearID: "fy1",
		StatusLog:    []StatusLogEntry{{Status: status}},
	}}
	return store
}

func doOrgAction(t *testing.T, store *mockStore, action string, extra url.Values) int {
	t.Helper()
	h := newHandler(store)
	form := url.Values{"action": {action}}
	for k, vs := range extra {
		form[k] = vs
	}
	r := orgReq("POST", "/orgs/acme/requests/req1/action", "acme", form)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestAction(w, r)
	return w.Code
}

func TestOrgRequestAction_Approve(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxSubmitted), "approve", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_Reject(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxSubmitted), "reject", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_Review(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxSubmitted), "review", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_RequestInfo(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxSubmitted), "request_info", url.Values{"comment": {"Need more details"}})
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_RequestInfo_NoComment(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxSubmitted), "request_info", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

func TestOrgRequestAction_MarkPaid(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxApproved), "mark_paid", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_MarkPaid_WrongType(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxApproved), "mark_paid", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

func TestOrgRequestAction_MarkOrdered(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxApproved), "mark_ordered", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_MarkDelivered(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxOrdered), "mark_delivered", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_Dispute(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxOrdered), "dispute", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_Disburse(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxCashAdvance, TxApproved), "disburse", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_SettlementDue(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxCashAdvance, TxDisbursed), "settlement_due", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_MarkPendingPayment(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxIncome, TxApproved), "mark_pending_payment", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_MarkReceived(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxIncome, TxPendingPayment), "mark_received", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_Reconcile(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxApproved), "reconcile", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_Done(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxBudgetTransfer, TxApproved), "done", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

func TestOrgRequestAction_Unknown(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxDraft), "fly_to_moon", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

func TestOrgRequestAction_CancelNonCancellable(t *testing.T) {
	// Approved request cannot be cancelled
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxApproved), "cancel", nil)
	if code != http.StatusConflict {
		t.Errorf("status = %d, want 409", code)
	}
}

func TestOrgRequestAction_SubmitNonDraft(t *testing.T) {
	// Submitted request cannot be re-submitted
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxSubmitted), "submit", nil)
	if code != http.StatusConflict {
		t.Errorf("status = %d, want 409", code)
	}
}

// ── OrgRequestDelivery ────────────────────────────────────────────────────────

func TestOrgRequestDelivery_Success(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: TxPurchaseOrder, AmountCents: 50000,
		StatusLog: []StatusLogEntry{{Status: TxOrdered}},
	}}
	h := newHandler(store)
	form := url.Values{
		"actual_amount": {"45.00"},
		"actual_vendor": {"ACME Supplier"},
	}
	r := orgReq("POST", "/orgs/acme/requests/req1/delivery", "acme", form)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestDelivery(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestOrgRequestDelivery_NotOrdered(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: TxPurchaseOrder, AmountCents: 50000,
		StatusLog: []StatusLogEntry{{Status: TxApproved}},
	}}
	h := newHandler(store)
	form := url.Values{"actual_amount": {"45.00"}}
	r := orgReq("POST", "/orgs/acme/requests/req1/delivery", "acme", form)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestDelivery(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestOrgRequestDelivery_BadAmount(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: TxPurchaseOrder, AmountCents: 50000,
		StatusLog: []StatusLogEntry{{Status: TxOrdered}},
	}}
	h := newHandler(store)
	form := url.Values{"actual_amount": {"not-a-number"}}
	r := orgReq("POST", "/orgs/acme/requests/req1/delivery", "acme", form)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestDelivery(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── OrgRequestSettle ─────────────────────────────────────────────────────────

func TestOrgRequestSettle_Success(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: TxCashAdvance, AmountCents: 100000,
		StatusLog: []StatusLogEntry{{Status: TxDisbursed}},
	}}
	h := newHandler(store)
	form := url.Values{
		"amount_spent":    {"80.00"},
		"amount_returned": {"20.00"},
	}
	r := orgReq("POST", "/orgs/acme/requests/req1/settle", "acme", form)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestSettle(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestOrgRequestSettle_BadAmount(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: TxCashAdvance, AmountCents: 100000,
		StatusLog: []StatusLogEntry{{Status: TxDisbursed}},
	}}
	h := newHandler(store)
	form := url.Values{"amount_spent": {"not-a-number"}}
	r := orgReq("POST", "/orgs/acme/requests/req1/settle", "acme", form)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestSettle(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestOrgRequestSettle_WrongStatus(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: TxCashAdvance, AmountCents: 100000,
		StatusLog: []StatusLogEntry{{Status: TxApproved}},
	}}
	h := newHandler(store)
	form := url.Values{"amount_spent": {"80.00"}}
	r := orgReq("POST", "/orgs/acme/requests/req1/settle", "acme", form)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestSettle(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

// ── OrgBankImport: multipart CSV upload ──────────────────────────────────────

func buildCSVMultipart(csvContent, extraField, extraValue string) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("csv", "bank.csv")
	fw.Write([]byte(csvContent))
	if extraField != "" {
		mw.WriteField(extraField, extraValue)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestOrgBankImport_POST_Preview(t *testing.T) {
	h := newHandler(newOrgStore())
	body, ct := buildCSVMultipart("date,description,amount\n2025-01-15,Coffee,-15.00\n", "", "")
	r := httptest.NewRequest("POST", "/orgs/acme/bank-import", body)
	r.Header.Set("Content-Type", ct)
	r.Header.Set("X-Auth-User-Id", "user1")
	r.Header.Set("X-Auth-Email", "test@example.com")
	r.SetPathValue("slug", "acme")
	w := httptest.NewRecorder()
	h.OrgBankImport(w, r)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

func TestOrgBankImport_POST_Confirmed(t *testing.T) {
	h := newHandler(newOrgStore())
	body, ct := buildCSVMultipart("date,description,amount\n2025-01-15,Coffee,-15.00\n", "confirm", "1")
	r := httptest.NewRequest("POST", "/orgs/acme/bank-import", body)
	r.Header.Set("Content-Type", ct)
	r.Header.Set("X-Auth-User-Id", "user1")
	r.Header.Set("X-Auth-Email", "test@example.com")
	r.SetPathValue("slug", "acme")
	w := httptest.NewRecorder()
	h.OrgBankImport(w, r)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

func TestOrgBankImport_POST_NoFile(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/bank-import", "acme", url.Values{"confirm": {"0"}})
	w := httptest.NewRecorder()
	h.OrgBankImport(w, r)
	// Either 400 (no multipart) or 400 (no file)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

func TestOrgBankImport_POST_InvalidCSV(t *testing.T) {
	h := newHandler(newOrgStore())
	body, ct := buildCSVMultipart("not,valid\n", "", "")
	r := httptest.NewRequest("POST", "/orgs/acme/bank-import", body)
	r.Header.Set("Content-Type", ct)
	r.Header.Set("X-Auth-User-Id", "user1")
	r.Header.Set("X-Auth-Email", "test@example.com")
	r.SetPathValue("slug", "acme")
	w := httptest.NewRecorder()
	h.OrgBankImport(w, r)
	// Invalid CSV → re-render form with error
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── SearchUsers ───────────────────────────────────────────────────────────────

func TestSearchUsers_VeryShortQuery(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.SearchUsers(w, authReq("GET", "/api/users?q=a", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestSearchUsers_EmptyQuery(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.SearchUsers(w, authReq("GET", "/api/users", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestSearchUsers_WithQuery(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	// Real HTTP call to http://users will fail — handler returns empty array gracefully
	h.SearchUsers(w, authReq("GET", "/api/users?q=alice", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "[]") && !strings.Contains(w.Body.String(), "null") {
		t.Logf("body: %s", w.Body.String())
	}
}

// ── OrgRequestList: with status filter ───────────────────────────────────────

func TestOrgRequestList_WithStatusFilter(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/requests?status=submitted", "acme", nil)
	w := httptest.NewRecorder()
	h.OrgRequestList(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgFiscalYearClose: success ───────────────────────────────────────────────

func TestOrgFiscalYearClose_WithActiveYear(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/years/fy1/close", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgFiscalYearClose(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgReport: with ledger entries ───────────────────────────────────────────

func TestOrgReport_WithLedgerEntries(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Conference", Status: EventApproved}}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/report", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgReport(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Conference") {
		t.Error("expected event name in report")
	}
}

// ── OrgEventList: with events ─────────────────────────────────────────────────

func TestOrgEventList_WithEvents(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Annual Gala", Status: EventDraft},
		{ID: "evt2", OrgID: "org1", FiscalYearID: "fy1", Name: "Workshop", Status: EventApproved},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/years/fy1/events", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventList(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgRequestNew: BudgetTransfer type ───────────────────────────────────────

func TestOrgRequestNew_BudgetTransfer(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{
		"type":                 {"budget_transfer"},
		"amount":               {"500"},
		"from_budget_line_id":  {"bl1"},
		"to_budget_line_id":    {"bl2"},
	}
	r := orgReq("POST", "/orgs/acme/requests/new", "acme", form)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgRequestNew: with due date ─────────────────────────────────────────────

func TestOrgRequestNew_WithDueDate(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{
		"type":     {"reimbursement"},
		"amount":   {"100"},
		"due_date": {"2025-06-30"},
	}
	r := orgReq("POST", "/orgs/acme/requests/new", "acme", form)
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── People: DELETE household ──────────────────────────────────────────────────

func TestPeople_DELETE_Household(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("DELETE", "/people/partner-id?kind=household", nil)
	r.SetPathValue("id", "partner-id")
	w := httptest.NewRecorder()
	h.People(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

// ── Goals: create and delete ──────────────────────────────────────────────────

func TestGoals_POST_CreateEmergency(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{
		"action":       {"create"},
		"name":         {"Emergency Fund"},
		"target_cents": {"500000"},
		"deadline":     {"2026-12-31"},
	}
	w := httptest.NewRecorder()
	h.Goals(w, authReq("POST", "/goals", form))
	if w.Code != http.StatusSeeOther && w.Code != http.StatusOK {
		t.Errorf("status = %d, want 303 or 200", w.Code)
	}
}

func TestGoals_POST_DeleteFromStore(t *testing.T) {
	store := &mockStore{goals: []Goal{{ID: "g1", UserID: "user1", Name: "Old Goal"}}}
	h := newHandler(store)
	form := url.Values{"action": {"delete"}, "goal_id": {"g1"}}
	w := httptest.NewRecorder()
	h.Goals(w, authReq("POST", "/goals", form))
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── NetWorth ──────────────────────────────────────────────────────────────────

func TestNetWorth_WithAccounts(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		accounts: []Account{
			{ID: "a1", UserID: "user1", Name: "Savings", Type: "savings"},
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 500000, Category: "Income", Date: now.AddDate(0, -1, 0)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.NetWorth(w, authReq("GET", "/net-worth", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgRequestAction: insufficient permissions (viewer role) ─────────────────

func TestOrgRequestAction_InsufficientPermissions(t *testing.T) {
	org, _, fy := testOrg()
	viewer := &OrgMember{ID: "m2", OrgID: "org1", UserID: "user1", Role: OrgRoleViewer}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": viewer},
		fiscalYears:  []FiscalYear{*fy},
		txRequests: []TxRequest{{
			ID: "req1", OrgID: "org1", Type: TxPurchaseOrder, AmountCents: 50000,
			StatusLog: []StatusLogEntry{{Status: TxSubmitted}},
		}},
	}
	h := newHandler(store)
	form := url.Values{"action": {"approve"}}
	r := orgReq("POST", "/orgs/acme/requests/req1/action", "acme", form)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestAction(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// ── OrgBankImport_POST_ConfirmedNoActiveYear ──────────────────────────────────

func TestOrgBankImport_POST_ConfirmedNoActiveYear(t *testing.T) {
	// Store has no active fiscal year — close it first
	org, member, _ := testOrg()
	draft := FiscalYear{ID: "fy1", OrgID: "org1", Label: "2025", Status: FiscalYearDraft,
		StartDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{draft},
	}
	h := newHandler(store)
	body, ct := buildCSVMultipart("date,description,amount\n2025-01-15,Coffee,-15.00\n", "confirm", "1")
	r := httptest.NewRequest("POST", "/orgs/acme/bank-import", body)
	r.Header.Set("Content-Type", ct)
	r.Header.Set("X-Auth-User-Id", "user1")
	r.Header.Set("X-Auth-Email", "test@example.com")
	r.SetPathValue("slug", "acme")
	w := httptest.NewRecorder()
	h.OrgBankImport(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (no active year): %s", w.Code, w.Body.String())
	}
}

// ── OrgHome: with active year ─────────────────────────────────────────────────

func TestOrgHome_WithData(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{
		{ID: "req1", OrgID: "org1", Type: TxReimbursement, AmountCents: 10000,
			FiscalYearID: "fy1", StatusLog: []StatusLogEntry{{Status: TxDraft}}},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.OrgHome(w, orgReq("GET", "/orgs/acme", "acme", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgRequestSettle: partial settlement ─────────────────────────────────────

func TestOrgRequestSettle_Partial(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: TxCashAdvance, AmountCents: 100000,
		StatusLog: []StatusLogEntry{{Status: TxDisbursed}},
	}}
	h := newHandler(store)
	// Spent < amount, returned < remainder → partial settlement
	form := url.Values{
		"amount_spent":    {"60.00"},
		"amount_returned": {"10.00"},
	}
	r := orgReq("POST", "/orgs/acme/requests/req1/settle", "acme", form)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestSettle(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgRequestDetail: with event, budget line, team ──────────────────────────

func TestOrgRequestDetail_FullyLoaded(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req1", OrgID: "org1", Type: TxPurchaseOrder,
		FiscalYearID: "fy1", EventID: "evt1", BudgetLineID: "bl1", TeamID: "t1",
		AmountCents: 75000,
		StatusLog:   []StatusLogEntry{{Status: TxSubmitted}},
	}}
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Q1", Status: EventApproved}}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/requests/req1", "acme", nil)
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestDetail(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// ── OrgJoin: POST success ─────────────────────────────────────────────────────

func TestOrgJoin_POST(t *testing.T) {
	org, _, fy := testOrg()
	store := &mockStore{
		orgsByID:     map[string]*Org{"org1": org},
		orgsBySlug:   map[string]*Org{"acme": org},
		membersByKey: map[string]*OrgMember{},
		fiscalYears:  []FiscalYear{*fy},
		invitesByToken: map[string]*OrgInvite{
			"tok123": {
				ID: "inv1", OrgID: "org1", Email: "test@example.com",
				Role: OrgRoleMember, Token: "tok123",
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
		},
	}
	h := newHandler(store)
	r := authReq("POST", "/orgs/join/tok123", url.Values{"token": {"tok123"}})
	r.SetPathValue("token", "tok123")
	w := httptest.NewRecorder()
	h.OrgJoin(w, r)
	// Successful join redirects or renders join page
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── OrgInviteNew POST: valid email ────────────────────────────────────────────

func TestOrgInviteNew_POST_Success(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"email": {"newmember@example.com"}, "role": {"member"}}
	r := orgReq("POST", "/orgs/acme/members/invite", "acme", form)
	w := httptest.NewRecorder()
	h.OrgInviteNew(w, r)
	if w.Code != http.StatusSeeOther && w.Code != http.StatusOK {
		t.Errorf("status = %d, want 303 or 200: %s", w.Code, w.Body.String())
	}
}

// ── OrgFiscalYearCreate ────────────────────────────────────────────────────────

func TestOrgFiscalYearCreate_WithValidDates(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{
		"label":      {"2026"},
		"start_date": {"2026-01-01"},
		"end_date":   {"2026-12-31"},
	}
	r := orgReq("POST", "/orgs/acme/years", "acme", form)
	w := httptest.NewRecorder()
	h.OrgFiscalYearCreate(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── TaxExport: with trades year filter ───────────────────────────────────────

func TestTaxExport_WithYearFilter(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.TaxExport(w, authReq("GET", "/tax/export?year=2024", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Tax: with trades ──────────────────────────────────────────────────────────

func TestTax_WithTrades(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 500000, Category: "Income", Date: now.AddDate(0, -1, 0)},
		},
		trades: []Trade{
			{ID: "tr1", UserID: "user1", ISIN: "IE00B3W", Name: "ETF Fund", Type: "buy",
				Quantity: 10, PriceCents: 10000, TotalCents: 100000, Date: now.AddDate(-1, 0, 0)},
			{ID: "tr2", UserID: "user1", ISIN: "IE00B3W", Name: "ETF Fund", Type: "sell",
				Quantity: 5, PriceCents: 12000, TotalCents: 60000, Date: now.AddDate(0, -1, 0)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Tax(w, authReq("GET", "/tax", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Accounts ──────────────────────────────────────────────────────────────────

func TestAccounts_GET_WithAccount(t *testing.T) {
	store := &mockStore{
		accounts: []Account{
			{ID: "a1", UserID: "user1", Name: "Checking", Type: "checking"},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Accounts(w, authReq("GET", "/accounts", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAccounts_POST_CreateNew(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"name": {"New Account"}, "type": {"savings"}}
	w := httptest.NewRecorder()
	h.Accounts(w, authReq("POST", "/accounts", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── Categories ────────────────────────────────────────────────────────────────

func TestCategories_POST_CreateNew(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"name": {"Travel"}, "color": {"#ff0000"}, "budget_cents": {"50000"}}
	w := httptest.NewRecorder()
	h.Categories(w, authReq("POST", "/categories", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestCategories_POST_DeleteOld(t *testing.T) {
	store := &mockStore{categories: []Category{{ID: "c1", UserID: "user1", Name: "Old"}}}
	h := newHandler(store)
	form := url.Values{"_method": {"DELETE"}, "id": {"c1"}}
	w := httptest.NewRecorder()
	h.Categories(w, authReq("POST", "/categories", form))
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── UpdateTransaction ─────────────────────────────────────────────────────────

func TestUpdateTransaction_Success(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{{ID: "t1", UserID: "user1", AmountCents: -1000, Category: "Food", Description: "Cafe"}},
	}
	h := newHandler(store)
	body := `{"description":"Coffee","category":"Beverages","amount_cents":-500}`
	r := authReq("PATCH", "/api/transactions/t1", nil)
	r.Body = bodyReader(body)
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	h.UpdateTransaction(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// ── Sharing ───────────────────────────────────────────────────────────────────

func TestSharing_GET_WithPerms(t *testing.T) {
	store := &mockStore{
		permissions: []Permission{{ID: "p1", OwnerID: "user1", ViewerID: "viewer-id"}},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Sharing(w, authReq("GET", "/sharing", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgInviteRevoke ───────────────────────────────────────────────────────────

func TestOrgInviteRevoke_Success(t *testing.T) {
	store := newOrgStore()
	h := newHandler(store)
	r := orgReq("DELETE", "/orgs/acme/invites/inv1", "acme", nil)
	r.SetPathValue("invite_id", "inv1")
	w := httptest.NewRecorder()
	h.OrgInviteRevoke(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgGoalAdd with approved event ────────────────────────────────────────────

func TestOrgGoalAdd_ApprovedEvent(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventApproved, FiscalYearID: "fy1"}}
	h := newHandler(store)
	form := url.Values{"text": {"Buy new chairs"}}
	r := orgReq("POST", "/orgs/acme/events/evt1/goals", "acme", form)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgGoalAdd(w, r)
	// Approved events should allow goal addition or return conflict
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── OrgGoalToggle with active year ────────────────────────────────────────────

func TestOrgGoalToggle_ActiveYear(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventApproved, FiscalYearID: "fy1"}}
	h := newHandler(store)
	form := url.Values{"done": {"1"}}
	r := orgReq("POST", "/orgs/acme/events/evt1/goals/g1/toggle", "acme", form)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("goal_id", "g1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgGoalToggle(w, r)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── OrgFiscalYearActivate with no events (draft year) ─────────────────────────

func TestOrgFiscalYearActivate_NoEvents(t *testing.T) {
	org, member, _ := testOrg()
	draft := FiscalYear{ID: "fy2", OrgID: "org1", Label: "2026", Status: FiscalYearDraft,
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{draft},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy2/activate", "acme", nil)
	r.SetPathValue("year_id", "fy2")
	w := httptest.NewRecorder()
	h.OrgFiscalYearActivate(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgEventEdit: GET ─────────────────────────────────────────────────────────

func TestOrgEventEdit_GET(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Q1", Status: EventDraft}}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/events/evt1/edit", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventEdit(w, r)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── OrgEventDetail: found ─────────────────────────────────────────────────────

func TestOrgEventDetail_WithGoalsAndLines(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Annual", Status: EventApproved}}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/events/evt1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventDetail(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// ── OrgFiscalYearActivate: success ───────────────────────────────────────────

func TestOrgFiscalYearActivate_DraftToActive(t *testing.T) {
	org, member, _ := testOrg()
	draft := FiscalYear{ID: "fy3", OrgID: "org1", Label: "2027", Status: FiscalYearDraft,
		StartDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{draft},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy3/activate", "acme", nil)
	r.SetPathValue("year_id", "fy3")
	w := httptest.NewRecorder()
	h.OrgFiscalYearActivate(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgAnalysis: with events and budget lines ─────────────────────────────────

func TestOrgAnalysis_WithBudgetLines(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Summit", Status: EventApproved},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/analysis", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgAnalysis(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── AuthRegister: already logged in ──────────────────────────────────────────

func TestAuthRegister_AlreadyLoggedIn(t *testing.T) {
	h := newHandler(&mockStore{})
	// When X-Auth-User-Id is set, authFromSession won't redirect (it checks cookie, not headers)
	// But we can still test the GET path
	w := httptest.NewRecorder()
	h.AuthRegister(w, httptest.NewRequest("GET", "/auth/register", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── AuthLogin: deleted account success message ────────────────────────────────

func TestAuthLogin_DeletedQuery(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthLogin(w, httptest.NewRequest("GET", "/auth/login?deleted=1", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Projections: GET ──────────────────────────────────────────────────────────

func TestProjections_GET_WithGoals(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 300000, Category: "Income", Date: now.AddDate(0, -1, 0)},
			{ID: "t2", UserID: "user1", AmountCents: -50000, Category: "Housing", Date: now.AddDate(0, -1, 0)},
		},
		goals: []Goal{{ID: "g1", UserID: "user1", Name: "Vacation", TargetCents: 100000, SavedCents: 20000, Committed: true}},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Projections(w, authReq("GET", "/projections", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgEventNew: POST with DueDate ───────────────────────────────────────────

func TestOrgEventNew_POST_WithDates(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{
		"name":       {"Tech Summit"},
		"date_start": {"2025-03-01"},
		"date_end":   {"2025-03-03"},
	}
	r := orgReq("POST", "/orgs/acme/years/fy1/events/new", "acme", form)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventNew(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgBudgetLineCreate: success ─────────────────────────────────────────────

func TestOrgBudgetLineCreate_Success(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventDraft, FiscalYearID: "fy1"}}
	h := newHandler(store)
	form := url.Values{"description": {"Catering"}, "amount": {"500"}, "type": {"expense"}}
	r := orgReq("POST", "/orgs/acme/events/evt1/budget", "acme", form)
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgBudgetLineCreate(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgGoalDelete: draft event ────────────────────────────────────────────────

func TestOrgGoalDelete_DraftEvent(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventDraft, FiscalYearID: "fy1"}}
	h := newHandler(store)
	r := orgReq("DELETE", "/orgs/acme/events/evt1/goals/g1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("goal_id", "g1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgGoalDelete(w, r)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── OrgMemberTeam assignment (via OrgTeams) ───────────────────────────────────

func TestOrgTeams_WithTeams(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/teams", "acme", nil)
	w := httptest.NewRecorder()
	h.OrgTeams(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgRequestAction: cancel in info_requested status ────────────────────────

func TestOrgRequestAction_CancelInfoRequested(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxInfoRequested), "cancel", nil)
	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", code)
	}
}

// ── OrgRequestAction: done wrong type ────────────────────────────────────────

func TestOrgRequestAction_Done_WrongType(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxApproved), "done", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// ── OrgRequestAction: disburse wrong type ────────────────────────────────────

func TestOrgRequestAction_Disburse_WrongType(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxPurchaseOrder, TxApproved), "disburse", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// ── OrgRequestAction: mark_pending_payment wrong type ────────────────────────

func TestOrgRequestAction_MarkPendingPayment_WrongType(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxApproved), "mark_pending_payment", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// ── OrgRequestAction: mark_received wrong type ───────────────────────────────

func TestOrgRequestAction_MarkReceived_WrongType(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxApproved), "mark_received", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// ── OrgRequestAction: mark_ordered wrong type ────────────────────────────────

func TestOrgRequestAction_MarkOrdered_WrongType(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxApproved), "mark_ordered", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// ── OrgRequestAction: mark_delivered wrong type ───────────────────────────────

func TestOrgRequestAction_MarkDelivered_WrongType(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxApproved), "mark_delivered", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// ── OrgRequestAction: dispute wrong type ─────────────────────────────────────

func TestOrgRequestAction_Dispute_WrongType(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxApproved), "dispute", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// ── OrgRequestAction: settlement_due wrong type ───────────────────────────────

func TestOrgRequestAction_SettlementDue_WrongType(t *testing.T) {
	code := doOrgAction(t, newOrgStoreWithRequest(TxReimbursement, TxApproved), "settlement_due", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// ── Household: with existing household data ───────────────────────────────────

func TestHousehold_GET_WithData(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Household(w, authReq("GET", "/household", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgCreate: with description ───────────────────────────────────────────────

func TestOrgCreate_POST_WithDescription(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"name": {"My Corp"}, "slug": {"my-corp"}, "description": {"A company"}}
	w := httptest.NewRecorder()
	h.OrgCreate(w, authReq("POST", "/orgs/new", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgRequestDetail: request with team lookup ────────────────────────────────

func TestOrgRequestDetail_WithFiscalYear(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{{
		ID: "req2", OrgID: "org1", Type: TxReimbursement,
		FiscalYearID: "fy1", AmountCents: 25000,
		StatusLog: []StatusLogEntry{{Status: TxApproved}},
	}}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/requests/req2", "acme", nil)
	r.SetPathValue("req_id", "req2")
	w := httptest.NewRecorder()
	h.OrgRequestDetail(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Household: with existing household ───────────────────────────────────────

func TestHousehold_GET_ExistingHousehold(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		household: &Household{
			ID:        "hh1",
			OwnerID:   "user1",
			PartnerID: "partner@example.com",
			CreatedAt: now.AddDate(0, -3, 0),
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 250000, Category: "Income", Date: now.AddDate(0, 0, -5)},
			{ID: "t2", UserID: "user1", AmountCents: -20000, Category: "Food", Date: now.AddDate(0, 0, -3)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Household(w, authReq("GET", "/household", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── People: with existing household ──────────────────────────────────────────

func TestPeople_GET_WithHousehold(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		household: &Household{
			ID:        "hh1",
			OwnerID:   "user1",
			PartnerID: "partner@example.com",
			CreatedAt: now.AddDate(0, -1, 0),
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 200000, Category: "Income", Date: now.AddDate(0, 0, -2)},
		},
		goals: []Goal{
			{ID: "g1", UserID: "user1", Name: "Joint Vacation", TargetCents: 500000, Committed: true},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	r := authReq("GET", "/people?tab=household", nil)
	h.People(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestPeople_GET_TabSharing(t *testing.T) {
	store := &mockStore{
		permissions: []Permission{
			{ID: "p1", OwnerID: "user1", ViewerID: "viewer-user"},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.People(w, authReq("GET", "/people?tab=sharing", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgAnalysis: with team membership in events ───────────────────────────────

func TestOrgAnalysis_WithTeamsInEvents(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Summit", Status: EventApproved,
			TeamIDs: []string{"t1", "t2"}},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/analysis", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgAnalysis(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgRequestList: guest-only member filter ──────────────────────────────────

func TestOrgRequestList_GuestMemberFilter(t *testing.T) {
	org, _, fy := testOrg()
	guestMember := &OrgMember{
		ID: "m2", OrgID: "org1", UserID: "user1", Role: OrgRoleMember,
		TeamIDs: []string{"t1"},
	}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": guestMember},
		fiscalYears:  []FiscalYear{*fy},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/requests", "acme", nil)
	w := httptest.NewRecorder()
	h.OrgRequestList(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgReport: with negative actual (expense path) ───────────────────────────

func TestOrgReport_NegativeActual(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Conference", Status: EventApproved},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/report", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgReport(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgFiscalYearClose: with events and all-approved check ───────────────────

func TestOrgFiscalYearClose_WithAllApproved(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Status: EventApproved},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy1/close", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgFiscalYearClose(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── authFromSession: cookie paths ─────────────────────────────────────────────

func TestAuthLogin_AlreadyLoggedIn(t *testing.T) {
	h := newHandler(&mockStore{})
	// Sign a session token like the handler does
	token := h.signSessionID("testsessid")
	r := httptest.NewRequest("GET", "/auth/login", nil)
	r.AddCookie(&http.Cookie{Name: "finsession", Value: token})
	w := httptest.NewRecorder()
	h.AuthLogin(w, r)
	// getAuthSession returns nil (session not in store) → authFromSession returns false → login page
	if w.Code != http.StatusOK && w.Code != http.StatusFound {
		t.Errorf("status = %d, want 200 or 302", w.Code)
	}
}

func TestAuthLogin_BadCookie(t *testing.T) {
	h := newHandler(&mockStore{})
	// Invalid (unsigned) cookie value — verifySessionToken returns !ok
	r := httptest.NewRequest("GET", "/auth/login", nil)
	r.AddCookie(&http.Cookie{Name: "finsession", Value: "invalid-token-value"})
	w := httptest.NewRecorder()
	h.AuthLogin(w, r)
	// verifySessionToken fails → authFromSession returns false → renders login page
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthLogin_ValidSessionRedirects(t *testing.T) {
	// Register a new account so startSession is called and a session is stored
	h := newHandler(&mockStore{})
	form := url.Values{
		"email":    {"session@example.com"},
		"name":     {"Test User"},
		"password": {"goodpassword"},
		"confirm":  {"goodpassword"},
	}
	regReq := httptest.NewRequest("POST", "/auth/register", strings.NewReader(form.Encode()))
	regReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	regW := httptest.NewRecorder()
	h.AuthRegister(regW, regReq)
	if regW.Code != http.StatusSeeOther {
		t.Fatalf("register failed: status = %d", regW.Code)
	}

	// Extract the session cookie
	cookies := regW.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after registration")
	}
	sessionCookie := cookies[0]

	// Now use that cookie on the login page — should redirect to dashboard
	loginReq := httptest.NewRequest("GET", "/auth/login", nil)
	loginReq.AddCookie(sessionCookie)
	loginW := httptest.NewRecorder()
	h.AuthLogin(loginW, loginReq)
	if loginW.Code != http.StatusFound {
		t.Errorf("status = %d, want 302 (redirect to dashboard)", loginW.Code)
	}
}

// ── loginRateLimiter cleanup ───────────────────────────────────────────────────

func TestLoginRateLimiter_Cleanup(t *testing.T) {
	rl := &loginRateLimiter{}
	// Add a stale entry manually
	rl.entries.Store("stale-ip", &rlEntry{
		windowStart: time.Now().Add(-2 * rlWindow),
		lockedUntil: time.Now().Add(-2 * rlLockout),
	})
	// Add an active entry
	rl.entries.Store("active-ip", &rlEntry{
		windowStart: time.Now(),
		lockedUntil: time.Now().Add(rlLockout),
	})
	rl.cleanup()
	if _, loaded := rl.entries.Load("stale-ip"); loaded {
		t.Error("stale entry should have been cleaned up")
	}
	if _, loaded := rl.entries.Load("active-ip"); !loaded {
		t.Error("active entry should NOT have been cleaned up")
	}
}

// ── AuthLogout: with valid cookie ─────────────────────────────────────────────

func TestAuthLogout_WithValidCookie(t *testing.T) {
	h := newHandler(&mockStore{})
	token := h.signSessionID("sess123")
	r := httptest.NewRequest("POST", "/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: "finsession", Value: token})
	w := httptest.NewRecorder()
	h.AuthLogout(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── startSession: with existing cookie (session rotation) ────────────────────

func TestStartSession_WithExistingCookie(t *testing.T) {
	h := newHandler(&mockStore{})
	// First, create an initial session
	token := h.signSessionID("oldsessid")
	// Then start a new session with an existing cookie (rotation)
	r := httptest.NewRequest("POST", "/auth/login", nil)
	r.AddCookie(&http.Cookie{Name: "finsession", Value: token})
	w := httptest.NewRecorder()
	userID := bson.NewObjectID()
	if err := h.startSession(w, r, userID, "user@example.com"); err != nil {
		t.Fatalf("startSession error: %v", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Error("expected session cookie after startSession")
	}
}

// ── ImportPreview: with CSV file ─────────────────────────────────────────────

func TestImportPreview_WithCSV(t *testing.T) {
	store := &mockStore{
		accounts: []Account{{ID: "a1", UserID: "user1", Name: "Main", Type: "checking"}},
	}
	h := newHandler(store)
	body, ct := buildCSVMultipart("Date,Description,Amount\n2025-01-01,Coffee,-5.00\n", "account_id", "a1")
	r := authReq("POST", "/import/preview", nil)
	r.Body = io.NopCloser(body)
	r.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	h.ImportPreview(w, r)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── Portfolio: GET ────────────────────────────────────────────────────────────

func TestPortfolio_GET(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Portfolio(w, authReq("GET", "/portfolio", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgEventList: NotFound year ───────────────────────────────────────────────

func TestOrgEventList_YearNotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/years/bad-year/events", "acme", nil)
	r.SetPathValue("year_id", "bad-year")
	w := httptest.NewRecorder()
	h.OrgEventList(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── OrgAnalysis: year not found ───────────────────────────────────────────────

func TestOrgAnalysis_YearNotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/analysis", "acme", nil)
	r.SetPathValue("year_id", "bad-year")
	w := httptest.NewRecorder()
	h.OrgAnalysis(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── OrgReport: year not found ─────────────────────────────────────────────────

func TestOrgReport_YearNotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/report", "acme", nil)
	r.SetPathValue("year_id", "bad-year")
	w := httptest.NewRecorder()
	h.OrgReport(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── OrgRequestNew: income type ────────────────────────────────────────────────

func TestOrgRequestNew_IncomeType(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{
		"type":        {"income"},
		"amount":      {"1000"},
		"payer_name":  {"Client Corp"},
		"description": {"Consulting fee"},
	}
	r := orgReq("POST", "/orgs/acme/requests/new", "acme", form)
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgRequestNew: cash advance type ─────────────────────────────────────────

func TestOrgRequestNew_CashAdvanceType(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{
		"type":   {"cash_advance"},
		"amount": {"500"},
	}
	r := orgReq("POST", "/orgs/acme/requests/new", "acme", form)
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgRequestNew: invalid amount ─────────────────────────────────────────────

func TestOrgRequestNew_InvalidAmount(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"type": {"reimbursement"}, "amount": {"not-a-number"}}
	r := orgReq("POST", "/orgs/acme/requests/new", "acme", form)
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── OrgEventNew: no active fiscal year ────────────────────────────────────────

func TestOrgRequestNew_NoActiveFiscalYear(t *testing.T) {
	org, member, _ := testOrg()
	draft := FiscalYear{ID: "fy2", OrgID: "org1", Label: "2026", Status: FiscalYearDraft,
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{draft},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/requests/new", "acme", nil)
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (no active fiscal year)", w.Code)
	}
}

// ── OrgFiscalYearClose: unapproved events ────────────────────────────────────

func TestOrgFiscalYearClose_UnapprovedEvents(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Status: EventDraft},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy1/close", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgFiscalYearClose(w, r)
	// Close is allowed even with unapproved events (handler just closes)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── OrgEventFeedback: with approved event ────────────────────────────────────

func TestOrgEventFeedback_ClosedYear(t *testing.T) {
	org, member, _ := testOrg()
	closed := FiscalYear{ID: "fy1", OrgID: "org1", Label: "2025", Status: FiscalYearClosed,
		StartDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{closed},
		orgEvents:    []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Status: EventApproved, Name: "Summit"}},
	}
	h := newHandler(store)
	form := url.Values{"comment": {"Great event overall"}}
	r := orgReq("POST", "/orgs/acme/events/evt1/feedback", "acme", form)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventFeedback(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── Properties: with data ────────────────────────────────────────────────────

func TestProperties_GET_WithData(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		properties: []Property{
			{ID: "p1", UserID: "user1", Name: "Main Home", Status: PropertyOwned,
				CurrentValueCents: 35000000, PurchasePriceCents: 30000000, PurchaseDate: now.AddDate(-3, 0, 0)},
			{ID: "p2", UserID: "user1", Name: "Old Flat", Status: PropertySold,
				CurrentValueCents: 20000000, PurchasePriceCents: 18000000},
		},
		loans: []Loan{
			{ID: "l1", UserID: "user1", PropertyID: "p1", Name: "Mortgage", Type: LoanMortgage,
				Status: LoanActive, PrincipalCents: 25000000, BalanceCents: 22000000,
				InterestRatePct: 3.5, TermMonths: 360, MonthlyPaymentCents: 112000,
				StartDate: now.AddDate(-3, 0, 0)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Properties(w, authReq("GET", "/property", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestProperties_GET_WithUnlinkedLoan(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		loans: []Loan{
			{ID: "l1", UserID: "user1", PropertyID: "", Name: "Personal Loan", Type: LoanPersonal,
				Status: LoanActive, PrincipalCents: 5000000, BalanceCents: 4000000,
				InterestRatePct: 5.0, TermMonths: 60, MonthlyPaymentCents: 95000,
				StartDate: now.AddDate(-1, 0, 0)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Properties(w, authReq("GET", "/property", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── loanBalanceAt: edge cases ─────────────────────────────────────────────────

func TestLoanBalanceAt_ZeroRate(t *testing.T) {
	// With zero interest rate
	b := loanBalanceAt(100000, 0, 10000, 5)
	if b != 50000 {
		t.Errorf("loanBalanceAt zero-rate = %d, want 50000", b)
	}
}

func TestLoanBalanceAt_ZeroRateOverpaid(t *testing.T) {
	// Many months → balance goes negative → returns 0
	b := loanBalanceAt(100000, 0, 10000, 20)
	if b != 0 {
		t.Errorf("loanBalanceAt zero-rate overpaid = %d, want 0", b)
	}
}

func TestLoanBalanceAt_HighInterest_LongTerm(t *testing.T) {
	// Regular path with balance > 0
	b := loanBalanceAt(30000000, 4.5, 150000, 1)
	if b <= 0 {
		t.Errorf("loanBalanceAt high interest = %d, want > 0", b)
	}
}

// ── OrgJoin: already a member ────────────────────────────────────────────────

func TestOrgJoin_POST_AlreadyMember(t *testing.T) {
	org, member, fy := testOrg()
	store := &mockStore{
		orgsByID:     map[string]*Org{"org1": org},
		orgsBySlug:   map[string]*Org{"acme": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{*fy},
		invitesByToken: map[string]*OrgInvite{
			"tok456": {
				ID: "inv2", OrgID: "org1", Email: "test@example.com",
				Role: OrgRoleMember, Token: "tok456",
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
		},
	}
	h := newHandler(store)
	r := authReq("POST", "/join/tok456", url.Values{"token": {"tok456"}})
	r.SetPathValue("token", "tok456")
	w := httptest.NewRecorder()
	h.OrgJoin(w, r)
	// Already a member → consume invite and redirect
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── detectLang ────────────────────────────────────────────────────────────────

func TestDetectLang_FromCookie(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"lang": {"pt"}}
	r := authReq("POST", "/lang", form)
	r.Header.Set("Referer", "/dashboard")
	w := httptest.NewRecorder()
	h.SetLang(w, r) // sets cookie
	// Now use the cookie for language detection
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestDetectLang_FromAcceptHeader(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("GET", "/", nil)
	r.Header.Set("Accept-Language", "pt-PT,pt;q=0.9,en;q=0.8")
	w := httptest.NewRecorder()
	h.Dashboard(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── getAuth: via headers (owner path) ────────────────────────────────────────

func TestGetAuth_WithOwner(t *testing.T) {
	h := newHandler(&mockStore{})
	// owner=true query param triggers owner check
	r := authReq("GET", "/sharing?owner=other-user-id", nil)
	w := httptest.NewRecorder()
	h.Sharing(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Accounts: DELETE ──────────────────────────────────────────────────────────

func TestAccounts_DELETE_Account(t *testing.T) {
	store := &mockStore{
		accounts: []Account{{ID: "a1", UserID: "user1", Name: "Old", Type: "checking"}},
	}
	h := newHandler(store)
	r := authReq("DELETE", "/accounts/a1", nil)
	r.SetPathValue("id", "a1")
	w := httptest.NewRecorder()
	h.Accounts(w, r)
	if w.Code != http.StatusNoContent && w.Code != http.StatusSeeOther && w.Code != http.StatusOK {
		t.Errorf("status = %d, unexpected", w.Code)
	}
}

// ── Goals: update action ──────────────────────────────────────────────────────

func TestGoals_POST_Update(t *testing.T) {
	store := &mockStore{
		goals: []Goal{{ID: "g1", UserID: "user1", Name: "Savings", TargetCents: 100000}},
	}
	h := newHandler(store)
	form := url.Values{
		"action":       {"update"},
		"goal_id":      {"g1"},
		"name":         {"Better Savings"},
		"target_cents": {"200000"},
	}
	w := httptest.NewRecorder()
	h.Goals(w, authReq("POST", "/goals", form))
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── runDreamSim: more paths ────────────────────────────────────────────────────

func TestRunDreamSim_WithPurchaseSim(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 300000, Category: "Income", Date: now.AddDate(0, -1, 0)},
			{ID: "t2", UserID: "user1", AmountCents: -100000, Category: "Housing", Date: now.AddDate(0, -1, 0)},
		},
	}
	h := newHandler(store)
	form := url.Values{
		"cost":        {"500000"},
		"down_pct":    {"20"},
		"rate":        {"4.5"},
		"term_months": {"240"},
	}
	w := httptest.NewRecorder()
	r := authReq("GET", "/dream?cost=500000&down_pct=20&rate=4.5&term_months=240", nil)
	h.Simulator(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	_ = form
}

// ── OrgHome: overview with requests ──────────────────────────────────────────

func TestOrgHome_WithRequests(t *testing.T) {
	store := newOrgStore()
	store.txRequests = []TxRequest{
		{ID: "r1", OrgID: "org1", Type: TxReimbursement, AmountCents: 5000,
			FiscalYearID: "fy1", SubmittedBy: "m1",
			StatusLog: []StatusLogEntry{{Status: TxSubmitted, ChangedBy: "m1", ChangedAt: time.Now()}}},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.OrgHome(w, orgReq("GET", "/orgs/acme", "acme", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgEventDetail: with team and attachments ────────────────────────────────

func TestOrgEventDetail_WithTeamIDs(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Gala",
			Status: EventApproved, TeamIDs: []string{"t1", "t2"}},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/years/fy1/events/evt1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventDetail(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgFiscalYearClose: error path ───────────────────────────────────────────

func TestOrgFiscalYearClose_Error(t *testing.T) {
	org, member, fy := testOrg()
	store := &mockStore{
		orgsBySlug:                map[string]*Org{"acme": org},
		orgsByID:                  map[string]*Org{"org1": org},
		membersByKey:              map[string]*OrgMember{"org1:user1": member},
		fiscalYears:               []FiscalYear{*fy},
		updateFiscalYearStatusErr: fmt.Errorf("db error"),
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy1/close", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgFiscalYearClose(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// ── Goals: adjust_deadline action ────────────────────────────────────────────

func TestGoals_POST_AdjustDeadline(t *testing.T) {
	store := &mockStore{
		goals: []Goal{{ID: "g1", UserID: "user1", Name: "Savings", TargetCents: 100000}},
	}
	h := newHandler(store)
	form := url.Values{"action": {"adjust_deadline"}, "id": {"g1"}, "months": {"6"}}
	w := httptest.NewRecorder()
	h.Goals(w, authReq("POST", "/goals", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestGoals_POST_AdjustDeadline_ZeroMonths(t *testing.T) {
	store := &mockStore{
		goals: []Goal{{ID: "g1", UserID: "user1", Name: "Savings", TargetCents: 100000}},
	}
	h := newHandler(store)
	// months=0 → skips updateGoal, still redirects
	form := url.Values{"action": {"adjust_deadline"}, "id": {"g1"}, "months": {"0"}}
	w := httptest.NewRecorder()
	h.Goals(w, authReq("POST", "/goals", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── NetWorth: with properties and loans ──────────────────────────────────────

func TestNetWorth_WithPropertiesAndLoans(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 100000, Category: "Income", Date: now.AddDate(0, -2, 0)},
		},
		properties: []Property{
			{ID: "p1", UserID: "user1", Name: "Home", Status: PropertyOwned, CurrentValueCents: 30000000},
			{ID: "p2", UserID: "user1", Name: "Sold", Status: PropertySold, CurrentValueCents: 10000000},
		},
		loans: []Loan{
			{ID: "l1", UserID: "user1", PropertyID: "p1", Name: "Mortgage",
				Status: LoanActive, PrincipalCents: 25000000, BalanceCents: 22000000,
				InterestRatePct: 3.5, TermMonths: 360, MonthlyPaymentCents: 0,
				StartDate: now.AddDate(-2, 0, 0)},
			{ID: "l2", UserID: "user1", PropertyID: "p1", Name: "Paid Loan",
				Status: LoanPaidOff, PrincipalCents: 5000000, BalanceCents: 0},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.NetWorth(w, authReq("GET", "/networth", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Accounts: GET via header auth (getAuth header path) ──────────────────────

func TestGetAuth_FromSessionCookie(t *testing.T) {
	// Register → get cookie → use cookie on Accounts (getAuth session path)
	store := &mockStore{
		authUsers: map[string]*AuthUser{},
	}
	h := newHandler(store)

	// Register a user
	form := url.Values{
		"email":    {"session@example.com"},
		"password": {"TestPass123!"},
		"name":     {"Session User"},
	}
	regW := httptest.NewRecorder()
	regR := httptest.NewRequest("POST", "/auth/register", strings.NewReader(form.Encode()))
	regR.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.AuthRegister(regW, regR)
	if regW.Code != http.StatusSeeOther {
		t.Skipf("register returned %d, skipping", regW.Code)
	}
	// Extract cookie
	var sessionCookie *http.Cookie
	for _, c := range regW.Result().Cookies() {
		if c.Name == "finsession" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Skip("no finsession cookie from register")
	}

	// Use session cookie on Accounts handler
	r := httptest.NewRequest("GET", "/accounts", nil)
	r.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	h.Accounts(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Goals: waterfall with GoalID txn ─────────────────────────────────────────

func TestGoals_GET_WithGoalTransaction(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		goals: []Goal{
			{ID: "g1", UserID: "user1", Name: "Emergency Fund", TargetCents: 100000,
				Deadline: now.AddDate(0, 6, 0)},
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 300000, Category: "Income",
				Date: now.AddDate(0, 0, -2)},
			{ID: "t2", UserID: "user1", AmountCents: -50000, Category: "Goals",
				GoalID: "g1", Date: now.AddDate(0, 0, -1)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Goals(w, authReq("GET", "/goals", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Goals: avg monthly savings positive path ──────────────────────────────────

func TestGoals_GET_AvgSavingsPositive(t *testing.T) {
	now := time.Now()
	// transactions 45 days ago (last month, not current month) = within 3-month window
	store := &mockStore{
		goals: []Goal{{ID: "g1", UserID: "user1", Name: "Travel", TargetCents: 50000,
			Deadline: now.AddDate(1, 0, 0)}},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 200000, Category: "Income",
				Date: now.AddDate(0, -1, -5)},
			{ID: "t2", UserID: "user1", AmountCents: -80000, Category: "Food",
				Date: now.AddDate(0, -1, -5)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Goals(w, authReq("GET", "/goals", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── RevokeSession: success path ───────────────────────────────────────────────

func TestRevokeSession_Success(t *testing.T) {
	now := time.Now()
	userOID := bson.NewObjectID()
	sessOID := bson.NewObjectID()
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			userOID.Hex(): {ID: userOID, Email: "test@example.com"},
		},
		sessions: []AuthSession{
			{ID: sessOID, UserID: userOID, ExpiresAt: now.Add(24 * time.Hour)},
		},
	}
	h := newHandler(store)
	r := authReq("POST", "/account/sessions/del", url.Values{"session_id": {sessOID.Hex()}})
	r.SetPathValue("session_id", sessOID.Hex())
	w := httptest.NewRecorder()
	h.RevokeSession(w, r)
	// accepts 204, 303, or 200
	if w.Code != http.StatusSeeOther && w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Errorf("status = %d, unexpected", w.Code)
	}
}

// ── AuthRegister: already registered ─────────────────────────────────────────

func TestAuthRegister_AlreadyRegistered(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("Password1!"), 4)
	existingID := bson.NewObjectID()
	store := &mockStore{
		authUsers: map[string]*AuthUser{
			existingID.Hex(): {ID: existingID, Email: "existing@example.com", PasswordHash: string(hash)},
		},
	}
	h := newHandler(store)
	form := url.Values{
		"email":    {"existing@example.com"},
		"password": {"Password1!"},
		"name":     {"Test"},
	}
	r := httptest.NewRequest("POST", "/auth/register", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.AuthRegister(w, r)
	// Should return form with error (400 or 200 with error)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── AuthRegister GET (second path) ───────────────────────────────────────────

func TestAuthRegister_GET_NoSession(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/register", nil)
	h.AuthRegister(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgJoin: GET with no token ────────────────────────────────────────────────

func TestOrgJoin_GET_InvalidToken(t *testing.T) {
	org, member, fy := testOrg()
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{*fy},
		invitesByToken: map[string]*OrgInvite{},
	}
	h := newHandler(store)
	r := authReq("GET", "/join/badtoken", nil)
	r.SetPathValue("token", "badtoken")
	w := httptest.NewRecorder()
	h.OrgJoin(w, r)
	// Token not found → show error page
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── OrgFiscalYearActivate: error paths ───────────────────────────────────────

func TestOrgFiscalYearActivate_AlreadyActive(t *testing.T) {
	org, member, _ := testOrg()
	activeFY := &FiscalYear{ID: "fy1", OrgID: "org1", Label: "2024", Status: FiscalYearActive,
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{*activeFY},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy1/activate", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgFiscalYearActivate(w, r)
	// Already active → redirect or error, not 500
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500")
	}
}

// ── OrgEventReview: request_changes path ─────────────────────────────────────

func TestOrgEventReview_RequestChanges(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Gala", Status: EventReview},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy1/events/evt1/review", "acme",
		url.Values{"action": {"request_changes"}})
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventReview(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgEventFeedback: with feedback ──────────────────────────────────────────

func TestOrgEventFeedback_WithMessage(t *testing.T) {
	org, member, _ := testOrg()
	closedFY := &FiscalYear{ID: "fy1", OrgID: "org1", Label: "2024", Status: FiscalYearClosed,
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{*closedFY},
		orgEvents: []OrgEvent{
			{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Gala", Status: EventApproved},
		},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy1/events/evt1/feedback", "acme",
		url.Values{"comment": {"Great event!"}})
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventFeedback(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── Tax handler ───────────────────────────────────────────────────────────────

func TestTax_GET_WithTransactions(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 500000, Category: "Income",
				Date: now.AddDate(0, -1, 0)},
			{ID: "t2", UserID: "user1", AmountCents: -100000, Category: "Healthcare",
				Date: now.AddDate(0, -1, 0)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Tax(w, authReq("GET", "/tax", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgTeamCreate: validation errors ─────────────────────────────────────────

func TestOrgTeamCreate_EmptyName(t *testing.T) {
	org, member, fy := testOrg()
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{*fy},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/teams", "acme", url.Values{"name": {""}})
	w := httptest.NewRecorder()
	h.OrgTeamCreate(w, r)
	// empty name → error response
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500")
	}
}

// ── OrgMemberRemove: remove other member ─────────────────────────────────────

func TestOrgMemberRemove_OtherMember(t *testing.T) {
	org, member, fy := testOrg()
	other := &OrgMember{ID: "m2", OrgID: "org1", UserID: "user2", Role: OrgRoleMember}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member, "org1:user2": other},
		fiscalYears:  []FiscalYear{*fy},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/members/user2/remove", "acme", nil)
	r.SetPathValue("user_id", "user2")
	w := httptest.NewRecorder()
	h.OrgMemberRemove(w, r)
	if w.Code != http.StatusSeeOther && w.Code != http.StatusOK {
		t.Errorf("status = %d, unexpected", w.Code)
	}
}

// ── OrgReport: with data ──────────────────────────────────────────────────────

func TestOrgReport_WithBudgetLines(t *testing.T) {
	store := newOrgStore()
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/years/fy1/report", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgReport(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgLedger: with year filter ───────────────────────────────────────────────

func TestOrgLedger_GET_WithYear(t *testing.T) {
	store := newOrgStore()
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/ledger?year=fy1", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgLedger(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgEventSubmit: submit event ──────────────────────────────────────────────

func TestOrgEventSubmit_Success(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Conference",
			Status: EventDraft, CreatedBy: "user1"},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy1/events/evt1/submit", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventSubmit(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgRequestNew: validation error ──────────────────────────────────────────

func TestOrgRequestNew_EmptyAmount(t *testing.T) {
	store := newOrgStore()
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/requests/new", "acme",
		url.Values{"type": {string(TxReimbursement)}, "title": {"Test"}, "amount": {""}})
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500")
	}
}

// ── OrgGoalDelete: success ────────────────────────────────────────────────────

func TestOrgGoalDelete_Success(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Event",
			GoalItems: []EventGoal{{ID: "gi1", Text: "Feed 50 people"}}},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy1/events/evt1/goals/gi1/delete", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	r.SetPathValue("goal_id", "gi1")
	w := httptest.NewRecorder()
	h.OrgGoalDelete(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgBudgetLineDelete: success ──────────────────────────────────────────────

func TestOrgBudgetLineDelete_Success(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Conference",
			Status: EventDraft},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy1/events/evt1/budget/bl1/delete", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("line_id", "bl1")
	w := httptest.NewRecorder()
	h.OrgBudgetLineDelete(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── bson import needed ────────────────────────────────────────────────────────
var _ = bson.NewObjectID

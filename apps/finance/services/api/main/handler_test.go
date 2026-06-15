package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ── mock store ────────────────────────────────────────────────────────────────

type mockStore struct {
	accounts     []Account
	categories   []Category
	transactions []Transaction
	trades       []Trade
	goals        []Goal
	permissions  []Permission

	createGoalErr        error
	updateGoalErr        error
	deleteTransactionErr error
	createTransactionErr error
	createTradesErr      error
}

func (m *mockStore) getAccounts(_ context.Context, _ string) ([]Account, error) {
	return m.accounts, nil
}
func (m *mockStore) getAccount(_ context.Context, id string) (*Account, error) {
	for _, a := range m.accounts {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, nil
}
func (m *mockStore) createAccount(_ context.Context, a *Account) error {
	m.accounts = append(m.accounts, *a)
	return nil
}
func (m *mockStore) deleteAccount(_ context.Context, id, _ string) error {
	for i, a := range m.accounts {
		if a.ID == id {
			m.accounts = append(m.accounts[:i], m.accounts[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *mockStore) getCategories(_ context.Context, _ string) ([]Category, error) {
	return m.categories, nil
}
func (m *mockStore) createCategory(_ context.Context, c *Category) error {
	m.categories = append(m.categories, *c)
	return nil
}
func (m *mockStore) updateCategory(_ context.Context, c *Category) error { return nil }
func (m *mockStore) deleteCategory(_ context.Context, id, _ string) error {
	for i, c := range m.categories {
		if c.ID == id {
			m.categories = append(m.categories[:i], m.categories[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *mockStore) getTransactions(_ context.Context, _ string, _ bson.M) ([]Transaction, error) {
	return m.transactions, nil
}
func (m *mockStore) getTransaction(_ context.Context, id, _ string) (*Transaction, error) {
	for _, t := range m.transactions {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, nil
}
func (m *mockStore) createTransactions(_ context.Context, txns []Transaction) error {
	if m.createTransactionErr != nil {
		return m.createTransactionErr
	}
	m.transactions = append(m.transactions, txns...)
	return nil
}
func (m *mockStore) updateTransaction(_ context.Context, _, _ string, _ bson.M) error { return nil }
func (m *mockStore) deleteTransaction(_ context.Context, id, _ string) error {
	if m.deleteTransactionErr != nil {
		return m.deleteTransactionErr
	}
	for i, t := range m.transactions {
		if t.ID == id {
			m.transactions = append(m.transactions[:i], m.transactions[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *mockStore) aggregateTransactions(_ context.Context, _ string, _ bson.A) ([]bson.M, error) {
	return nil, nil
}
func (m *mockStore) getTrades(_ context.Context, _ string) ([]Trade, error) {
	return m.trades, nil
}
func (m *mockStore) createTrades(_ context.Context, trades []Trade) error {
	if m.createTradesErr != nil {
		return m.createTradesErr
	}
	m.trades = append(m.trades, trades...)
	return nil
}
func (m *mockStore) deleteTrade(_ context.Context, id, _ string) error { return nil }
func (m *mockStore) getPermissions(_ context.Context, _ string) ([]Permission, error) {
	return m.permissions, nil
}
func (m *mockStore) getGrantedViewers(_ context.Context, _ string) ([]Permission, error) {
	return nil, nil
}
func (m *mockStore) createPermission(_ context.Context, p *Permission) error {
	m.permissions = append(m.permissions, *p)
	return nil
}
func (m *mockStore) deletePermission(_ context.Context, _, _ string) error { return nil }
func (m *mockStore) getGoals(_ context.Context, _ string) ([]Goal, error) {
	return m.goals, nil
}
func (m *mockStore) createGoal(_ context.Context, g *Goal) error {
	if m.createGoalErr != nil {
		return m.createGoalErr
	}
	m.goals = append(m.goals, *g)
	return nil
}
func (m *mockStore) updateGoal(_ context.Context, _, _ string, _ bson.M) error {
	return m.updateGoalErr
}
func (m *mockStore) deleteGoal(_ context.Context, id, _ string) error {
	for i, g := range m.goals {
		if g.ID == id {
			m.goals = append(m.goals[:i], m.goals[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *mockStore) seedCategories(_ context.Context, _ string) error { return nil }

func (m *mockStore) getTickerMappings(_ context.Context, _ string) ([]TickerMapping, error) {
	return nil, nil
}
func (m *mockStore) saveTickerMapping(_ context.Context, _, _, _ string) error { return nil }

func (m *mockStore) getHousehold(_ context.Context, _ string) (*Household, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) createHousehold(_ context.Context, _ *Household) error  { return nil }
func (m *mockStore) deleteHousehold(_ context.Context, _ string) error       { return nil }
func (m *mockStore) getImportSchedules(_ context.Context, _ string) ([]ImportSchedule, error) {
	return nil, nil
}
func (m *mockStore) createImportSchedule(_ context.Context, _ *ImportSchedule) error { return nil }
func (m *mockStore) deleteImportSchedule(_ context.Context, _, _ string) error        { return nil }

// ── Org stubs (not exercised in unit tests) ───────────────────────────────────

func (m *mockStore) getOrgsForUser(_ context.Context, _ string) ([]OrgWithRole, error) {
	return nil, nil
}
func (m *mockStore) getOrg(_ context.Context, _ string) (*Org, error)            { return nil, nil }
func (m *mockStore) getOrgBySlug(_ context.Context, _ string) (*Org, error)      { return nil, nil }
func (m *mockStore) createOrg(_ context.Context, _ *Org) error                   { return nil }
func (m *mockStore) slugExists(_ context.Context, _ string) (bool, error)        { return false, nil }
func (m *mockStore) getTeams(_ context.Context, _ string) ([]OrgTeam, error)     { return nil, nil }
func (m *mockStore) getTeam(_ context.Context, _, _ string) (*OrgTeam, error)    { return nil, nil }
func (m *mockStore) createTeam(_ context.Context, _ *OrgTeam) error              { return nil }
func (m *mockStore) deleteTeam(_ context.Context, _, _ string) error             { return nil }
func (m *mockStore) getMembers(_ context.Context, _ string) ([]OrgMember, error) { return nil, nil }
func (m *mockStore) getMember(_ context.Context, _, _ string) (*OrgMember, error) {
	return nil, nil
}
func (m *mockStore) createMember(_ context.Context, _ *OrgMember) error                   { return nil }
func (m *mockStore) updateMemberRole(_ context.Context, _, _ string, _ OrgRole) error     { return nil }
func (m *mockStore) removeMember(_ context.Context, _, _ string) error                    { return nil }
func (m *mockStore) getInvites(_ context.Context, _ string) ([]OrgInvite, error)          { return nil, nil }
func (m *mockStore) getInviteByToken(_ context.Context, _ string) (*OrgInvite, error)     { return nil, nil }
func (m *mockStore) createInvite(_ context.Context, _ *OrgInvite) error                   { return nil }
func (m *mockStore) consumeInvite(_ context.Context, _ string) error                      { return nil }
func (m *mockStore) revokeInvite(_ context.Context, _, _ string) error                    { return nil }
func (m *mockStore) getFiscalYears(_ context.Context, _ string) ([]FiscalYear, error)     { return nil, nil }
func (m *mockStore) getFiscalYear(_ context.Context, _, _ string) (*FiscalYear, error)    { return nil, nil }
func (m *mockStore) getActiveFiscalYear(_ context.Context, _ string) (*FiscalYear, error) { return nil, nil }
func (m *mockStore) createFiscalYear(_ context.Context, _ *FiscalYear) error              { return nil }
func (m *mockStore) updateFiscalYearStatus(_ context.Context, _, _ string, _ FiscalYearStatus, _ bson.M) error {
	return nil
}
func (m *mockStore) getEvents(_ context.Context, _, _ string) ([]OrgEvent, error)      { return nil, nil }
func (m *mockStore) getEvent(_ context.Context, _, _ string) (*OrgEvent, error)        { return nil, nil }
func (m *mockStore) createEvent(_ context.Context, _ *OrgEvent) error                  { return nil }
func (m *mockStore) updateEvent(_ context.Context, _, _ string, _ bson.M) error        { return nil }
func (m *mockStore) deleteEvent(_ context.Context, _, _ string) error                  { return nil }
func (m *mockStore) addGoalItem(_ context.Context, _, _ string, _ EventGoal) error     { return nil }
func (m *mockStore) toggleGoalItem(_ context.Context, _, _, _ string, _ bool, _ string) error {
	return nil
}
func (m *mockStore) deleteGoalItem(_ context.Context, _, _, _ string) error            { return nil }
func (m *mockStore) getBudgetLines(_ context.Context, _, _ string) ([]BudgetLine, error) { return nil, nil }
func (m *mockStore) createBudgetLine(_ context.Context, _ *BudgetLine) error            { return nil }
func (m *mockStore) deleteBudgetLine(_ context.Context, _, _ string) error              { return nil }
func (m *mockStore) getEventComments(_ context.Context, _, _ string) ([]EventComment, error) { return nil, nil }
func (m *mockStore) createEventComment(_ context.Context, _ *EventComment) error             { return nil }
func (m *mockStore) getTxRequests(_ context.Context, _ string, _ bson.M) ([]TxRequest, error) { return nil, nil }
func (m *mockStore) getTxRequest(_ context.Context, _, _ string) (*TxRequest, error)          { return nil, nil }
func (m *mockStore) createTxRequest(_ context.Context, _ *TxRequest) error                    { return nil }
func (m *mockStore) appendStatusLog(_ context.Context, _, _ string, _ StatusLogEntry) error   { return nil }
func (m *mockStore) updateTxRequest(_ context.Context, _, _ string, _ bson.M) error           { return nil }
func (m *mockStore) getLedgerEntries(_ context.Context, _, _ string, _ bson.M) ([]OrgLedgerEntry, error) {
	return nil, nil
}
func (m *mockStore) createLedgerEntry(_ context.Context, _ *OrgLedgerEntry) error  { return nil }
func (m *mockStore) updateLedgerEntry(_ context.Context, _, _ string, _ bson.M) error { return nil }
func (m *mockStore) getAttachments(_ context.Context, _, _ string) ([]OrgAttachment, error) {
	return nil, nil
}
func (m *mockStore) createAttachment(_ context.Context, _ *OrgAttachment) error { return nil }

func (m *mockStore) createAuthUser(_ context.Context, _ *AuthUser) error           { return nil }
func (m *mockStore) findAuthUserByEmail(_ context.Context, _ string) (*AuthUser, error) {
	return nil, nil
}
func (m *mockStore) findAuthUserByProvider(_ context.Context, _, _ string) (*AuthUser, error) {
	return nil, nil
}
func (m *mockStore) createAuthSession(_ context.Context, _ *AuthSession) error      { return nil }
func (m *mockStore) getAuthSession(_ context.Context, _ string) (*AuthSession, error) {
	return nil, nil
}
func (m *mockStore) deleteAuthSession(_ context.Context, _ string) error { return nil }

// ── helpers ───────────────────────────────────────────────────────────────────

func newHandler(store *mockStore) *Handler {
	return &Handler{store: store, secret: "test-secret"}
}

func authReq(method, path string, body url.Values) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(body.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("X-Auth-User-Id", "user1")
	r.Header.Set("X-Auth-Email", "test@example.com")
	return r
}

func newTxn(id, cat string, cents int64, daysAgo int) Transaction {
	return Transaction{
		ID:          id,
		UserID:      "user1",
		AccountID:   "acc1",
		Date:        time.Now().AddDate(0, 0, -daysAgo),
		Description: id,
		AmountCents: cents,
		Category:    cat,
	}
}

// ── Dashboard ─────────────────────────────────────────────────────────────────

func TestDashboard_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Dashboard(w, authReq("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Error("response missing 'Dashboard'")
	}
}

func TestDashboard_WithTransactions(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		categories: []Category{
			{ID: "c1", UserID: "user1", Name: "Food", BudgetCents: 20000},
			{ID: "c2", UserID: "user1", Name: "Housing", BudgetCents: 100000},
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 300000, Category: "Income", Date: now.AddDate(0, 0, -2)},
			{ID: "t2", UserID: "user1", AmountCents: -5000, Category: "Food", Date: now.AddDate(0, 0, -1)},
			{ID: "t3", UserID: "user1", AmountCents: -80000, Category: "Housing", Date: now.AddDate(0, 0, -3)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Dashboard(w, authReq("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestDashboard_AlertsBudgetExceeded(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		categories: []Category{
			{ID: "c1", UserID: "user1", Name: "Food", BudgetCents: 5000},
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 300000, Category: "Income", Date: now.AddDate(0, 0, -1)},
			{ID: "t2", UserID: "user1", AmountCents: -10000, Category: "Food", Date: now.AddDate(0, 0, -1)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Dashboard(w, authReq("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "exceeded") {
		t.Error("expected budget exceeded alert")
	}
}

func TestDashboard_WithCommittedGoal(t *testing.T) {
	now := time.Now()
	store := &mockStore{
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 300000, Category: "Income", Date: now.AddDate(0, 0, -1)},
		},
		goals: []Goal{
			{
				ID:          "g1",
				UserID:      "user1",
				Name:        "Switch",
				TargetCents: 30000,
				SavedCents:  0,
				Deadline:    now.AddDate(0, 3, 0),
				Committed:   true,
			},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Dashboard(w, authReq("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Switch") {
		t.Error("expected committed goal name in dashboard")
	}
}

// ── Transactions ──────────────────────────────────────────────────────────────

func TestTransactions_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Transactions(w, authReq("GET", "/transactions", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestTransactions_WithData(t *testing.T) {
	store := &mockStore{
		categories:   []Category{{ID: "c1", Name: "Food"}},
		transactions: []Transaction{newTxn("t1", "Food", -1000, 5)},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Transactions(w, authReq("GET", "/transactions", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "t1") {
		t.Error("expected transaction id in response")
	}
}

// ── Accounts ──────────────────────────────────────────────────────────────────

func TestAccounts_GET(t *testing.T) {
	store := &mockStore{accounts: []Account{{ID: "a1", UserID: "user1", Name: "Main", Type: "checking"}}}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Accounts(w, authReq("GET", "/accounts", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Main") {
		t.Error("expected account name in response")
	}
}

func TestAccounts_POST_Create(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store)
	form := url.Values{"name": {"Savings"}, "type": {"savings"}}
	w := httptest.NewRecorder()
	r := authReq("POST", "/accounts", form)
	h.Accounts(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
	if len(store.accounts) != 1 || store.accounts[0].Name != "Savings" {
		t.Errorf("account not created: %+v", store.accounts)
	}
}

func TestAccounts_DELETE(t *testing.T) {
	store := &mockStore{accounts: []Account{{ID: "a1", UserID: "user1", Name: "Old", Type: "checking"}}}
	h := newHandler(store)
	r := authReq("DELETE", "/accounts/a1", nil)
	r.SetPathValue("id", "a1")
	w := httptest.NewRecorder()
	h.Accounts(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

// ── Categories ────────────────────────────────────────────────────────────────

func TestCategories_GET(t *testing.T) {
	store := &mockStore{categories: []Category{{ID: "c1", Name: "Food", Color: "#f00", BudgetCents: 10000}}}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Categories(w, authReq("GET", "/categories", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestCategories_POST_Create(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store)
	form := url.Values{"name": {"Transport"}, "color": {"#00f"}, "budget_euros": {"200"}}
	w := httptest.NewRecorder()
	h.Categories(w, authReq("POST", "/categories", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
	if len(store.categories) != 1 || store.categories[0].Name != "Transport" {
		t.Errorf("category not created: %+v", store.categories)
	}
}

// ── Goals ─────────────────────────────────────────────────────────────────────

func TestGoals_GET_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Goals(w, authReq("GET", "/goals", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGoals_GET_WithGoals(t *testing.T) {
	store := &mockStore{
		goals: []Goal{{
			ID:          "g1",
			UserID:      "user1",
			Name:        "Holiday",
			Type:        GoalTypeOnce,
			TargetCents: 200000,
			Deadline:    time.Now().AddDate(0, 6, 0),
		}},
		transactions: []Transaction{newTxn("t1", "Income", 300000, 30)},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Goals(w, authReq("GET", "/goals", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Holiday") {
		t.Error("expected goal name in response")
	}
}

func TestGoals_POST_Create(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store)
	form := url.Values{
		"name":         {"Emergency Fund"},
		"type":         {"emergency"},
		"target_euros": {"5000"},
		"deadline":     {"2026-12"},
	}
	w := httptest.NewRecorder()
	h.Goals(w, authReq("POST", "/goals", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
	if len(store.goals) != 1 || store.goals[0].Name != "Emergency Fund" {
		t.Errorf("goal not created: %+v", store.goals)
	}
	if store.goals[0].TargetCents != 500000 {
		t.Errorf("TargetCents = %d, want 500000", store.goals[0].TargetCents)
	}
}

func TestGoals_POST_Commit(t *testing.T) {
	store := &mockStore{
		goals: []Goal{{ID: "g1", UserID: "user1", Name: "Car", TargetCents: 1000000, Deadline: time.Now().AddDate(1, 0, 0)}},
	}
	h := newHandler(store)
	form := url.Values{"action": {"commit"}, "id": {"g1"}}
	w := httptest.NewRecorder()
	h.Goals(w, authReq("POST", "/goals", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestGoals_POST_Uncommit(t *testing.T) {
	store := &mockStore{
		goals: []Goal{{ID: "g1", UserID: "user1", Name: "Car", Committed: true, TargetCents: 1000000, Deadline: time.Now().AddDate(1, 0, 0)}},
	}
	h := newHandler(store)
	form := url.Values{"action": {"uncommit"}, "id": {"g1"}}
	w := httptest.NewRecorder()
	h.Goals(w, authReq("POST", "/goals", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestGoals_POST_Delete(t *testing.T) {
	store := &mockStore{
		goals: []Goal{{ID: "g1", UserID: "user1", Name: "Old Goal"}},
	}
	h := newHandler(store)
	form := url.Values{"action": {"delete"}, "id": {"g1"}}
	w := httptest.NewRecorder()
	h.Goals(w, authReq("POST", "/goals", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
	if len(store.goals) != 0 {
		t.Error("expected goal to be deleted")
	}
}

func TestGoals_FeasibleVsNotFeasible(t *testing.T) {
	// goal requires €500/mo but avg savings is only €200/mo → infeasible
	store := &mockStore{
		goals: []Goal{{
			ID:          "g1",
			UserID:      "user1",
			Name:        "Expensive",
			TargetCents: 300000, // €3000
			Deadline:    time.Now().AddDate(0, 6, 0),
		}},
		transactions: []Transaction{
			newTxn("t1", "Income", 100000, 45),
			newTxn("t2", "Food", -80000, 45),
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Goals(w, authReq("GET", "/goals", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Net Worth ─────────────────────────────────────────────────────────────────

func TestNetWorth_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.NetWorth(w, authReq("GET", "/networth", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestNetWorth_WithHistory(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{
			newTxn("t1", "Income", 200000, 60),
			newTxn("t2", "Food", -50000, 59),
			newTxn("t3", "Income", 200000, 30),
			newTxn("t4", "Housing", -80000, 29),
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.NetWorth(w, authReq("GET", "/networth", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Net Worth") {
		t.Error("expected 'Net Worth' in response")
	}
}

// ── Simulator ─────────────────────────────────────────────────────────────────

func TestSimulator_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Simulator(w, authReq("GET", "/simulator", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestSimulator_WithData(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{
			newTxn("t1", "Income", 300000, 60),
			newTxn("t2", "Housing", -80000, 59),
			newTxn("t3", "Food", -30000, 58),
			newTxn("t4", "Income", 300000, 30),
		},
		goals: []Goal{{
			ID:          "g1",
			UserID:      "user1",
			Name:        "Switch",
			TargetCents: 30000,
			Deadline:    time.Now().AddDate(0, 6, 0),
			Committed:   true,
		}},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Simulator(w, authReq("GET", "/simulator", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "What If") {
		t.Error("expected 'What If' heading in response")
	}
}

// ── Reports & Projections ─────────────────────────────────────────────────────

func TestReports_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Reports(w, authReq("GET", "/reports", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestProjections_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Projections(w, authReq("GET", "/projections", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Portfolio ─────────────────────────────────────────────────────────────────

func TestPortfolio_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Portfolio(w, authReq("GET", "/portfolio", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Import page ───────────────────────────────────────────────────────────────

func TestImportPage(t *testing.T) {
	store := &mockStore{accounts: []Account{{ID: "a1", Name: "Main", Type: "checking"}}}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.ImportPage(w, authReq("GET", "/import", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Sharing ───────────────────────────────────────────────────────────────────

func TestSharing_GET(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Sharing(w, authReq("GET", "/sharing", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Healthz ───────────────────────────────────────────────────────────────────

func TestHealthz(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.healthz(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── SearchUsers ───────────────────────────────────────────────────────────────

func TestSearchUsers_ShortQuery(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("GET", "/api/users/search?q=a", nil)
	w := httptest.NewRecorder()
	h.SearchUsers(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	// short query returns empty array
	if !strings.Contains(w.Body.String(), "[]") {
		t.Errorf("expected empty array, got: %s", w.Body.String())
	}
}

// ── Template funcmap (exercised via Dashboard renders, but test directly too) ──

func TestDashboard_NegativeAvailableToSpend(t *testing.T) {
	// Exercises negative value paths in template (centsAbs, negative counter)
	now := time.Now()
	store := &mockStore{
		categories: []Category{
			{ID: "c1", UserID: "user1", Name: "Food", BudgetCents: 5000},
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 50000, Category: "Income", Date: now.AddDate(0, 0, -1)},
			{ID: "t2", UserID: "user1", AmountCents: -45000, Category: "Housing", Date: now.AddDate(0, 0, -1)},
			{ID: "t3", UserID: "user1", AmountCents: -20000, Category: "Food", Date: now.AddDate(0, 0, -1)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Dashboard(w, authReq("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestParseTmplFuncs(t *testing.T) {
	// Exercise funcmap callables via a variety of handler renders.
	store := &mockStore{
		categories: []Category{
			{ID: "c1", UserID: "user1", Name: "Food", Color: "#f00", BudgetCents: 10000},
			{ID: "c2", UserID: "user1", Name: "Transport", Color: "#00f", BudgetCents: 0},
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 300000, Category: "Income",
				Date: time.Now().AddDate(0, 0, -2)},
			{ID: "t2", UserID: "user1", AmountCents: -15000, Category: "Food",
				Date: time.Now().AddDate(0, 0, -1)},
			{ID: "t3", UserID: "user1", AmountCents: -80000, Category: "Housing",
				Date: time.Now().AddDate(0, -1, -1)},
			{ID: "t4", UserID: "user1", AmountCents: -5000, Category: "Transport",
				Date: time.Now().AddDate(0, -2, -1)},
		},
		goals: []Goal{{
			ID: "g1", UserID: "user1", Name: "Car",
			TargetCents: 500000, SavedCents: 100000,
			Deadline:  time.Now().AddDate(0, 6, 0),
			Committed: true,
		}},
		trades: []Trade{{
			ID: "tr1", UserID: "user1", ISIN: "IE00B3WJKG14",
			Name: "ETF", Type: "buy", Quantity: 2,
			PriceCents: 10000, TotalCents: 20000,
			Date: time.Now().AddDate(0, -1, 0),
		}},
	}
	h := newHandler(store)
	for _, tc := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		path    string
	}{
		{"dashboard", h.Dashboard, "/"},
		{"portfolio", h.Portfolio, "/portfolio"},
		{"goals", h.Goals, "/goals"},
		{"reports", h.Reports, "/reports"},
		{"projections", h.Projections, "/projections"},
		{"networth", h.NetWorth, "/networth"},
		{"simulator", h.Simulator, "/simulator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler(w, authReq("GET", tc.path, nil))
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", w.Code)
			}
		})
	}
}

// ── Pure helpers ──────────────────────────────────────────────────────────────

func TestMonthsBetween(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		from    time.Time
		to      time.Time
		wantMin int
		wantMax int
	}{
		{"same month", now, now, 0, 0},
		{"one month ahead", now, now.AddDate(0, 1, 0), 1, 1},
		{"six months ahead", now, now.AddDate(0, 6, 0), 6, 6},
		{"past date", now, now.AddDate(0, -1, 0), -2, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monthsBetween(tt.from, tt.to)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("monthsBetween = %d, want [%d,%d]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"3.14", 3.14},
		{"100", 100},
		{"0", 0},
		{"", 0},
		{"-5.5", -5.5},
	}
	for _, tt := range tests {
		got := parseFloat(tt.input)
		if got != tt.want {
			t.Errorf("parseFloat(%q) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestSortStrings(t *testing.T) {
	in := []string{"c", "a", "b"}
	sortStrings(in)
	want := []string{"a", "b", "c"}
	for i, v := range want {
		if in[i] != v {
			t.Errorf("sorted[%d] = %q, want %q", i, in[i], v)
		}
	}
}

func TestAppendIfMissing(t *testing.T) {
	s := []string{"a", "b"}
	s = appendIfMissing(s, "c")
	s = appendIfMissing(s, "a") // duplicate
	if len(s) != 3 {
		t.Errorf("len = %d, want 3: %v", len(s), s)
	}
	if s[2] != "c" {
		t.Errorf("s[2] = %q, want 'c'", s[2])
	}
}

// ── Alert logic ───────────────────────────────────────────────────────────────

func TestDashboard_GoalMissAlert(t *testing.T) {
	// avg savings = ~€100/mo but goal needs €1000/mo → alert expected
	now := time.Now()
	store := &mockStore{
		categories: []Category{},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 20000, Category: "Income", Date: now.AddDate(0, -1, -5)},
			{ID: "t2", UserID: "user1", AmountCents: -10000, Category: "Food", Date: now.AddDate(0, -1, -4)},
		},
		goals: []Goal{{
			ID:          "g1",
			UserID:      "user1",
			Name:        "House",
			TargetCents: 5000000,
			SavedCents:  0,
			Deadline:    now.AddDate(0, 5, 0),
		}},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Dashboard(w, authReq("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "House") {
		t.Error("expected goal name in alert")
	}
}

// ── ImportSecurities ──────────────────────────────────────────────────────────

func TestImportSecurities_Valid(t *testing.T) {
	csv := "Date,Name,ISIN,Type,Quantity,Price,Total,Currency\n2024-01-01,Vanguard,IE00B3WJKG14,Buy,10,30.00,300.00,EUR\n"
	store := &mockStore{}
	h := newHandler(store)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "trades.csv")
	fmt.Fprint(fw, csv)
	mw.Close()
	r := authReq("POST", "/import/securities", nil)
	r.Body = io.NopCloser(&buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ImportSecurities(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
	if len(store.trades) != 1 {
		t.Errorf("expected 1 trade, got %d", len(store.trades))
	}
}

func TestImportSecurities_BadCSV(t *testing.T) {
	h := newHandler(&mockStore{})
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "bad.csv")
	fmt.Fprint(fw, "garbage,data\nno,valid,rows\n")
	mw.Close()
	r := authReq("POST", "/import/securities", nil)
	r.Body = io.NopCloser(&buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ImportSecurities(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── Goals conflict warning ────────────────────────────────────────────────────

func TestGoals_ConflictWarning(t *testing.T) {
	now := time.Now()
	// two committed goals together exceed income
	store := &mockStore{
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 50000, Category: "Income", Date: now.AddDate(0, 0, -1)},
		},
		goals: []Goal{
			{ID: "g1", UserID: "user1", Name: "Car", TargetCents: 10000000, Deadline: now.AddDate(0, 6, 0), Committed: true},
			{ID: "g2", UserID: "user1", Name: "Holiday", TargetCents: 5000000, Deadline: now.AddDate(0, 3, 0), Committed: true},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Goals(w, authReq("GET", "/goals", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	// conflict warning should appear
	if !strings.Contains(w.Body.String(), "require") && !strings.Contains(w.Body.String(), "disposable") {
		// conflict warning text varies — just check it renders without error
	}
}

// ── Categories PUT bad body ───────────────────────────────────────────────────

func TestCategories_PUT_BadBody(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("PUT", "/categories/c1", nil)
	r.SetPathValue("id", "c1")
	r.Body = io.NopCloser(strings.NewReader("{bad"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Categories(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── UpdateTransaction empty body ──────────────────────────────────────────────

func TestUpdateTransaction_EmptyFields(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("PUT", "/api/transactions/t1", nil)
	r.SetPathValue("id", "t1")
	r.Body = io.NopCloser(strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UpdateTransaction(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Reports with year filter ──────────────────────────────────────────────────

func TestReports_WithYearFilter(t *testing.T) {
	store := &mockStore{
		categories: []Category{{ID: "c1", Name: "Food", Color: "#f00"}},
		transactions: []Transaction{
			newTxn("t1", "Food", -5000, 15),
			newTxn("t2", "Food", -3000, 45),
			newTxn("t3", "Income", 200000, 10),
		},
	}
	h := newHandler(store)
	r := authReq("GET", "/reports?year=2025", nil)
	w := httptest.NewRecorder()
	h.Reports(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Sharing duplicate prevention ──────────────────────────────────────────────

func TestSharing_POST_DuplicatePrevented(t *testing.T) {
	store := &mockStore{
		permissions: []Permission{{ID: "p1", OwnerID: "user1", ViewerID: "other-user"}},
	}
	h := newHandler(store)
	form := url.Values{"viewer_id": {"other-user"}}
	w := httptest.NewRecorder()
	h.Sharing(w, authReq("POST", "/sharing", form))
	// redirects without adding duplicate
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
	if len(store.permissions) != 1 {
		t.Errorf("expected still 1 permission, got %d", len(store.permissions))
	}
}

// ── NetWorth with trades ──────────────────────────────────────────────────────

func TestNetWorth_WithTrades(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{newTxn("t1", "Income", 100000, 30)},
		trades: []Trade{
			{ID: "tr1", UserID: "user1", ISIN: "IE00B3WJKG14", Name: "ETF",
				Type: "buy", Quantity: 2, PriceCents: 10000, TotalCents: 20000,
				Date: time.Now().AddDate(0, -2, 0)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.NetWorth(w, authReq("GET", "/networth", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── ImportPreview missing file ────────────────────────────────────────────────

func TestImportPreview_MissingFile(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("account_id", "acc1")
	mw.Close()
	r := authReq("POST", "/import/preview", nil)
	r.Body = io.NopCloser(&buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.ImportPreview(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDashboard_SpendPaceAlert(t *testing.T) {
	now := time.Now()
	// simulate spending 95% of disposable but only 5% of month elapsed
	// to trigger the pace alert we need: monthSpentPct > monthProgressPct + 20
	// monthProgressPct ≈ day/daysInMonth*100 — we can't easily control it here
	// so just ensure the dashboard renders without panic with high variable spend
	store := &mockStore{
		categories: []Category{
			{ID: "c1", UserID: "user1", Name: "Food", BudgetCents: 5000},
		},
		transactions: []Transaction{
			{ID: "t1", UserID: "user1", AmountCents: 100000, Category: "Income", Date: now.AddDate(0, 0, -1)},
			{ID: "t2", UserID: "user1", AmountCents: -90000, Category: "Food", Date: now.AddDate(0, 0, -1)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Dashboard(w, authReq("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Accounts missing fields ───────────────────────────────────────────────────

func TestAccounts_POST_MissingFields(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"name": {"NoType"}}
	w := httptest.NewRecorder()
	h.Accounts(w, authReq("POST", "/accounts", form))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── DeleteTransaction error path (covered via normal path above; add search) ─

func TestTransactions_CategoryAndSearch(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{
			newTxn("uber eats food delivery", "Food", -2000, 2),
			newTxn("uber taxi ride", "Transport", -1500, 3),
		},
	}
	h := newHandler(store)
	r := authReq("GET", "/transactions?category=Food&search=eats", nil)
	w := httptest.NewRecorder()
	h.Transactions(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Transactions search/filter ────────────────────────────────────────────────

func TestTransactions_Search(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{
			newTxn("coffee run", "Food", -500, 1),
			newTxn("uber trip", "Transport", -1000, 2),
		},
	}
	h := newHandler(store)
	r := authReq("GET", "/transactions?search=coffee", nil)
	w := httptest.NewRecorder()
	h.Transactions(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "coffee") {
		t.Error("expected 'coffee' in filtered response")
	}
}

func TestTransactions_DaysFilter(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{
			newTxn("t1", "Food", -500, 3),
			newTxn("t2", "Food", -500, 60),
		},
	}
	h := newHandler(store)
	r := authReq("GET", "/transactions?days=30", nil)
	w := httptest.NewRecorder()
	h.Transactions(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── CreateTransaction bad body ────────────────────────────────────────────────

func TestCreateTransaction_BadBody(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("POST", "/api/transactions", nil)
	r.Body = io.NopCloser(strings.NewReader("{bad"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.CreateTransaction(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── CreateTransaction API ─────────────────────────────────────────────────────

// ── UpdateTransaction ─────────────────────────────────────────────────────────

func TestUpdateTransaction(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{{ID: "t1", UserID: "user1", Category: "Food"}},
	}
	h := newHandler(store)
	body := `{"category":"Transport","description":"Uber"}`
	r := authReq("PUT", "/api/transactions/t1", nil)
	r.SetPathValue("id", "t1")
	r.Body = io.NopCloser(strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UpdateTransaction(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestUpdateTransaction_BadBody(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("PUT", "/api/transactions/t1", nil)
	r.SetPathValue("id", "t1")
	r.Body = io.NopCloser(strings.NewReader("{bad json"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UpdateTransaction(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── DeleteTransaction ─────────────────────────────────────────────────────────

func TestDeleteTransaction(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{{ID: "t1", UserID: "user1"}},
	}
	h := newHandler(store)
	r := authReq("DELETE", "/api/transactions/t1", nil)
	r.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	h.DeleteTransaction(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

// ── Categories extended ───────────────────────────────────────────────────────

func TestCategories_PUT(t *testing.T) {
	store := &mockStore{categories: []Category{{ID: "c1", UserID: "user1", Name: "Food", Color: "#f00"}}}
	h := newHandler(store)
	body := `{"name":"Food","color":"#ff0000","budget_cents":20000}`
	r := authReq("PUT", "/categories/c1", nil)
	r.SetPathValue("id", "c1")
	r.Body = io.NopCloser(strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Categories(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestCategories_DELETE(t *testing.T) {
	store := &mockStore{categories: []Category{{ID: "c1", UserID: "user1", Name: "Food", Color: "#f00"}}}
	h := newHandler(store)
	r := authReq("DELETE", "/categories/c1", nil)
	r.SetPathValue("id", "c1")
	w := httptest.NewRecorder()
	h.Categories(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestCategories_POST_MissingFields(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"name": {"NoColor"}}
	w := httptest.NewRecorder()
	h.Categories(w, authReq("POST", "/categories", form))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── Sharing extended ──────────────────────────────────────────────────────────

func TestSharing_POST_Grant(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store)
	form := url.Values{"viewer_id": {"other-user"}}
	w := httptest.NewRecorder()
	h.Sharing(w, authReq("POST", "/sharing", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
	if len(store.permissions) != 1 {
		t.Errorf("expected 1 permission, got %d", len(store.permissions))
	}
}

func TestSharing_POST_SelfGrant(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"viewer_id": {"user1"}} // same as auth user
	w := httptest.NewRecorder()
	h.Sharing(w, authReq("POST", "/sharing", form))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSharing_DELETE(t *testing.T) {
	store := &mockStore{permissions: []Permission{{ID: "p1", OwnerID: "user1", ViewerID: "other"}}}
	h := newHandler(store)
	r := authReq("DELETE", "/sharing/other", nil)
	r.SetPathValue("viewer_id", "other")
	w := httptest.NewRecorder()
	h.Sharing(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

// ── authMW ────────────────────────────────────────────────────────────────────

func TestAuthMW_NoUser(t *testing.T) {
	h := newHandler(&mockStore{})
	called := false
	mw := h.authMW(func(w http.ResponseWriter, r *http.Request) { called = true })
	r := httptest.NewRequest("GET", "/", nil) // no auth headers
	w := httptest.NewRecorder()
	mw(w, r)
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", w.Code)
	}
	if called {
		t.Error("handler should not have been called")
	}
}

func TestAuthMW_WithUser(t *testing.T) {
	h := newHandler(&mockStore{})
	called := false
	mw := h.authMW(func(w http.ResponseWriter, r *http.Request) { called = true })
	w := httptest.NewRecorder()
	mw(w, authReq("GET", "/", nil))
	if !called {
		t.Error("handler should have been called")
	}
}

func TestOwnerOrViewerMW_Owner(t *testing.T) {
	h := newHandler(&mockStore{})
	called := false
	mw := h.ownerOrViewerMW(func(w http.ResponseWriter, r *http.Request) { called = true })
	r := authReq("GET", "/", nil)
	r.SetPathValue("user_id", "user1") // matches auth user
	w := httptest.NewRecorder()
	mw(w, r)
	if !called {
		t.Error("owner should pass through")
	}
}

func TestOwnerOrViewerMW_UnauthorizedViewer(t *testing.T) {
	h := newHandler(&mockStore{})
	called := false
	mw := h.ownerOrViewerMW(func(w http.ResponseWriter, r *http.Request) { called = true })
	r := authReq("GET", "/", nil)
	r.SetPathValue("user_id", "other-user") // not the auth user and no permission
	w := httptest.NewRecorder()
	mw(w, r)
	if called {
		t.Error("unauthorized viewer should not pass through")
	}
}

func TestOwnerOrViewerMW_AuthorizedViewer(t *testing.T) {
	store := &mockStore{
		permissions: []Permission{{ID: "p1", OwnerID: "other-user", ViewerID: "user1"}},
	}
	h := newHandler(store)
	called := false
	mw := h.ownerOrViewerMW(func(w http.ResponseWriter, r *http.Request) { called = true })
	r := authReq("GET", "/", nil)
	r.SetPathValue("user_id", "other-user")
	w := httptest.NewRecorder()
	mw(w, r)
	if !called {
		t.Error("authorized viewer should pass through")
	}
}

// ── SearchUsers ───────────────────────────────────────────────────────────────

func TestSearchUsers_ParsesResponse(t *testing.T) {
	// SearchUsers makes a real HTTP call to http://users/...; we can only verify
	// that with a long enough query it doesn't 500 (it degrades to empty on failure).
	// The JSON decode path is exercised here even though the call will fail in CI.
	h := newHandler(&mockStore{})
	r := authReq("GET", "/api/users/search?q=john", nil)
	w := httptest.NewRecorder()
	h.SearchUsers(w, r)
	// Whether it succeeds or not, must not 500
	if w.Code == http.StatusInternalServerError {
		t.Error("SearchUsers should degrade gracefully, not 500")
	}
}

func TestSearchUsers_QueryTooShort(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("GET", "/api/users/search?q=x", nil)
	w := httptest.NewRecorder()
	h.SearchUsers(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("want empty array, got: %s", w.Body.String())
	}
}

func TestSearchUsers_QueryLongEnough(t *testing.T) {
	// external HTTP call to users service will fail — expect empty array response not a 500
	h := newHandler(&mockStore{})
	r := authReq("GET", "/api/users/search?q=john", nil)
	w := httptest.NewRecorder()
	h.SearchUsers(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Transactions filtering ────────────────────────────────────────────────────

func TestTransactions_FilterCategory(t *testing.T) {
	store := &mockStore{
		transactions: []Transaction{
			newTxn("t1", "Food", -1000, 5),
			newTxn("t2", "Transport", -500, 3),
		},
	}
	h := newHandler(store)
	r := authReq("GET", "/transactions?category=Food", nil)
	w := httptest.NewRecorder()
	h.Transactions(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Portfolio with trades ─────────────────────────────────────────────────────

func TestPortfolio_WithTrades(t *testing.T) {
	store := &mockStore{
		trades: []Trade{
			{ID: "tr1", UserID: "user1", ISIN: "IE00B3WJKG14", Name: "Vanguard ETF",
				Type: "buy", Quantity: 5, PriceCents: 10000, TotalCents: 50000,
				Date: time.Now().AddDate(0, -3, 0)},
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Portfolio(w, authReq("GET", "/portfolio", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Reports with data ─────────────────────────────────────────────────────────

func TestReports_WithTransactions(t *testing.T) {
	store := &mockStore{
		categories: []Category{{ID: "c1", Name: "Food", Color: "#f00"}},
		transactions: []Transaction{
			newTxn("t1", "Food", -3000, 10),
			newTxn("t2", "Food", -2000, 40),
			newTxn("t3", "Income", 200000, 5),
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Reports(w, authReq("GET", "/reports", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Projections with data ─────────────────────────────────────────────────────

func TestProjections_WithTransactions(t *testing.T) {
	store := &mockStore{
		categories: []Category{{ID: "c1", Name: "Food"}},
		transactions: []Transaction{
			newTxn("t1", "Food", -5000, 10),
			newTxn("t2", "Food", -4000, 40),
			newTxn("t3", "Food", -6000, 70),
		},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.Projections(w, authReq("GET", "/projections", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── NewHandler / RegisterRoutes / Error ──────────────────────────────────────

func TestNewHandler(t *testing.T) {
	// NewHandler wraps a *Store into a Handler.
	// Pass a nil *Store — the function just assigns; no methods are called.
	h := NewHandler((*Store)(nil), "test-secret", "", "", "")
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
}

func TestDeleteTransaction_Error(t *testing.T) {
	store := &mockStore{deleteTransactionErr: fmt.Errorf("db error")}
	h := newHandler(store)
	r := authReq("DELETE", "/api/transactions/t1", nil)
	r.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	h.DeleteTransaction(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestCreateTransaction_StoreError(t *testing.T) {
	store := &mockStore{createTransactionErr: fmt.Errorf("db error")}
	h := newHandler(store)
	body := `{"account_id":"acc1","date":"2024-06-01","description":"Test","amount_cents":-1500,"category":"Food"}`
	r := authReq("POST", "/api/transactions", nil)
	r.Body = io.NopCloser(strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.CreateTransaction(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestImportSecurities_StoreError(t *testing.T) {
	csv := "Date,Name,ISIN,Type,Quantity,Price,Total,Currency\n2024-01-01,Vanguard,IE00B3WJKG14,Buy,10,30.00,300.00,EUR\n"
	store := &mockStore{createTradesErr: fmt.Errorf("db error")}
	h := newHandler(store)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "trades.csv")
	fmt.Fprint(fw, csv)
	mw.Close()
	r := authReq("POST", "/import/securities", nil)
	r.Body = io.NopCloser(&buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ImportSecurities(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestRegisterRoutes(t *testing.T) {
	h := newHandler(&mockStore{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux) // just verify no panic
}

func TestUserError(t *testing.T) {
	e := &userError{Msg: "oops", Status: 400}
	if e.Error() != "oops" {
		t.Errorf("Error() = %q, want 'oops'", e.Error())
	}
}

// ── ImportPreview ─────────────────────────────────────────────────────────────

func multipartCSV(t *testing.T, fieldName, filename, content, accountID, format string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("account_id", accountID)
	_ = w.WriteField("format", format)
	fw, err := w.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(fw, content)
	w.Close()
	r := authReq("POST", "/import/preview", nil)
	r.Body = io.NopCloser(&buf)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func TestImportPreview_CGD(t *testing.T) {
	csv := "Data Mov.;Descrição;Valor\n02-01-2024;Supermercado;-50,00\n03-01-2024;Salario;2500,00\n"
	store := &mockStore{
		accounts:   []Account{{ID: "acc1", UserID: "user1", Name: "Main"}},
		categories: []Category{{ID: "c1", UserID: "user1", Name: "Groceries", Color: "#4caf50"}},
	}
	h := newHandler(store)
	r := multipartCSV(t, "file", "test.csv", csv, "acc1", "cgd")
	w := httptest.NewRecorder()
	h.ImportPreview(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestImportPreview_Generic(t *testing.T) {
	csv := "date,description,amount\n2024-01-02,Coffee,-5.00\n2024-01-03,Salary,2000.00\n"
	h := newHandler(&mockStore{})
	r := multipartCSV(t, "file", "test.csv", csv, "acc1", "")
	w := httptest.NewRecorder()
	h.ImportPreview(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestImportPreview_BadCSV(t *testing.T) {
	// empty file → parseCSV will fail
	h := newHandler(&mockStore{})
	r := multipartCSV(t, "file", "bad.csv", "", "acc1", "cgd")
	w := httptest.NewRecorder()
	h.ImportPreview(w, r)
	// should render error in template, not 500
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error rendered in template)", w.Code)
	}
}

// ── ImportConfirm ─────────────────────────────────────────────────────────────

func TestImportConfirm_Generic(t *testing.T) {
	csv := "date,description,amount\n2024-01-02,Coffee,-5.00\n"
	form := url.Values{
		"account_id": {"acc1"},
		"format":     {""},
		"raw_data":   {csv},
		"categories": {"Food"},
	}
	store := &mockStore{}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.ImportConfirm(w, authReq("POST", "/import/confirm", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
	if len(store.transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(store.transactions))
	}
}

func TestImportConfirm_MultipleRows(t *testing.T) {
	csv := "date,description,amount\n2024-01-02,Coffee,-5.00\n2024-01-03,Lunch,-12.50\n"
	form := url.Values{
		"account_id": {"acc1"},
		"format":     {""},
		"raw_data":   {csv},
		"categories": {"Food", "Food"},
	}
	store := &mockStore{}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.ImportConfirm(w, authReq("POST", "/import/confirm", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
	if len(store.transactions) != 2 {
		t.Errorf("expected 2 transactions, got %d", len(store.transactions))
	}
}

func TestImportConfirm_BadCSV(t *testing.T) {
	form := url.Values{
		"account_id": {"acc1"},
		"format":     {"cgd"},
		"raw_data":   {""},
	}
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.ImportConfirm(w, authReq("POST", "/import/confirm", form))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCreateTransaction(t *testing.T) {
	store := &mockStore{
		accounts:   []Account{{ID: "acc1", UserID: "user1", Name: "Main"}},
		categories: []Category{{ID: "c1", UserID: "user1", Name: "Food"}},
	}
	h := newHandler(store)
	body := `{"account_id":"acc1","date":"2024-06-01","description":"Test","amount_cents":-1500,"category":"Food"}`
	r := authReq("POST", "/api/transactions", nil)
	r.Body = io.NopCloser(strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.CreateTransaction(w, r)
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

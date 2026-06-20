//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	homemongo "homelab/pkg/mongo"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ── Test harness ──────────────────────────────────────────────────────────────

var integrationStore *Store

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		panic(fmt.Sprintf("start mongo container: %v", err))
	}
	defer container.Terminate(ctx) //nolint:errcheck

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Sprintf("get connection string: %v", err))
	}

	// The homelab mongo package reads MONGO_URI and MONGO_DB from env.
	os.Setenv("MONGO_URI", uri)
	os.Setenv("MONGO_DB", "test_finance")

	db, err := homemongo.Connect(ctx)
	if err != nil {
		panic(fmt.Sprintf("connect to test mongo: %v", err))
	}
	defer db.Close(ctx) //nolint:errcheck

	integrationStore = NewStore(db)
	integrationStore.ensureAuthIndexes(ctx)

	m.Run()
}

// drop wipes the given collections for test isolation.
func drop(t *testing.T, colls ...string) {
	t.Helper()
	ctx := context.Background()
	for _, c := range colls {
		if err := integrationStore.db.Collection(c).Drop(ctx); err != nil {
			t.Fatalf("drop %s: %v", c, err)
		}
	}
}

// ── Accounts ──────────────────────────────────────────────────────────────────

func TestStore_Accounts(t *testing.T) {
	drop(t, "finance_accounts")
	ctx := context.Background()

	a := &Account{ID: "acc1", UserID: "u1", Name: "Checking", Type: "checking"}
	if err := integrationStore.createAccount(ctx, a); err != nil {
		t.Fatalf("createAccount: %v", err)
	}

	got, err := integrationStore.getAccounts(ctx, "u1")
	if err != nil || len(got) != 1 || got[0].Name != "Checking" {
		t.Fatalf("getAccounts: len=%d err=%v", len(got), err)
	}

	acc, err := integrationStore.getAccount(ctx, "acc1")
	if err != nil || acc == nil || acc.Type != "checking" {
		t.Fatalf("getAccount: %v %v", acc, err)
	}

	// isolation: another user sees nothing
	none, _ := integrationStore.getAccounts(ctx, "other")
	if len(none) != 0 {
		t.Fatalf("expected no accounts for other user, got %d", len(none))
	}

	if err := integrationStore.deleteAccount(ctx, "acc1", "u1"); err != nil {
		t.Fatalf("deleteAccount: %v", err)
	}
	empty, _ := integrationStore.getAccounts(ctx, "u1")
	if len(empty) != 0 {
		t.Fatalf("expected 0 accounts after delete, got %d", len(empty))
	}
}

// ── Categories ────────────────────────────────────────────────────────────────

func TestStore_Categories(t *testing.T) {
	drop(t, "finance_categories")
	ctx := context.Background()

	c := &Category{ID: "cat1", UserID: "u1", Name: "Food", Color: "#FF6384", BudgetCents: 30000}
	if err := integrationStore.createCategory(ctx, c); err != nil {
		t.Fatalf("createCategory: %v", err)
	}

	cats, err := integrationStore.getCategories(ctx, "u1")
	if err != nil || len(cats) != 1 {
		t.Fatalf("getCategories: %v %v", cats, err)
	}

	c.Name = "Groceries"
	c.BudgetCents = 40000
	if err := integrationStore.updateCategory(ctx, c); err != nil {
		t.Fatalf("updateCategory: %v", err)
	}
	updated, _ := integrationStore.getCategories(ctx, "u1")
	if updated[0].Name != "Groceries" || updated[0].BudgetCents != 40000 {
		t.Fatalf("update not applied: %+v", updated[0])
	}

	if err := integrationStore.deleteCategory(ctx, "cat1", "u1"); err != nil {
		t.Fatalf("deleteCategory: %v", err)
	}
	empty, _ := integrationStore.getCategories(ctx, "u1")
	if len(empty) != 0 {
		t.Fatal("expected 0 categories after delete")
	}
}

func TestStore_SeedCategories(t *testing.T) {
	drop(t, "finance_categories")
	ctx := context.Background()

	if err := integrationStore.seedCategories(ctx, "u_seed"); err != nil {
		t.Fatalf("seedCategories: %v", err)
	}
	cats, _ := integrationStore.getCategories(ctx, "u_seed")
	if len(cats) == 0 {
		t.Fatal("expected seeded categories, got none")
	}
	first := len(cats)

	// second call is idempotent
	if err := integrationStore.seedCategories(ctx, "u_seed"); err != nil {
		t.Fatalf("seedCategories second call: %v", err)
	}
	cats2, _ := integrationStore.getCategories(ctx, "u_seed")
	if len(cats2) != first {
		t.Fatalf("idempotency broken: first=%d second=%d", first, len(cats2))
	}
}

// ── Transactions ──────────────────────────────────────────────────────────────

func TestStore_Transactions(t *testing.T) {
	drop(t, "finance_transactions")
	ctx := context.Background()
	now := time.Now()

	txns := []Transaction{
		{ID: "t1", UserID: "u1", AmountCents: 200000, Category: "Income", Date: now.AddDate(0, -1, 0)},
		{ID: "t2", UserID: "u1", AmountCents: -50000, Category: "Food", Date: now.AddDate(0, -1, -5), GoalID: "g1"},
		{ID: "t3", UserID: "u2", AmountCents: 10000, Category: "Income", Date: now},
	}
	if err := integrationStore.createTransactions(ctx, txns); err != nil {
		t.Fatalf("createTransactions: %v", err)
	}

	// list for u1, sorted desc by date
	got, err := integrationStore.getTransactions(ctx, "u1", bson.M{})
	if err != nil || len(got) != 2 {
		t.Fatalf("getTransactions u1: len=%d err=%v", len(got), err)
	}
	if got[0].ID != "t1" {
		t.Fatalf("sort order wrong: first=%s, want t1", got[0].ID)
	}

	// get by id
	tx, err := integrationStore.getTransaction(ctx, "t2", "u1")
	if err != nil || tx == nil || tx.Category != "Food" {
		t.Fatalf("getTransaction: %v %v", tx, err)
	}

	// category filter
	filtered, _ := integrationStore.getTransactions(ctx, "u1", bson.M{"category": "Food"})
	if len(filtered) != 1 || filtered[0].ID != "t2" {
		t.Fatalf("filter by category: %v", filtered)
	}

	// update
	if err := integrationStore.updateTransaction(ctx, "t1", "u1", bson.M{"category": "Salary"}); err != nil {
		t.Fatalf("updateTransaction: %v", err)
	}
	upd, _ := integrationStore.getTransaction(ctx, "t1", "u1")
	if upd.Category != "Salary" {
		t.Fatalf("update not applied: %+v", upd)
	}

	// delete
	if err := integrationStore.deleteTransaction(ctx, "t2", "u1"); err != nil {
		t.Fatalf("deleteTransaction: %v", err)
	}
	after, _ := integrationStore.getTransactions(ctx, "u1", bson.M{})
	if len(after) != 1 {
		t.Fatalf("expected 1 txn after delete, got %d", len(after))
	}
}

func TestStore_AggregateTransactions(t *testing.T) {
	drop(t, "finance_transactions")
	ctx := context.Background()
	now := time.Now()

	txns := []Transaction{
		{ID: "ag1", UserID: "u1", AmountCents: 100000, Category: "Income", Date: now},
		{ID: "ag2", UserID: "u1", AmountCents: 200000, Category: "Income", Date: now},
		{ID: "ag3", UserID: "u1", AmountCents: -30000, Category: "Food", Date: now},
	}
	integrationStore.createTransactions(ctx, txns) //nolint:errcheck

	pipeline := bson.A{
		bson.M{"$group": bson.M{"_id": "$category", "total": bson.M{"$sum": "$amount_cents"}}},
	}
	results, err := integrationStore.aggregateTransactions(ctx, "u1", pipeline)
	if err != nil || len(results) == 0 {
		t.Fatalf("aggregateTransactions: results=%d err=%v", len(results), err)
	}
}

func TestStore_GoalFundedCents(t *testing.T) {
	drop(t, "finance_transactions")
	ctx := context.Background()
	now := time.Now()

	txns := []Transaction{
		{ID: "gf1", UserID: "u1", AmountCents: -50000, Category: "Goals", GoalID: "g1", Date: now},
		{ID: "gf2", UserID: "u1", AmountCents: -30000, Category: "Goals", GoalID: "g1", Date: now},
		{ID: "gf3", UserID: "u1", AmountCents: -20000, Category: "Goals", GoalID: "g2", Date: now},
		{ID: "gf4", UserID: "u1", AmountCents: 10000, Category: "Income", Date: now}, // positive: excluded
	}
	integrationStore.createTransactions(ctx, txns) //nolint:errcheck

	funds, err := integrationStore.getGoalFundedCentsAll(ctx, "u1")
	if err != nil {
		t.Fatalf("getGoalFundedCentsAll: %v", err)
	}
	if funds["g1"] != 80000 {
		t.Errorf("g1 funded=%d, want 80000", funds["g1"])
	}
	if funds["g2"] != 20000 {
		t.Errorf("g2 funded=%d, want 20000", funds["g2"])
	}

	goalTxns, err := integrationStore.getGoalTransactions(ctx, "u1", "g1")
	if err != nil || len(goalTxns) != 2 {
		t.Fatalf("getGoalTransactions: len=%d err=%v", len(goalTxns), err)
	}
}

// ── Goals ─────────────────────────────────────────────────────────────────────

func TestStore_Goals(t *testing.T) {
	drop(t, "finance_goals")
	ctx := context.Background()
	now := time.Now()

	g := &Goal{
		ID:          "goal1",
		UserID:      "u1",
		Name:        "Emergency Fund",
		Type:        GoalTypeOnce,
		TargetCents: 1000000,
		Deadline:    now.AddDate(1, 0, 0),
		CreatedAt:   now,
	}
	if err := integrationStore.createGoal(ctx, g); err != nil {
		t.Fatalf("createGoal: %v", err)
	}

	goals, err := integrationStore.getGoals(ctx, "u1")
	if err != nil || len(goals) != 1 || goals[0].Name != "Emergency Fund" {
		t.Fatalf("getGoals: %v %v", goals, err)
	}

	if err := integrationStore.updateGoal(ctx, "goal1", "u1", bson.M{"committed": true}); err != nil {
		t.Fatalf("updateGoal: %v", err)
	}
	updated, _ := integrationStore.getGoals(ctx, "u1")
	if !updated[0].Committed {
		t.Fatal("goal not committed after update")
	}

	if err := integrationStore.deleteGoal(ctx, "goal1", "u1"); err != nil {
		t.Fatalf("deleteGoal: %v", err)
	}
	empty, _ := integrationStore.getGoals(ctx, "u1")
	if len(empty) != 0 {
		t.Fatal("expected 0 goals after delete")
	}
}

// ── Trades ────────────────────────────────────────────────────────────────────

func TestStore_Trades(t *testing.T) {
	drop(t, "finance_trades")
	ctx := context.Background()
	now := time.Now()

	trades := []Trade{
		{ID: "tr1", UserID: "u1", ISIN: "IE00B3WJKG14", Name: "MSCI World",
			Type: "buy", Quantity: 10, PriceCents: 4500, TotalCents: 45000, Date: now},
		{ID: "tr2", UserID: "u1", ISIN: "IE00B3WJKG14", Name: "MSCI World",
			Type: "sell", Quantity: 2, PriceCents: 5000, TotalCents: 10000, Date: now.AddDate(0, 1, 0)},
	}
	if err := integrationStore.createTrades(ctx, trades); err != nil {
		t.Fatalf("createTrades: %v", err)
	}

	got, err := integrationStore.getTrades(ctx, "u1")
	if err != nil || len(got) != 2 {
		t.Fatalf("getTrades: len=%d err=%v", len(got), err)
	}
	// sorted ascending by date
	if got[0].ID != "tr1" {
		t.Fatalf("sort order wrong: first=%s, want tr1", got[0].ID)
	}

	if err := integrationStore.deleteTrade(ctx, "tr1", "u1"); err != nil {
		t.Fatalf("deleteTrade: %v", err)
	}
	after, _ := integrationStore.getTrades(ctx, "u1")
	if len(after) != 1 || after[0].ID != "tr2" {
		t.Fatalf("expected 1 trade after delete, got %v", after)
	}
}

// ── Permissions ───────────────────────────────────────────────────────────────

func TestStore_Permissions(t *testing.T) {
	drop(t, "finance_permissions")
	ctx := context.Background()
	now := time.Now()

	p := &Permission{ID: "perm1", OwnerID: "u1", ViewerID: "u2", CreatedAt: now}
	if err := integrationStore.createPermission(ctx, p); err != nil {
		t.Fatalf("createPermission: %v", err)
	}

	perms, err := integrationStore.getPermissions(ctx, "u1")
	if err != nil || len(perms) != 1 || perms[0].ViewerID != "u2" {
		t.Fatalf("getPermissions: %v %v", perms, err)
	}

	viewers, err := integrationStore.getGrantedViewers(ctx, "u2")
	if err != nil || len(viewers) != 1 || viewers[0].OwnerID != "u1" {
		t.Fatalf("getGrantedViewers: %v %v", viewers, err)
	}

	if err := integrationStore.deletePermission(ctx, "u1", "u2"); err != nil {
		t.Fatalf("deletePermission: %v", err)
	}
	empty, _ := integrationStore.getPermissions(ctx, "u1")
	if len(empty) != 0 {
		t.Fatal("expected 0 permissions after delete")
	}
}

// ── Ticker mappings ───────────────────────────────────────────────────────────

func TestStore_TickerMappings(t *testing.T) {
	drop(t, "finance_ticker_mappings")
	ctx := context.Background()

	if err := integrationStore.saveTickerMapping(ctx, "u1", "IE00B3WJKG14", "SWDA.L"); err != nil {
		t.Fatalf("saveTickerMapping: %v", err)
	}
	mappings, err := integrationStore.getTickerMappings(ctx, "u1")
	if err != nil || len(mappings) == 0 {
		t.Fatalf("getTickerMappings: %v %v", mappings, err)
	}

	// upsert: same ISIN, different ticker
	if err := integrationStore.saveTickerMapping(ctx, "u1", "IE00B3WJKG14", "IWDA.AS"); err != nil {
		t.Fatalf("saveTickerMapping upsert: %v", err)
	}
	mappings2, _ := integrationStore.getTickerMappings(ctx, "u1")
	if len(mappings2) != 1 {
		t.Fatalf("expected 1 mapping after upsert, got %d", len(mappings2))
	}
}

// ── Household ─────────────────────────────────────────────────────────────────

func TestStore_Household(t *testing.T) {
	drop(t, "finance_households")
	ctx := context.Background()

	h := &Household{ID: "hh1", OwnerID: "u1", PartnerID: "u2"}
	if err := integrationStore.createHousehold(ctx, h); err != nil {
		t.Fatalf("createHousehold: %v", err)
	}

	// owner finds it
	got, err := integrationStore.getHousehold(ctx, "u1")
	if err != nil || got == nil || got.PartnerID != "u2" {
		t.Fatalf("getHousehold(owner): %v %v", got, err)
	}

	// partner also finds it
	got2, err := integrationStore.getHousehold(ctx, "u2")
	if err != nil || got2 == nil || got2.OwnerID != "u1" {
		t.Fatalf("getHousehold(partner): %v %v", got2, err)
	}

	if err := integrationStore.deleteHousehold(ctx, "u1"); err != nil {
		t.Fatalf("deleteHousehold: %v", err)
	}
	_, err = integrationStore.getHousehold(ctx, "u1")
	if err == nil {
		t.Fatal("expected error for missing household")
	}
}

// ── Import schedules ──────────────────────────────────────────────────────────

func TestStore_ImportSchedules(t *testing.T) {
	drop(t, "finance_import_schedules")
	ctx := context.Background()

	sched := &ImportSchedule{
		ID: "sched1", UserID: "u1", AccountID: "acc1",
		Label: "CGD Monthly", Format: "cgd", Active: true,
	}
	if err := integrationStore.createImportSchedule(ctx, sched); err != nil {
		t.Fatalf("createImportSchedule: %v", err)
	}

	schedules, err := integrationStore.getImportSchedules(ctx, "u1")
	if err != nil || len(schedules) != 1 || schedules[0].Format != "cgd" {
		t.Fatalf("getImportSchedules: %v %v", schedules, err)
	}

	if err := integrationStore.deleteImportSchedule(ctx, "sched1", "u1"); err != nil {
		t.Fatalf("deleteImportSchedule: %v", err)
	}
	empty, _ := integrationStore.getImportSchedules(ctx, "u1")
	if len(empty) != 0 {
		t.Fatal("expected 0 schedules after delete")
	}
}

// ── Auth users & sessions ─────────────────────────────────────────────────────

func TestStore_AuthUsers(t *testing.T) {
	drop(t, "finance_users", "finance_sessions")
	ctx := context.Background()

	u := &AuthUser{Email: "alice@example.com", Name: "Alice", PasswordHash: "hash"}
	if err := integrationStore.createAuthUser(ctx, u); err != nil {
		t.Fatalf("createAuthUser: %v", err)
	}
	if u.ID.IsZero() {
		t.Fatal("expected ID to be set after createAuthUser")
	}

	found, err := integrationStore.findAuthUserByEmail(ctx, "alice@example.com")
	if err != nil || found == nil || found.Name != "Alice" {
		t.Fatalf("findAuthUserByEmail: %v %v", found, err)
	}

	found2, err := integrationStore.findAuthUserByID(ctx, u.ID.Hex())
	if err != nil || found2 == nil {
		t.Fatalf("findAuthUserByID: %v %v", found2, err)
	}

	notFound, err := integrationStore.findAuthUserByEmail(ctx, "nobody@example.com")
	if err != nil || notFound != nil {
		t.Fatalf("expected nil for unknown email, got %v %v", notFound, err)
	}

	// google provider
	u2 := &AuthUser{Email: "bob@gmail.com", Provider: "google", ProviderID: "g-123"}
	integrationStore.createAuthUser(ctx, u2) //nolint:errcheck
	g, err := integrationStore.findAuthUserByProvider(ctx, "google", "g-123")
	if err != nil || g == nil || g.Email != "bob@gmail.com" {
		t.Fatalf("findAuthUserByProvider: %v %v", g, err)
	}
}

func TestStore_AuthSessions(t *testing.T) {
	drop(t, "finance_users", "finance_sessions")
	ctx := context.Background()

	u := &AuthUser{Email: "sess@example.com", Name: "Sess"}
	if err := integrationStore.createAuthUser(ctx, u); err != nil {
		t.Fatalf("createAuthUser: %v", err)
	}

	sess := &AuthSession{
		UserID:    u.ID,
		Email:     u.Email,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := integrationStore.createAuthSession(ctx, sess); err != nil {
		t.Fatalf("createAuthSession: %v", err)
	}
	if sess.ID.IsZero() {
		t.Fatal("expected session ID to be set")
	}

	got, err := integrationStore.getAuthSession(ctx, sess.ID.Hex())
	if err != nil || got == nil || got.Email != "sess@example.com" {
		t.Fatalf("getAuthSession: %v %v", got, err)
	}

	sessions, err := integrationStore.getSessionsByUserID(ctx, u.ID.Hex())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("getSessionsByUserID: len=%d err=%v", len(sessions), err)
	}

	if err := integrationStore.deleteSessionForUser(ctx, sess.ID.Hex(), u.ID.Hex()); err != nil {
		t.Fatalf("deleteSessionForUser: %v", err)
	}
	empty, _ := integrationStore.getSessionsByUserID(ctx, u.ID.Hex())
	if len(empty) != 0 {
		t.Fatal("expected 0 sessions after deleteSessionForUser")
	}
}

func TestStore_DeleteAuthSession(t *testing.T) {
	drop(t, "finance_users", "finance_sessions")
	ctx := context.Background()

	u := &AuthUser{Email: "del@example.com"}
	integrationStore.createAuthUser(ctx, u) //nolint:errcheck

	sess := &AuthSession{UserID: u.ID, Email: u.Email, ExpiresAt: time.Now().Add(time.Hour)}
	integrationStore.createAuthSession(ctx, sess) //nolint:errcheck

	if err := integrationStore.deleteAuthSession(ctx, sess.ID.Hex()); err != nil {
		t.Fatalf("deleteAuthSession: %v", err)
	}
	got, _ := integrationStore.getAuthSession(ctx, sess.ID.Hex())
	if got != nil {
		t.Fatal("session still exists after delete")
	}
}

func TestStore_DeleteAllUserData(t *testing.T) {
	drop(t,
		"finance_users", "finance_sessions", "finance_accounts", "finance_categories",
		"finance_transactions", "finance_trades", "finance_goals", "finance_properties",
		"finance_loans", "finance_permissions", "finance_households",
	)
	ctx := context.Background()

	u := &AuthUser{Email: "purge@example.com", Name: "Purge"}
	integrationStore.createAuthUser(ctx, u) //nolint:errcheck
	uid := u.ID.Hex()

	integrationStore.createAccount(ctx, &Account{ID: "da1", UserID: uid, Name: "Main", Type: "checking"})        //nolint:errcheck
	integrationStore.createGoal(ctx, &Goal{ID: "dg1", UserID: uid, Name: "Emergency", TargetCents: 100000})      //nolint:errcheck
	integrationStore.createProperty(ctx, &Property{ID: "dp1", UserID: uid, Name: "Home", Status: PropertyOwned}) //nolint:errcheck
	integrationStore.createHousehold(ctx, &Household{ID: "dhh1", OwnerID: uid, PartnerID: "other"})              //nolint:errcheck
	integrationStore.createAuthSession(ctx, &AuthSession{                                                         //nolint:errcheck
		UserID: u.ID, Email: u.Email, ExpiresAt: time.Now().Add(time.Hour),
	})

	if err := integrationStore.deleteAllUserData(ctx, uid); err != nil {
		t.Fatalf("deleteAllUserData: %v", err)
	}

	notFound, _ := integrationStore.findAuthUserByEmail(ctx, "purge@example.com")
	if notFound != nil {
		t.Fatal("user still exists after deleteAllUserData")
	}
	accs, _ := integrationStore.getAccounts(ctx, uid)
	if len(accs) != 0 {
		t.Fatalf("expected 0 accounts after purge, got %d", len(accs))
	}
	goals, _ := integrationStore.getGoals(ctx, uid)
	if len(goals) != 0 {
		t.Fatalf("expected 0 goals after purge, got %d", len(goals))
	}
}

// ── Properties & Loans ────────────────────────────────────────────────────────

func TestStore_Properties(t *testing.T) {
	drop(t, "finance_properties")
	ctx := context.Background()
	now := time.Now()

	p := &Property{
		ID: "prop1", UserID: "u1", Name: "Main Residence",
		Status: PropertyOwned, CurrentValueCents: 30000000,
		PurchasePriceCents: 25000000, PurchaseDate: now.AddDate(-5, 0, 0),
	}
	if err := integrationStore.createProperty(ctx, p); err != nil {
		t.Fatalf("createProperty: %v", err)
	}

	props, err := integrationStore.getProperties(ctx, "u1")
	if err != nil || len(props) != 1 || props[0].Name != "Main Residence" {
		t.Fatalf("getProperties: %v %v", props, err)
	}

	prop, err := integrationStore.getProperty(ctx, "prop1", "u1")
	if err != nil || prop == nil {
		t.Fatalf("getProperty: %v %v", prop, err)
	}

	if err := integrationStore.updateProperty(ctx, "prop1", "u1", bson.M{"current_value_cents": int64(32000000)}); err != nil {
		t.Fatalf("updateProperty: %v", err)
	}
	updated, _ := integrationStore.getProperty(ctx, "prop1", "u1")
	if updated.CurrentValueCents != 32000000 {
		t.Fatalf("update not applied: %d", updated.CurrentValueCents)
	}

	if err := integrationStore.deleteProperty(ctx, "prop1", "u1"); err != nil {
		t.Fatalf("deleteProperty: %v", err)
	}
	empty, _ := integrationStore.getProperties(ctx, "u1")
	if len(empty) != 0 {
		t.Fatal("expected 0 properties after delete")
	}
}

func TestStore_Loans(t *testing.T) {
	drop(t, "finance_loans")
	ctx := context.Background()
	now := time.Now()

	l := &Loan{
		ID: "loan1", UserID: "u1", PropertyID: "prop1", Name: "Mortgage",
		Type: LoanMortgage, Status: LoanActive,
		PrincipalCents: 20000000, BalanceCents: 18000000,
		InterestRatePct: 3.5, TermMonths: 360,
		MonthlyPaymentCents: 100000, StartDate: now.AddDate(-2, 0, 0),
	}
	if err := integrationStore.createLoan(ctx, l); err != nil {
		t.Fatalf("createLoan: %v", err)
	}

	loans, err := integrationStore.getLoans(ctx, "u1")
	if err != nil || len(loans) != 1 || loans[0].Name != "Mortgage" {
		t.Fatalf("getLoans: %v %v", loans, err)
	}

	loan, err := integrationStore.getLoan(ctx, "loan1", "u1")
	if err != nil || loan == nil || loan.Type != LoanMortgage {
		t.Fatalf("getLoan: %v %v", loan, err)
	}

	if err := integrationStore.updateLoan(ctx, "loan1", "u1", bson.M{"balance_cents": int64(17500000)}); err != nil {
		t.Fatalf("updateLoan: %v", err)
	}
	updated, _ := integrationStore.getLoan(ctx, "loan1", "u1")
	if updated.BalanceCents != 17500000 {
		t.Fatalf("update not applied: %d", updated.BalanceCents)
	}

	if err := integrationStore.deleteLoan(ctx, "loan1", "u1"); err != nil {
		t.Fatalf("deleteLoan: %v", err)
	}
	empty, _ := integrationStore.getLoans(ctx, "u1")
	if len(empty) != 0 {
		t.Fatal("expected 0 loans after delete")
	}
}

// ── Org: orgs, teams, members, invites ────────────────────────────────────────

func TestStore_Orgs(t *testing.T) {
	drop(t, "org_organizations", "org_members")
	ctx := context.Background()
	now := time.Now()

	o := &Org{ID: "org1", Name: "Acme Corp", Slug: "acme", OwnerUserID: "u1", CreatedAt: now}
	if err := integrationStore.createOrg(ctx, o); err != nil {
		t.Fatalf("createOrg: %v", err)
	}

	got, err := integrationStore.getOrg(ctx, "org1")
	if err != nil || got == nil || got.Slug != "acme" {
		t.Fatalf("getOrg: %v %v", got, err)
	}

	got2, err := integrationStore.getOrgBySlug(ctx, "acme")
	if err != nil || got2 == nil || got2.ID != "org1" {
		t.Fatalf("getOrgBySlug: %v %v", got2, err)
	}

	exists, err := integrationStore.slugExists(ctx, "acme")
	if err != nil || !exists {
		t.Fatalf("slugExists: exists=%v err=%v", exists, err)
	}
	absent, _ := integrationStore.slugExists(ctx, "nope")
	if absent {
		t.Fatal("slug 'nope' should not exist")
	}

	// add a member so getOrgsForUser works
	m := &OrgMember{ID: "mem1", OrgID: "org1", UserID: "u1", Email: "u1@test.com", Role: OrgRoleAdmin, CreatedAt: now}
	if err := integrationStore.createMember(ctx, m); err != nil {
		t.Fatalf("createMember: %v", err)
	}
	orgs, err := integrationStore.getOrgsForUser(ctx, "u1")
	if err != nil || len(orgs) != 1 || orgs[0].Org.Slug != "acme" {
		t.Fatalf("getOrgsForUser: %v %v", orgs, err)
	}
}

func TestStore_Teams(t *testing.T) {
	drop(t, "org_teams")
	ctx := context.Background()
	now := time.Now()

	team := &OrgTeam{ID: "team1", OrgID: "org1", Name: "Events", Type: TeamTypeInternal, CreatedAt: now}
	if err := integrationStore.createTeam(ctx, team); err != nil {
		t.Fatalf("createTeam: %v", err)
	}

	teams, err := integrationStore.getTeams(ctx, "org1")
	if err != nil || len(teams) != 1 || teams[0].Name != "Events" {
		t.Fatalf("getTeams: %v %v", teams, err)
	}

	got, err := integrationStore.getTeam(ctx, "team1", "org1")
	if err != nil || got == nil {
		t.Fatalf("getTeam: %v %v", got, err)
	}

	if err := integrationStore.deleteTeam(ctx, "team1", "org1"); err != nil {
		t.Fatalf("deleteTeam: %v", err)
	}
	empty, _ := integrationStore.getTeams(ctx, "org1")
	if len(empty) != 0 {
		t.Fatal("expected 0 teams after delete")
	}
}

func TestStore_Members(t *testing.T) {
	drop(t, "org_members")
	ctx := context.Background()
	now := time.Now()

	m := &OrgMember{ID: "m1", OrgID: "org1", UserID: "u1", Email: "u1@t.com", Role: OrgRoleMember, CreatedAt: now}
	if err := integrationStore.createMember(ctx, m); err != nil {
		t.Fatalf("createMember: %v", err)
	}

	members, err := integrationStore.getMembers(ctx, "org1")
	if err != nil || len(members) != 1 {
		t.Fatalf("getMembers: %v %v", members, err)
	}

	got, err := integrationStore.getMember(ctx, "org1", "u1")
	if err != nil || got == nil || got.Role != OrgRoleMember {
		t.Fatalf("getMember: %v %v", got, err)
	}

	if err := integrationStore.updateMemberRole(ctx, "m1", "org1", OrgRoleFinance); err != nil {
		t.Fatalf("updateMemberRole: %v", err)
	}
	updated, _ := integrationStore.getMember(ctx, "org1", "u1")
	if updated.Role != OrgRoleFinance {
		t.Fatalf("role not updated: %v", updated.Role)
	}

	if err := integrationStore.removeMember(ctx, "m1", "org1"); err != nil {
		t.Fatalf("removeMember: %v", err)
	}
	empty, _ := integrationStore.getMembers(ctx, "org1")
	if len(empty) != 0 {
		t.Fatal("expected 0 members after remove")
	}
}

func TestStore_Invites(t *testing.T) {
	drop(t, "org_invites")
	ctx := context.Background()
	now := time.Now()

	inv := &OrgInvite{
		ID: "inv1", OrgID: "org1", Email: "new@test.com",
		Role: OrgRoleMember, Token: "tok-abc123",
		ExpiresAt: now.Add(48 * time.Hour), CreatedAt: now,
	}
	if err := integrationStore.createInvite(ctx, inv); err != nil {
		t.Fatalf("createInvite: %v", err)
	}

	invites, err := integrationStore.getInvites(ctx, "org1")
	if err != nil || len(invites) != 1 {
		t.Fatalf("getInvites: %v %v", invites, err)
	}

	got, err := integrationStore.getInviteByToken(ctx, "tok-abc123")
	if err != nil || got == nil || got.Email != "new@test.com" {
		t.Fatalf("getInviteByToken: %v %v", got, err)
	}

	if err := integrationStore.consumeInvite(ctx, "inv1"); err != nil {
		t.Fatalf("consumeInvite: %v", err)
	}
	// getInviteByToken filters out consumed invites — nil means it was correctly marked used.
	consumed, _ := integrationStore.getInviteByToken(ctx, "tok-abc123")
	if consumed != nil {
		t.Fatal("consumed invite should no longer be findable by token")
	}

	// revoke a second invite
	inv2 := &OrgInvite{ID: "inv2", OrgID: "org1", Token: "tok-xyz", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	integrationStore.createInvite(ctx, inv2) //nolint:errcheck
	if err := integrationStore.revokeInvite(ctx, "inv2", "org1"); err != nil {
		t.Fatalf("revokeInvite: %v", err)
	}
	all, _ := integrationStore.getInvites(ctx, "org1")
	for _, i := range all {
		if i.ID == "inv2" {
			t.Fatal("revoked invite still in list")
		}
	}
}

// ── Fiscal years ──────────────────────────────────────────────────────────────

func TestStore_FiscalYears(t *testing.T) {
	drop(t, "org_fiscal_years")
	ctx := context.Background()
	now := time.Now()

	fy := &FiscalYear{
		ID: "fy2025", OrgID: "org1", Label: "2025",
		Status:    FiscalYearDraft,
		StartDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
		CreatedAt: now,
	}
	if err := integrationStore.createFiscalYear(ctx, fy); err != nil {
		t.Fatalf("createFiscalYear: %v", err)
	}

	years, err := integrationStore.getFiscalYears(ctx, "org1")
	if err != nil || len(years) != 1 || years[0].Label != "2025" {
		t.Fatalf("getFiscalYears: %v %v", years, err)
	}

	year, err := integrationStore.getFiscalYear(ctx, "fy2025", "org1")
	if err != nil || year == nil {
		t.Fatalf("getFiscalYear: %v %v", year, err)
	}

	// activate
	if err := integrationStore.updateFiscalYearStatus(ctx, "fy2025", "org1", FiscalYearActive, bson.M{"started_at": now}); err != nil {
		t.Fatalf("updateFiscalYearStatus activate: %v", err)
	}
	active, err := integrationStore.getActiveFiscalYear(ctx, "org1")
	if err != nil || active == nil || active.Status != FiscalYearActive {
		t.Fatalf("getActiveFiscalYear: %v %v", active, err)
	}

	// close
	if err := integrationStore.updateFiscalYearStatus(ctx, "fy2025", "org1", FiscalYearClosed, bson.M{"closed_at": now}); err != nil {
		t.Fatalf("updateFiscalYearStatus close: %v", err)
	}
	closed, _ := integrationStore.getFiscalYear(ctx, "fy2025", "org1")
	if closed.Status != FiscalYearClosed {
		t.Fatalf("expected closed, got %v", closed.Status)
	}
}

// ── Events, budget lines, comments ───────────────────────────────────────────

func TestStore_Events(t *testing.T) {
	drop(t, "org_events", "org_budget_lines", "org_event_comments")
	ctx := context.Background()
	now := time.Now()

	ev := &OrgEvent{
		ID: "ev1", OrgID: "org1", FiscalYearID: "fy1",
		Name: "Annual Gala", Status: EventDraft,
		CreatedBy: "u1", CreatedAt: now,
	}
	if err := integrationStore.createEvent(ctx, ev); err != nil {
		t.Fatalf("createEvent: %v", err)
	}

	events, err := integrationStore.getEvents(ctx, "org1", "fy1")
	if err != nil || len(events) != 1 {
		t.Fatalf("getEvents: %v %v", events, err)
	}

	got, err := integrationStore.getEvent(ctx, "ev1", "org1")
	if err != nil || got == nil || got.Name != "Annual Gala" {
		t.Fatalf("getEvent: %v %v", got, err)
	}

	if err := integrationStore.updateEvent(ctx, "ev1", "org1", bson.M{"status": EventReview}); err != nil {
		t.Fatalf("updateEvent: %v", err)
	}
	updated, _ := integrationStore.getEvent(ctx, "ev1", "org1")
	if updated.Status != EventReview {
		t.Fatalf("status not updated: %v", updated.Status)
	}

	// goal items
	gi := EventGoal{ID: "gi1", Text: "Feed 100 people"}
	if err := integrationStore.addGoalItem(ctx, "ev1", "org1", gi); err != nil {
		t.Fatalf("addGoalItem: %v", err)
	}
	if err := integrationStore.toggleGoalItem(ctx, "ev1", "org1", "gi1", true, "u1"); err != nil {
		t.Fatalf("toggleGoalItem: %v", err)
	}
	withGoal, _ := integrationStore.getEvent(ctx, "ev1", "org1")
	if len(withGoal.GoalItems) == 0 || !withGoal.GoalItems[0].Done {
		t.Fatalf("goal item not toggled: %+v", withGoal.GoalItems)
	}
	if err := integrationStore.deleteGoalItem(ctx, "ev1", "org1", "gi1"); err != nil {
		t.Fatalf("deleteGoalItem: %v", err)
	}

	// budget line
	bl := &BudgetLine{
		ID: "bl1", EventID: "ev1", OrgID: "org1",
		Category: "Catering", Type: BudgetExpense, PlannedCents: 500000, CreatedAt: now,
	}
	if err := integrationStore.createBudgetLine(ctx, bl); err != nil {
		t.Fatalf("createBudgetLine: %v", err)
	}
	lines, err := integrationStore.getBudgetLines(ctx, "ev1", "org1")
	if err != nil || len(lines) != 1 {
		t.Fatalf("getBudgetLines: %v %v", lines, err)
	}
	if err := integrationStore.deleteBudgetLine(ctx, "bl1", "org1"); err != nil {
		t.Fatalf("deleteBudgetLine: %v", err)
	}

	// event comment
	c := &EventComment{
		ID: "ec1", EventID: "ev1", OrgID: "org1", UserID: "u1",
		Kind: CommentReview, Body: "Looks good", CreatedAt: now,
	}
	if err := integrationStore.createEventComment(ctx, c); err != nil {
		t.Fatalf("createEventComment: %v", err)
	}
	comments, err := integrationStore.getEventComments(ctx, "ev1", "org1")
	if err != nil || len(comments) != 1 {
		t.Fatalf("getEventComments: %v %v", comments, err)
	}

	// delete event
	if err := integrationStore.deleteEvent(ctx, "ev1", "org1"); err != nil {
		t.Fatalf("deleteEvent: %v", err)
	}
	empty, _ := integrationStore.getEvents(ctx, "org1", "fy1")
	if len(empty) != 0 {
		t.Fatal("expected 0 events after delete")
	}
}

// ── TxRequests ────────────────────────────────────────────────────────────────

func TestStore_TxRequests(t *testing.T) {
	drop(t, "org_tx_requests")
	ctx := context.Background()
	now := time.Now()

	req := &TxRequest{
		ID: "req1", OrgID: "org1", FiscalYearID: "fy1",
		Type: TxReimbursement, AmountCents: 15000,
		SubmittedBy: "m1", CreatedAt: now,
		StatusLog: []StatusLogEntry{{
			Status:    TxSubmitted,
			ChangedBy: "m1",
			ChangedAt: now,
		}},
	}
	if err := integrationStore.createTxRequest(ctx, req); err != nil {
		t.Fatalf("createTxRequest: %v", err)
	}

	reqs, err := integrationStore.getTxRequests(ctx, "org1", bson.M{})
	if err != nil || len(reqs) != 1 {
		t.Fatalf("getTxRequests: %v %v", reqs, err)
	}

	got, err := integrationStore.getTxRequest(ctx, "req1", "org1")
	if err != nil || got == nil || got.AmountCents != 15000 {
		t.Fatalf("getTxRequest: %v %v", got, err)
	}

	entry := StatusLogEntry{Status: TxApproved, ChangedBy: "admin1", ChangedAt: now}
	if err := integrationStore.appendStatusLog(ctx, "req1", "org1", entry); err != nil {
		t.Fatalf("appendStatusLog: %v", err)
	}

	if err := integrationStore.updateTxRequest(ctx, "req1", "org1", bson.M{"status": TxApproved}); err != nil {
		t.Fatalf("updateTxRequest: %v", err)
	}
	updated, _ := integrationStore.getTxRequest(ctx, "req1", "org1")
	if len(updated.StatusLog) < 2 {
		t.Fatalf("expected ≥2 status log entries, got %d", len(updated.StatusLog))
	}
}

// ── Ledger ────────────────────────────────────────────────────────────────────

func TestStore_Ledger(t *testing.T) {
	drop(t, "org_ledger")
	ctx := context.Background()
	now := time.Now()

	entry := &OrgLedgerEntry{
		ID: "le1", OrgID: "org1", FiscalYearID: "fy1",
		RequestID: "req1", AmountCents: 15000, Description: "Reimbursement",
		Date: now, CreatedAt: now,
	}
	if err := integrationStore.createLedgerEntry(ctx, entry); err != nil {
		t.Fatalf("createLedgerEntry: %v", err)
	}

	entries, err := integrationStore.getLedgerEntries(ctx, "org1", "fy1", bson.M{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("getLedgerEntries: %v %v", entries, err)
	}

	if err := integrationStore.updateLedgerEntry(ctx, "le1", "org1", bson.M{"description": "Updated"}); err != nil {
		t.Fatalf("updateLedgerEntry: %v", err)
	}
	updated, _ := integrationStore.getLedgerEntries(ctx, "org1", "fy1", bson.M{})
	if updated[0].Description != "Updated" {
		t.Fatalf("ledger entry not updated: %v", updated[0].Description)
	}
}

// ── Attachments ───────────────────────────────────────────────────────────────

func TestStore_Attachments(t *testing.T) {
	drop(t, "org_attachments")
	ctx := context.Background()
	now := time.Now()

	att := &OrgAttachment{
		ID: "att1", OrgID: "org1", RequestID: "req1",
		Filename: "invoice.pdf", StoragePath: "/data/org-files/org1/req1/att1",
		UploadedBy: "u1", UploadedAt: now,
	}
	if err := integrationStore.createAttachment(ctx, att); err != nil {
		t.Fatalf("createAttachment: %v", err)
	}

	atts, err := integrationStore.getAttachments(ctx, "req1", "org1")
	if err != nil || len(atts) != 1 || atts[0].Filename != "invoice.pdf" {
		t.Fatalf("getAttachments: %v %v", atts, err)
	}
}

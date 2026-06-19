package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// SeedAdmin looks up the admin user by email directly in the shared MongoDB
// (both services use the same DB) and seeds demo data if the account has no
// existing transactions.
func SeedAdmin(ctx context.Context, store *Store) {
	email := os.Getenv("SEED_USER_EMAIL")
	if email == "" {
		email = "admin@homelab.local"
	}

	userID, err := lookupUserByEmailMongo(ctx, store, email)
	if err != nil {
		slog.Warn("seed: could not resolve admin user, skipping", "email", email, "err", err)
		return
	}

	// idempotent — skip if any transactions already exist
	existing, err := store.getTransactions(ctx, userID, bson.M{})
	if err == nil && len(existing) > 0 {
		slog.Info("seed: data already present, skipping", "user_id", userID)
		return
	}

	slog.Info("seed: seeding demo data", "user_id", userID, "email", email)

	if err := seedAll(ctx, store, userID); err != nil {
		slog.Error("seed: failed", "err", err)
	} else {
		slog.Info("seed: done")
	}
}

// lookupUserByEmailMongo resolves a user ID from email.
// Checks finance_users first (standalone/cloud deployment), then the shared
// "users" collection (Traefik forward-auth deployment).
func lookupUserByEmailMongo(ctx context.Context, store *Store, email string) (string, error) {
	// standalone auth
	var finUser struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := store.db.Collection("finance_users").FindOne(ctx, bson.M{"email": email}).Decode(&finUser); err == nil {
		return finUser.ID.Hex(), nil
	}
	// legacy shared-auth fallback
	var legacy struct {
		ID    string `bson:"_id"`
		Email string `bson:"email"`
	}
	if err := store.db.Collection("users").FindOne(ctx, bson.M{"email": email}).Decode(&legacy); err != nil {
		return "", fmt.Errorf("user %q not found in mongo: %w", email, err)
	}
	return legacy.ID, nil
}

// SeedExtras seeds goals, property, and loan data if not already present.
// Called independently so it runs even when transactions already exist.
func SeedExtras(ctx context.Context, store *Store) {
	email := os.Getenv("SEED_USER_EMAIL")
	if email == "" {
		email = "admin@homelab.local"
	}
	userID, err := lookupUserByEmailMongo(ctx, store, email)
	if err != nil {
		slog.Warn("seed extras: could not resolve user, skipping", "email", email, "err", err)
		return
	}
	slog.Info("seed extras: checking for goals/property", "user_id", userID)

	// goals
	goals, _ := store.getGoals(ctx, userID)
	if len(goals) == 0 {
		slog.Info("seed: seeding goals", "user_id", userID)
		if err := seedGoals(ctx, store, userID); err != nil {
			slog.Error("seed: goals failed", "err", err)
		}
		goals, _ = store.getGoals(ctx, userID)
	}

	// goal-tagged transactions (tx-backed progress — idempotent)
	if len(goals) > 0 {
		if err := seedGoalTransactions(ctx, store, userID, goals); err != nil {
			slog.Error("seed: goal transactions failed", "err", err)
		}
	}

	// properties & loans
	props, _ := store.getProperties(ctx, userID)
	if len(props) == 0 {
		slog.Info("seed: seeding property + loan", "user_id", userID)
		if err := seedProperty(ctx, store, userID); err != nil {
			slog.Error("seed: property failed", "err", err)
		}
	}
}

func seedGoals(ctx context.Context, store *Store, userID string) error {
	now := time.Now()
	goals := []*Goal{
		{
			ID:          bson.NewObjectID().Hex(),
			UserID:      userID,
			Name:        "Emergency fund (3 months)",
			Type:        GoalTypeEmergency,
			TargetCents: 390000, // €3,900
			SavedCents:  210000, // €2,100 already set aside
			Deadline:    now.AddDate(1, 0, 0),
			Committed:   true,
			CreatedAt:   now.AddDate(0, -4, 0),
		},
		{
			ID:          bson.NewObjectID().Hex(),
			UserID:      userID,
			Name:        "Japan trip",
			Type:        GoalTypeOnce,
			TargetCents: 350000, // €3,500
			SavedCents:  80000,  // €800 saved
			Deadline:    now.AddDate(1, 6, 0),
			Committed:   false,
			CreatedAt:   now.AddDate(0, -2, 0),
		},
		{
			ID:          bson.NewObjectID().Hex(),
			UserID:      userID,
			Name:        "MacBook Pro",
			Type:        GoalTypeOnce,
			TargetCents: 250000, // €2,500
			SavedCents:  40000,  // €400
			Deadline:    now.AddDate(0, 10, 0),
			Committed:   false,
			CreatedAt:   now.AddDate(0, -1, 0),
		},
		{
			ID:          bson.NewObjectID().Hex(),
			UserID:      userID,
			Name:        "House down payment",
			Type:        GoalTypeDeposit,
			TargetCents: 4000000, // €40,000
			SavedCents:  500000,  // €5,000 saved
			Deadline:    now.AddDate(5, 0, 0),
			Committed:   true,
			CreatedAt:   now.AddDate(0, -6, 0),
		},
	}
	for _, g := range goals {
		if err := store.createGoal(ctx, g); err != nil {
			return fmt.Errorf("create goal %q: %w", g.Name, err)
		}
	}
	return nil
}

// seedGoalTransactions back-fills goal-tagged transactions so the tx-backed
// SavedCents and waterfall reflect realistic progress. Idempotent — skips if
// any goal-tagged transactions already exist.
func seedGoalTransactions(ctx context.Context, store *Store, userID string, goals []Goal) error {
	existing, _ := store.getTransactions(ctx, userID, bson.M{
		"goal_id": bson.M{"$exists": true, "$ne": ""},
	})
	if len(existing) > 0 {
		slog.Info("seed: goal transactions already present, skipping")
		return nil
	}

	accounts, _ := store.getAccounts(ctx, userID)
	savingsID := ""
	checkingID := ""
	for _, a := range accounts {
		switch a.Type {
		case "savings":
			savingsID = a.ID
		case "checking":
			if checkingID == "" {
				checkingID = a.ID
			}
		}
	}
	if savingsID == "" {
		savingsID = checkingID
	}
	if savingsID == "" && len(accounts) > 0 {
		savingsID = accounts[0].ID
	}

	goalsByName := make(map[string]Goal, len(goals))
	for _, g := range goals {
		goalsByName[g.Name] = g
	}

	now := time.Now()
	monthStart := func(monthsAgo int, day int) time.Time {
		t := time.Date(now.Year(), now.Month(), day, 0, 0, 0, 0, now.Location())
		return t.AddDate(0, -monthsAgo, 0)
	}

	var txns []Transaction
	mkTxn := func(date time.Time, desc string, cents int64, goalID string) {
		txns = append(txns, Transaction{
			ID:          bson.NewObjectID().Hex(),
			UserID:      userID,
			AccountID:   savingsID,
			Date:        date,
			Description: desc,
			AmountCents: -cents, // outflow
			Category:    "Investments",
			GoalID:      goalID,
			CreatedAt:   time.Now(),
		})
	}

	// Emergency fund (3 months) — 7×€300 past months + €300 this month = €2,400
	if g, ok := goalsByName["Emergency fund (3 months)"]; ok {
		for i := 7; i >= 1; i-- {
			mkTxn(monthStart(i, 5), "Emergency fund transfer", 30000, g.ID)
		}
		mkTxn(monthStart(0, 5), "Emergency fund transfer", 30000, g.ID)
	}

	// House down payment — 10×€500 past months + €500 this month = €5,500
	if g, ok := goalsByName["House down payment"]; ok {
		for i := 10; i >= 1; i-- {
			mkTxn(monthStart(i, 10), "House down payment savings", 50000, g.ID)
		}
		mkTxn(monthStart(0, 10), "House down payment savings", 50000, g.ID)
	}

	// Japan trip — 4×€200 past months + €200 this month = €1,000
	if g, ok := goalsByName["Japan trip"]; ok {
		for i := 4; i >= 1; i-- {
			mkTxn(monthStart(i, 15), "Japan trip savings", 20000, g.ID)
		}
		mkTxn(monthStart(0, 15), "Japan trip savings", 20000, g.ID)
	}

	// MacBook Pro — 2×€200 past months + €200 this month = €600
	if g, ok := goalsByName["MacBook Pro"]; ok {
		for i := 2; i >= 1; i-- {
			mkTxn(monthStart(i, 20), "MacBook Pro savings", 20000, g.ID)
		}
		mkTxn(monthStart(0, 20), "MacBook Pro savings", 20000, g.ID)
	}

	if len(txns) == 0 {
		return nil
	}
	slog.Info("seed: inserting goal-tagged transactions", "count", len(txns))
	return store.createTransactions(ctx, txns)
}

func seedProperty(ctx context.Context, store *Store, userID string) error {
	now := time.Now()
	propID := bson.NewObjectID().Hex()
	loanID := bson.NewObjectID().Hex()

	prop := &Property{
		ID:                 propID,
		UserID:             userID,
		Name:               "Apartamento T2 — Porto",
		Address:            "Rua de Santa Catarina, Porto",
		PurchasePriceCents: 18000000, // €180,000
		CurrentValueCents:  22000000, // €220,000
		AppreciationPct:    3.0,
		PurchaseDate:       now.AddDate(-4, 0, 0),
		Status:             PropertyOwned,
		CreatedAt:          now.AddDate(-4, 0, 0),
	}
	if err := store.createProperty(ctx, prop); err != nil {
		return fmt.Errorf("create property: %w", err)
	}

	// 25-year mortgage, started 4 years ago, ~€900/month at 3.5%
	loan := &Loan{
		ID:                  loanID,
		UserID:              userID,
		PropertyID:          propID,
		Name:                "Hipoteca CGD — Porto",
		Type:                LoanMortgage,
		PrincipalCents:      18000000,
		BalanceCents:        16068700, // after 48 payments
		InterestRatePct:     3.5,
		TermMonths:          300, // 25 years
		StartDate:           now.AddDate(-4, 0, 0),
		MonthlyPaymentCents: 90070, // €900.70 EMI
		Status:              LoanActive,
		CreatedAt:           now.AddDate(-4, 0, 0),
	}
	return store.createLoan(ctx, loan)
}

func seedAll(ctx context.Context, store *Store, userID string) error {
	// ── Accounts ─────────────────────────────────────────────────────────
	checkingID := bson.NewObjectID().Hex()
	savingsID := bson.NewObjectID().Hex()
	creditID := bson.NewObjectID().Hex()
	investID := bson.NewObjectID().Hex()

	accounts := []*Account{
		{ID: checkingID, UserID: userID, Name: "CGD Checking", Type: "checking"},
		{ID: savingsID, UserID: userID, Name: "CGD Savings", Type: "savings"},
		{ID: creditID, UserID: userID, Name: "Visa Credit", Type: "credit"},
		{ID: investID, UserID: userID, Name: "Trade Republic", Type: "securities"},
	}
	for _, a := range accounts {
		if err := store.createAccount(ctx, a); err != nil {
			return fmt.Errorf("create account: %w", err)
		}
	}

	// ── Categories with budgets ───────────────────────────────────────────
	type catDef struct {
		Name        string
		Color       string
		BudgetCents int64
	}
	catDefs := []catDef{
		{"Groceries", "#4caf50", 30000},
		{"Food", "#ff9800", 20000},
		{"Transport", "#2196f3", 8000},
		{"Housing", "#9c27b0", 80000},
		{"Utilities", "#607d8b", 10000},
		{"Health", "#f44336", 5000},
		{"Clothing", "#e91e63", 10000},
		{"Games", "#673ab7", 3000},
		{"Entertainment", "#ff5722", 5000},
		{"Subscriptions", "#795548", 4000},
		{"Shopping", "#ff6f00", 15000},
		{"Income", "#2e7d32", 0},
		{"Investments", "#1565c0", 20000},
		{"Others", "#9e9e9e", 5000},
	}
	catIDByName := make(map[string]string)
	for _, cd := range catDefs {
		id := bson.NewObjectID().Hex()
		catIDByName[cd.Name] = id
		cat := &Category{
			ID:          id,
			UserID:      userID,
			Name:        cd.Name,
			Color:       cd.Color,
			BudgetCents: cd.BudgetCents,
		}
		if err := store.createCategory(ctx, cat); err != nil {
			return fmt.Errorf("create category %s: %w", cd.Name, err)
		}
	}

	// ── Transactions — 6 months of realistic Portuguese household spend ───
	now := time.Now()
	var txns []Transaction

	type txDef struct {
		daysAgo     int
		desc        string
		amountCents int64
		cat         string
		accountID   string
	}

	rawTxns := []txDef{
		// ── Month 0 (current month) ──────────────────────────────────────
		{0, "Salary — Homelab Corp", 230000, "Income", checkingID},
		{1, "Continente Supermercado", -4230, "Groceries", checkingID},
		{2, "MEO Internet", -3999, "Utilities", checkingID},
		{3, "Glovo — Sushi House", -2150, "Food", creditID},
		{4, "Uber Lisboa", -850, "Transport", creditID},
		{5, "Steam — Elden Ring DLC", -2999, "Games", creditID},
		{6, "Pingo Doce", -3120, "Groceries", checkingID},
		{7, "Farmácia Saúde", -1540, "Health", checkingID},
		{8, "Spotify Premium", -999, "Subscriptions", creditID},
		{9, "Netflix", -1599, "Subscriptions", creditID},
		{10, "Lidl Supermercado", -5640, "Groceries", checkingID},
		{11, "Restaurante O Barrigas", -3280, "Food", creditID},
		{12, "Trade Republic — VWCE Buy", -50000, "Investments", investID},
		{13, "CP — Lisboa Cascais", -310, "Transport", creditID},
		{14, "Decathlon", -4990, "Shopping", creditID},
		{15, "Rendimento subarrendamento", 30000, "Income", checkingID},
		// ── Month 1 ──────────────────────────────────────────────────────
		{30, "Salary — Homelab Corp", 230000, "Income", checkingID},
		{31, "Auchan Supermercado", -6780, "Groceries", checkingID},
		{32, "EDP Energia", -6200, "Utilities", checkingID},
		{33, "Renda apartamento", -80000, "Housing", checkingID},
		{34, "McDonald's Marquês", -1190, "Food", creditID},
		{35, "Zara — Coleção Verão", -8990, "Clothing", creditID},
		{36, "Bolt ride", -620, "Transport", creditID},
		{37, "NOS Telemóvel", -1799, "Utilities", checkingID},
		{38, "Intermarché", -4420, "Groceries", checkingID},
		{39, "Ginásio Holmes Place", -4900, "Health", checkingID},
		{40, "Amazon Prime", -799, "Subscriptions", creditID},
		{41, "Glovo — Burger King", -1890, "Food", creditID},
		{42, "Trade Republic — SXR8 Buy", -30000, "Investments", investID},
		{43, "Via Verde portagens", -920, "Transport", checkingID},
		{44, "Fnac — Livros", -2350, "Shopping", creditID},
		{45, "H&M online", -5490, "Clothing", creditID},
		// ── Month 2 ──────────────────────────────────────────────────────
		{60, "Salary — Homelab Corp", 230000, "Income", checkingID},
		{61, "Continente Supermercado", -5120, "Groceries", checkingID},
		{62, "MEO Internet", -3999, "Utilities", checkingID},
		{63, "Renda apartamento", -80000, "Housing", checkingID},
		{64, "Pastelaria Batalha", -480, "Food", creditID},
		{65, "Uber Lisboa", -1240, "Transport", creditID},
		{66, "Epic Games — Fortnite", -1999, "Games", creditID},
		{67, "Lidl Supermercado", -4890, "Groceries", checkingID},
		{68, "Farmácia da Baixa", -2310, "Health", checkingID},
		{69, "Spotify Premium", -999, "Subscriptions", creditID},
		{70, "Netflix", -1599, "Subscriptions", creditID},
		{71, "Restaurante Eleven", -9400, "Food", creditID},
		{72, "Trade Republic — VWCE Buy", -50000, "Investments", investID},
		{73, "Teatro Nacional", -2500, "Entertainment", creditID},
		{74, "IKEA Lisboa", -14900, "Shopping", creditID},
		{75, "Pingo Doce", -3670, "Groceries", checkingID},
		// ── Month 3 ──────────────────────────────────────────────────────
		{90, "Salary — Homelab Corp", 230000, "Income", checkingID},
		{91, "Auchan Supermercado", -7230, "Groceries", checkingID},
		{92, "EDP Energia", -5800, "Utilities", checkingID},
		{93, "Renda apartamento", -80000, "Housing", checkingID},
		{94, "KFC Colombo", -1440, "Food", creditID},
		{95, "Bolt ride", -740, "Transport", creditID},
		{96, "NOS Telemóvel", -1799, "Utilities", checkingID},
		{97, "Intermarché", -5560, "Groceries", checkingID},
		{98, "Consulta médica particular", -8000, "Health", checkingID},
		{99, "Disney+", -799, "Subscriptions", creditID},
		{100, "Amazon Prime", -799, "Subscriptions", creditID},
		{101, "Restaurante A Cevicheria", -6200, "Food", creditID},
		{102, "Trade Republic — SXR8 Buy", -30000, "Investments", investID},
		{103, "CP — InterCity Porto", -2450, "Transport", checkingID},
		{104, "Livraria Bertrand", -1890, "Shopping", creditID},
		// ── Month 4 ──────────────────────────────────────────────────────
		{120, "Salary — Homelab Corp", 230000, "Income", checkingID},
		{121, "Continente Supermercado", -6010, "Groceries", checkingID},
		{122, "MEO Internet", -3999, "Utilities", checkingID},
		{123, "Renda apartamento", -80000, "Housing", checkingID},
		{124, "Glovo — Pizza Hut", -2340, "Food", creditID},
		{125, "Uber Lisboa", -990, "Transport", creditID},
		{126, "PlayStation Store", -2999, "Games", creditID},
		{127, "Lidl Supermercado", -5210, "Groceries", checkingID},
		{128, "Farmácia Saúde", -890, "Health", checkingID},
		{129, "Spotify Premium", -999, "Subscriptions", creditID},
		{130, "Netflix", -1599, "Subscriptions", creditID},
		{131, "Tasca do Chico — jantar", -5400, "Food", creditID},
		{132, "Trade Republic — VWCE Buy", -50000, "Investments", investID},
		{133, "Fnac — AirPods", -17900, "Shopping", creditID},
		{134, "Pingo Doce", -4120, "Groceries", checkingID},
		// ── Month 5 ──────────────────────────────────────────────────────
		{150, "Salary — Homelab Corp", 230000, "Income", checkingID},
		{151, "Auchan Supermercado", -5890, "Groceries", checkingID},
		{152, "EDP Energia", -6400, "Utilities", checkingID},
		{153, "Renda apartamento", -80000, "Housing", checkingID},
		{154, "Nando's Lisboa", -1980, "Food", creditID},
		{155, "Bolt ride", -510, "Transport", creditID},
		{156, "NOS Telemóvel", -1799, "Utilities", checkingID},
		{157, "Intermarché", -4780, "Groceries", checkingID},
		{158, "Óculos — Ótica Avenida", -12000, "Health", checkingID},
		{159, "Disney+", -799, "Subscriptions", creditID},
		{160, "Amazon Prime", -799, "Subscriptions", creditID},
		{161, "Cinemateca Portuguesa", -600, "Entertainment", creditID},
		{162, "Trade Republic — VWCE Buy", -50000, "Investments", investID},
		{163, "Zara — Outono", -12490, "Clothing", creditID},
		{164, "Worten — Monitor", -34900, "Shopping", creditID},
	}

	for _, td := range rawTxns {
		date := now.AddDate(0, 0, -td.daysAgo).Truncate(24 * time.Hour)
		txns = append(txns, Transaction{
			ID:          bson.NewObjectID().Hex(),
			UserID:      userID,
			AccountID:   td.accountID,
			Date:        date,
			Description: td.desc,
			AmountCents: td.amountCents,
			Category:    td.cat,
			CreatedAt:   time.Now(),
		})
	}

	if err := store.createTransactions(ctx, txns); err != nil {
		return fmt.Errorf("create transactions: %w", err)
	}

	// ── Portfolio trades ─────────────────────────────────────────────────
	trades := []Trade{
		// VWCE — Vanguard FTSE All-World
		{
			ID: bson.NewObjectID().Hex(), UserID: userID,
			ISIN: "IE00B3RBWM25", Name: "VWCE - Vanguard All-World",
			Type: "buy", Quantity: 12, PriceCents: 11820, TotalCents: 141840,
			Date: now.AddDate(0, -5, 5), CreatedAt: time.Now(),
		},
		{
			ID: bson.NewObjectID().Hex(), UserID: userID,
			ISIN: "IE00B3RBWM25", Name: "VWCE - Vanguard All-World",
			Type: "buy", Quantity: 8, PriceCents: 11960, TotalCents: 95680,
			Date: now.AddDate(0, -3, 12), CreatedAt: time.Now(),
		},
		{
			ID: bson.NewObjectID().Hex(), UserID: userID,
			ISIN: "IE00B3RBWM25", Name: "VWCE - Vanguard All-World",
			Type: "buy", Quantity: 6, PriceCents: 12100, TotalCents: 72600,
			Date: now.AddDate(0, -1, 12), CreatedAt: time.Now(),
		},
		// SXR8 — iShares Core S&P 500
		{
			ID: bson.NewObjectID().Hex(), UserID: userID,
			ISIN: "IE00B5BMR087", Name: "SXR8 - iShares S&P 500",
			Type: "buy", Quantity: 15, PriceCents: 53200, TotalCents: 798000,
			Date: now.AddDate(0, -4, 8), CreatedAt: time.Now(),
		},
		{
			ID: bson.NewObjectID().Hex(), UserID: userID,
			ISIN: "IE00B5BMR087", Name: "SXR8 - iShares S&P 500",
			Type: "buy", Quantity: 10, PriceCents: 55100, TotalCents: 551000,
			Date: now.AddDate(0, -2, 3), CreatedAt: time.Now(),
		},
		// EUNL — iShares Core MSCI World
		{
			ID: bson.NewObjectID().Hex(), UserID: userID,
			ISIN: "IE00B4L5Y983", Name: "EUNL - iShares MSCI World",
			Type: "buy", Quantity: 20, PriceCents: 8950, TotalCents: 179000,
			Date: now.AddDate(0, -5, 20), CreatedAt: time.Now(),
		},
		{
			ID: bson.NewObjectID().Hex(), UserID: userID,
			ISIN: "IE00B4L5Y983", Name: "EUNL - iShares MSCI World",
			Type: "buy", Quantity: 10, PriceCents: 9210, TotalCents: 92100,
			Date: now.AddDate(0, -2, 18), CreatedAt: time.Now(),
		},
	}

	if err := store.createTrades(ctx, trades); err != nil {
		return fmt.Errorf("create trades: %w", err)
	}

	return nil
}

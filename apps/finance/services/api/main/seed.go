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

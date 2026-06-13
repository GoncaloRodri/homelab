package main

import "time"

var DefaultCategories = []string{
	"Groceries", "Food", "Transport", "Housing", "Utilities",
	"Health", "Clothing", "Games", "Entertainment", "Education",
	"Subscriptions", "Shopping", "Income", "Investments", "Others",
}

var DefaultCategoryColors = map[string]string{
	"Groceries":     "#4caf50",
	"Food":          "#ff9800",
	"Transport":     "#2196f3",
	"Housing":       "#9c27b0",
	"Utilities":     "#607d8b",
	"Health":        "#f44336",
	"Clothing":      "#e91e63",
	"Games":         "#673ab7",
	"Entertainment": "#ff5722",
	"Education":     "#00bcd4",
	"Subscriptions": "#795548",
	"Shopping":      "#ff6f00",
	"Income":        "#2e7d32",
	"Investments":   "#1565c0",
	"Others":        "#9e9e9e",
}

type Account struct {
	ID     string `bson:"_id" json:"id"`
	UserID string `bson:"user_id" json:"user_id"`
	Name   string `bson:"name" json:"name"`
	Type   string `bson:"type" json:"type"` // checking, savings, credit, securities
}

type Category struct {
	ID         string `bson:"_id" json:"id"`
	UserID     string `bson:"user_id" json:"user_id"`
	Name       string `bson:"name" json:"name"`
	Color      string `bson:"color" json:"color"`
	BudgetCents int64 `bson:"budget_cents" json:"budget_cents"`
}

type Transaction struct {
	ID          string    `bson:"_id" json:"id"`
	UserID      string    `bson:"user_id" json:"user_id"`
	AccountID   string    `bson:"account_id" json:"account_id"`
	Date        time.Time `bson:"date" json:"date"`
	Description string    `bson:"description" json:"description"`
	AmountCents int64     `bson:"amount_cents" json:"amount_cents"`
	Category    string    `bson:"category" json:"category"`
	BankRef     string    `bson:"bank_ref,omitempty" json:"bank_ref,omitempty"`
	RawCSV      string    `bson:"raw_csv,omitempty" json:"raw_csv,omitempty"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
}

type Trade struct {
	ID          string    `bson:"_id" json:"id"`
	UserID      string    `bson:"user_id" json:"user_id"`
	ISIN        string    `bson:"isin" json:"isin"`
	Name        string    `bson:"name" json:"name"`
	Type        string    `bson:"type" json:"type"` // buy or sell
	Quantity    float64   `bson:"quantity" json:"quantity"`
	PriceCents  int64     `bson:"price_cents" json:"price_cents"`
	TotalCents  int64     `bson:"total_cents" json:"total_cents"`
	Date        time.Time `bson:"date" json:"date"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
}

type Holding struct {
	ISIN            string  `json:"isin"`
	Name            string  `json:"name"`
	SharesOwned     float64 `json:"shares_owned"`
	AvgEntryCents   int64   `json:"avg_entry_cents"`
	TotalCostCents  int64   `json:"total_cost_cents"`
	CurrentPriceCents int64 `json:"current_price_cents"`
	CurrentValueCents int64 `json:"current_value_cents"`
	UnrealizedPCLCents int64 `json:"unrealized_pnl_cents"`
	UnrealizedPCLPct  float64 `json:"unrealized_pnl_pct"`
}

type RealizedPCL struct {
	TotalCents int64 `json:"total_cents"`
	Trades     []Trade `json:"trades"`
}

type Permission struct {
	ID        string    `bson:"_id" json:"id"`
	OwnerID   string    `bson:"owner_id" json:"owner_id"`
	ViewerID  string    `bson:"viewer_id" json:"viewer_id"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

type CSVImportRow struct {
	Date        string `json:"date"`
	Description string `json:"description"`
	AmountCents int64  `json:"amount_cents"`
	Category    string `json:"category"`
}

type CSVImportPreview struct {
	AccountID string         `json:"account_id"`
	Rows      []CSVImportRow `json:"rows"`
	Total     int            `json:"total"`
}

// FixedCategories are treated as recurring committed costs, not variable spend.
var FixedCategories = map[string]bool{
	"Housing":       true,
	"Utilities":     true,
	"Subscriptions": true,
	"Investments":   true,
}

type RecurringExpense struct {
	Category    string
	MonthlyCents int64
}

type DashboardData struct {
	UserID        string
	Email         string
	Title         string
	Route         string
	IsOwner       bool
	ThisMonth     *PeriodSummary
	LastMonth     *PeriodSummary
	RecentTxns    []Transaction
	BalanceTrend  []BalancePoint

	// Phase 1 fields
	ThisMonthIncome    int64
	ThisMonthExpense   int64
	CategoryBudgets    map[string]int64
	CategoryColors     map[string]string

	AvailableToSpend   int64  // income − fixed − variable budgets spent so far
	DisposableIncome   int64  // income − fixed recurring costs
	MonthProgressPct   int    // % of month elapsed
	MonthSpentPct      int    // % of disposable already spent

	RecurringExpenses  []RecurringExpense
	BankShouldBe       int64  // sum of upcoming fixed costs + safety buffer
	SafetyBufferCents  int64

	SavingsRatePct     int    // savings / income * 100 this month
	LastMonthSavingsRatePct int

	PortfolioValueCents      int64
	PortfolioPCLCents        int64
	PortfolioHoldings        []Holding
	PortfolioPricesAvailable bool
}

type PeriodSummary struct {
	TotalCents    int64
	ByCategory    map[string]int64
	CategoryNames map[string]string
}

type BalancePoint struct {
	Date   time.Time
	Cents  int64
}

type ReportData struct {
	UserID       string
	Email        string
	Title        string
	Route        string
	MonthlyData  []MonthlyCategorySummary
	CategoryNames map[string]string
	Year         int
}

type MonthlyCategorySummary struct {
	Month    string
	Totals   map[string]int64
}

type ProjectionData struct {
	UserID        string
	Email         string
	Title         string
	Route         string
	MonthlyAvg    map[string]float64
	AnnualTotal   int64
	CategoryNames map[string]string
}

type PortfolioData struct {
	UserID      string
	Email       string
	Title       string
	Route       string
	Holdings    []Holding
	TotalValueCents  int64
	TotalCostCents   int64
	TotalPCLCents    int64
	TotalPCLPct      float64
	RealizedPCLCents int64
}

type SharingData struct {
	UserID   string
	Email    string
	Title    string
	Route    string
	Grants   []Permission
	Viewers  []SharingUser
}

type SharingUser struct {
	ID    string
	Email string
}

// GoalType classifies a financial goal for display and calculation purposes.
type GoalType string

const (
	GoalTypeOnce        GoalType = "once"        // one-off purchase (Switch, holiday)
	GoalTypeDeposit     GoalType = "deposit"      // house deposit / down-payment
	GoalTypeEmergency   GoalType = "emergency"    // emergency fund (N months of expenses)
	GoalTypeInvestment  GoalType = "investment"   // recurring investment target
)

type Goal struct {
	ID            string    `bson:"_id" json:"id"`
	UserID        string    `bson:"user_id" json:"user_id"`
	Name          string    `bson:"name" json:"name"`
	Type          GoalType  `bson:"type" json:"type"`
	TargetCents   int64     `bson:"target_cents" json:"target_cents"`
	SavedCents    int64     `bson:"saved_cents" json:"saved_cents"`
	Deadline      time.Time `bson:"deadline" json:"deadline"`
	Committed     bool      `bson:"committed" json:"committed"` // Phase 3: false until user commits
	CreatedAt     time.Time `bson:"created_at" json:"created_at"`
}

// GoalPlan is computed at request time — never stored.
type GoalPlan struct {
	Goal
	MonthsLeft          int64
	MonthlyCents        int64
	ImpactOnDisposable  int64
	MonthsAtCurrentRate int64
	Feasible            bool
	ProgressPct         int64
}

type GoalsData struct {
	UserID             string
	Email              string
	Title              string
	Route              string
	Goals              []GoalPlan
	AvgMonthlySavings  int64  // 3-month average savings for projection
	DisposableIncome   int64  // from current month dashboard calc
}

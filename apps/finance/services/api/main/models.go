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
	ID          string `bson:"_id" json:"id"`
	UserID      string `bson:"user_id" json:"user_id"`
	Name        string `bson:"name" json:"name"`
	Color       string `bson:"color" json:"color"`
	BudgetCents int64  `bson:"budget_cents" json:"budget_cents"`
	// GoalID, when set, auto-tags transactions in this category to the linked goal.
	GoalID string `bson:"goal_id,omitempty" json:"goal_id,omitempty"`
}

type Transaction struct {
	ID          string    `bson:"_id" json:"id"`
	UserID      string    `bson:"user_id" json:"user_id"`
	AccountID   string    `bson:"account_id" json:"account_id"`
	Date        time.Time `bson:"date" json:"date"`
	Description string    `bson:"description" json:"description"`
	AmountCents int64     `bson:"amount_cents" json:"amount_cents"`
	Category    string    `bson:"category" json:"category"`
	GoalID      string    `bson:"goal_id,omitempty" json:"goal_id,omitempty"`
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
	Fingerprint string `json:"fingerprint"`
	Duplicate   bool   `json:"duplicate"`
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

// WaterfallRow is one drilldown entry inside the interactive waterfall.
type WaterfallRow struct {
	Name  string
	Color string
	Cents int64 // always positive (absolute spend or income amount)
}

type DashboardData struct {
	T             *Translator
	UserID        string
	Email         string
	Title         string
	Route         string
	IsOwner       bool
	ThisMonth     *PeriodSummary
	LastMonth     *PeriodSummary
	RecentTxns    []Transaction
	BalanceTrend  []BalancePoint

	ThisMonthIncome    int64
	ThisMonthExpense   int64
	CategoryBudgets    map[string]int64
	CategoryColors     map[string]string
	MonthProgressPct   int

	// Transaction-backed waterfall totals
	WaterfallIncome   int64
	WaterfallLiving   int64
	WaterfallGoals    int64
	WaterfallFreeCash int64

	// Drill-down: sorted category rows + pre-grouped transactions
	IncomeCats          []WaterfallRow
	LivingCats          []WaterfallRow
	IncomeCatTxns       map[string][]Transaction // category → this-month income txns
	LivingCatTxns       map[string][]Transaction // category → this-month living txns
	GoalFundedThisMonth map[string]int64          // goalID → amount funded this month

	SavingsRatePct          int
	LastMonthSavingsRatePct int

	PortfolioValueCents      int64
	PortfolioPCLCents        int64
	PortfolioHoldings        []Holding
	PortfolioPricesAvailable bool

	NetWorthCents int64

	Alerts    []Alert
	DashGoals []GoalPlan
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
	T            *Translator
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
	T             *Translator
	UserID        string
	Email         string
	Title         string
	Route         string
	MonthlyAvg    map[string]float64
	AnnualTotal   int64
	CategoryNames map[string]string
}

type PortfolioData struct {
	T           *Translator
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
	// ISINs for which no price could be fetched (so user can supply a ticker)
	MissingPrices []string
}

type SharingData struct {
	T        *Translator
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

// ── Tax Summary ──────────────────────────────────────────────────────────────

type TaxDeductible struct {
	Category    string
	Description string
	TotalCents  int64
}

type CapitalGainEntry struct {
	ISIN       string
	Name       string
	BuyCents   int64
	SellCents  int64
	GainCents  int64
	GainPct    float64
}

type TaxData struct {
	T        *Translator
	UserID   string
	Email    string
	Title    string
	Route    string
	Year     int

	GrossIncomeCents   int64
	CapitalGainsCents  int64
	CapitalLossesCents int64
	NetCapitalCents    int64

	Deductibles    []TaxDeductible
	TotalDeductCents int64

	CapitalEntries []CapitalGainEntry

	// year options for selector
	AvailableYears []int
}

// ── Household ────────────────────────────────────────────────────────────────

type Household struct {
	ID        string    `bson:"_id" json:"id"`
	OwnerID   string    `bson:"owner_id" json:"owner_id"`
	PartnerID string    `bson:"partner_id" json:"partner_id"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

// PeopleData combines Sharing and Household into a single page.
type PeopleData struct {
	T      *Translator
	UserID string
	Email  string
	Title  string
	Route  string
	Tab    string // "sharing" | "household"

	// sharing tab
	Grants  []Permission
	Viewers []SharingUser
	Granted []Permission

	// household tab
	HasHousehold         bool
	IsOwner              bool
	PartnerEmail         string
	PartnerID            string
	CombinedIncomeCents  int64
	CombinedExpenseCents int64
	CombinedDisposable   int64
	MyIncomeCents        int64
	PartnerIncomeCents   int64
	MyGoals              []GoalPlan
	PartnerGoals         []GoalPlan
}

// SettingsData combines Accounts and Categories into a single page.
type SettingsData struct {
	T          *Translator
	UserID     string
	Email      string
	Title      string
	Route      string
	Tab        string // "accounts" | "categories"
	Accounts   []Account
	Categories []Category
	Goals      []Goal            // for category → goal linking dropdown
	GoalNameByID map[string]string // goalID → name, for table display
}

type HouseholdData struct {
	T        *Translator
	UserID   string
	Email    string
	Title    string
	Route    string

	HasHousehold  bool
	IsOwner       bool
	PartnerEmail  string
	PartnerID     string

	// combined view
	CombinedIncomeCents    int64
	CombinedExpenseCents   int64
	CombinedDisposable     int64
	MyIncomeCents          int64
	PartnerIncomeCents     int64
	MyGoals                []GoalPlan
	PartnerGoals           []GoalPlan
	SharedGoals            []GoalPlan // goals from both users
}

// ── Auto Import ──────────────────────────────────────────────────────────────

type ImportSchedule struct {
	ID        string    `bson:"_id" json:"id"`
	UserID    string    `bson:"user_id" json:"user_id"`
	AccountID string    `bson:"account_id" json:"account_id"`
	Label     string    `bson:"label" json:"label"`
	Format    string    `bson:"format" json:"format"`    // cgd, traderepublic, generic
	URL       string    `bson:"url" json:"url"`          // URL to fetch CSV from (optional)
	Active    bool      `bson:"active" json:"active"`
	LastRunAt time.Time `bson:"last_run_at" json:"last_run_at"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

type AutoImportData struct {
	T         *Translator
	UserID    string
	Email     string
	Title     string
	Route     string
	Accounts  []Account
	Schedules []ImportSchedule
}

type AlertLevel string

const (
	AlertWarn  AlertLevel = "warn"
	AlertDanger AlertLevel = "danger"
	AlertInfo  AlertLevel = "info"
)

type Alert struct {
	Level   AlertLevel
	Message string
}

type SimulatorGoal struct {
	Name         string
	MonthlyCents int64
	MonthsLeft   int64
	Committed    bool
}

type SimulatorData struct {
	T        *Translator
	UserID   string
	Email    string
	Title    string
	Route    string

	// current state passed to JS
	IncomeCents      int64
	FixedCents       int64 // recurring fixed costs (no goals)
	GoalsCents       int64 // committed goal contributions
	DisposableCents  int64 // income − fixed − goals
	AvgSavingsCents  int64 // 3-month avg monthly savings
	Goals            []SimulatorGoal

	// savings rate history: one point per past month
	SavingsHistory []SavingsPoint
}

type SavingsPoint struct {
	Month       string
	IncomeCents int64
	SavedCents  int64
	RatePct     int
}

type NetWorthPoint struct {
	Month      string // "2025-01"
	AssetCents int64
	LiabCents  int64
	NetCents   int64
}

type NetWorthData struct {
	T      *Translator
	UserID string
	Email  string
	Title  string
	Route  string

	// current snapshot
	CashCents           int64 // running balance of all non-credit accounts
	PortfolioCents      int64 // market value (or cost basis)
	CreditCents         int64 // total outstanding on credit accounts (positive = owed)
	PropertyValueCents  int64 // sum of current value of non-sold properties
	LoanBalanceCents    int64 // sum of active loan balances
	PropertyEquityCents int64 // PropertyValueCents - LoanBalanceCents
	NetWorthCents       int64 // cash + portfolio + propertyEquity − credit

	PortfolioPricesAvailable bool

	// month-by-month history
	History []NetWorthPoint
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
	FundingTxns         []Transaction // recent transactions tagged to this goal
}

type GoalsData struct {
	T                       *Translator
	UserID                  string
	Email                   string
	Title                   string
	Route                   string
	Tab                     string // "goals" or "planner"
	Goals             []GoalPlan
	AvgMonthlySavings int64
	// Waterfall (transaction-backed)
	WaterfallIncome   int64 // gross income this month
	WaterfallLiving   int64 // outflows not tagged to any goal
	WaterfallGoals    int64 // outflows tagged to goals this month
	WaterfallFreeCash int64 // income - living - goals
	// Planner tab
	PlannerType       string // "purchase" or "transition"
	PlanProperties    []PropertyView
	PlanLoans         []LoanView
	// Transition simulation
	HasPlanResult  bool
	PlanResult     *DreamSimResult
	PlanForm       DreamForm
	// Purchase simulation
	HasPurchaseResult  bool
	PurchaseResult     *PurchaseSimResult
}

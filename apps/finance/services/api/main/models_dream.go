package main

import "time"

// DreamForm holds the raw user inputs echoed back to the template.
type DreamForm struct {
	PropertyID             string
	LoanID                 string
	DreamCostCents         int64
	DownPaymentPct         float64
	ConstructionRatePct    float64
	ConstructionTermYears  int // what the user typed (years); converted to months internally
	ConstructionTermMonths int // years * 12
	BuildMonths            int
	MonthlySavingsCents    int64
	ExpectedSalePriceCents int64
}

// DreamSimResult is the computed output of one simulation run.
type DreamSimResult struct {
	// Echoed inputs
	Form DreamForm

	// Current state (from selected property/loan)
	CurrentProperty     *PropertyView
	CurrentLoan         *LoanView
	CurrentMonthlyCents int64

	// Construction loan details
	ConstructionLoanCents  int64
	ConstructionMonthly    int64

	// Phase 1 — save the down payment
	DownPaymentCents  int64
	AlreadyHaveCents  int64 // equity usable today
	StillNeededCents  int64
	Phase1Months      int
	Phase1EndDate     time.Time

	// Phase 2 — construction (both loans running)
	Phase2Months           int
	Phase2EndDate          time.Time
	Phase2MonthlyCents     int64 // mortgage + construction EMI
	ExistingBalanceAtSale  int64 // mortgage balance at time of sale
	ConstructionBalAtSale  int64 // construction loan balance at time of sale

	// Phase 3 — sell current house, pay down construction loan
	SalePriceCents        int64
	MortgagePayoffCents   int64
	NetProceedsCents      int64
	RemainingBalanceCents int64 // construction loan after applying proceeds

	// Phase 4 — just the construction loan
	Phase4MonthlyCents int64
	Phase4Months       int
	Phase4EndDate      time.Time

	// Totals
	TotalMonths        int
	TotalYears         int // TotalMonths / 12
	TotalRemMonths     int // TotalMonths % 12
	FinalDate          time.Time
	TotalInterestCents int64

	Warning string
}

// PurchaseSimResult is the computed output for a simple save-for-purchase goal.
type PurchaseSimResult struct {
	Name                string
	TargetCents         int64
	MonthlySavingsCents int64
	// At-current-savings projection
	MonthsNeeded int
	YearsNeeded  int
	RemMonths    int
	ReachDate    time.Time
	// Deadline projection (only set when a deadline was provided)
	HasDeadline              bool
	DeadlineDate             time.Time
	DeadlineMonths           int
	MonthlyNeededForDeadline int64
	Feasible                 bool // monthly savings >= monthly needed for deadline
}

// DreamData is passed to the dream.html template (kept for compat; redirect goes to /goals).
type DreamData struct {
	UserID string
	Email  string
	Title  string
	Route  string

	Properties []PropertyView
	Loans      []LoanView

	HasResult bool
	Result    *DreamSimResult
	Form      DreamForm
}

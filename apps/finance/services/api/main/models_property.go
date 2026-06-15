package main

import "time"

type PropertyStatus string

const (
	PropertyOwned    PropertyStatus = "owned"
	PropertyBuilding PropertyStatus = "building"
	PropertySold     PropertyStatus = "sold"
)

type LoanType string

const (
	LoanMortgage     LoanType = "mortgage"
	LoanConstruction LoanType = "construction"
	LoanPersonal     LoanType = "personal"
)

type LoanStatus string

const (
	LoanActive  LoanStatus = "active"
	LoanPaidOff LoanStatus = "paid_off"
)

type Property struct {
	ID                 string         `bson:"_id" json:"id"`
	UserID             string         `bson:"user_id" json:"user_id"`
	Name               string         `bson:"name" json:"name"`
	Address            string         `bson:"address,omitempty" json:"address,omitempty"`
	PurchasePriceCents int64          `bson:"purchase_price_cents" json:"purchase_price_cents"`
	CurrentValueCents  int64          `bson:"current_value_cents" json:"current_value_cents"`
	AppreciationPct    float64        `bson:"appreciation_pct" json:"appreciation_pct"` // annual %
	PurchaseDate       time.Time      `bson:"purchase_date" json:"purchase_date"`
	Status             PropertyStatus `bson:"status" json:"status"`
	Notes              string         `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt          time.Time      `bson:"created_at" json:"created_at"`
}

type Loan struct {
	ID                  string     `bson:"_id" json:"id"`
	UserID              string     `bson:"user_id" json:"user_id"`
	PropertyID          string     `bson:"property_id,omitempty" json:"property_id,omitempty"`
	Name                string     `bson:"name" json:"name"`
	Type                LoanType   `bson:"type" json:"type"`
	PrincipalCents      int64      `bson:"principal_cents" json:"principal_cents"`
	BalanceCents        int64      `bson:"balance_cents" json:"balance_cents"`
	InterestRatePct     float64    `bson:"interest_rate_pct" json:"interest_rate_pct"` // annual %
	TermMonths          int        `bson:"term_months" json:"term_months"`
	StartDate           time.Time  `bson:"start_date" json:"start_date"`
	MonthlyPaymentCents int64      `bson:"monthly_payment_cents" json:"monthly_payment_cents"` // 0 = computed
	Status              LoanStatus `bson:"status" json:"status"`
	Notes               string     `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt           time.Time  `bson:"created_at" json:"created_at"`
}

// LoanView enriches a Loan with computed amortization fields — never stored.
type LoanView struct {
	Loan
	EffectiveMonthlyPaymentCents int64
	RemainingMonths              int
	PayoffDate                   time.Time
	TotalRemainingInterestCents  int64
	PaidSoFarCents               int64
	PaidPct                      int64 // int64 so the "sub" template func works
}

// PropertyView enriches a Property with equity and linked loan — never stored.
type PropertyView struct {
	Property
	LinkedLoan  *LoanView
	EquityCents int64
	GainCents   int64
	GainPct     float64
	EquityPct   int64  // int64 so the "sub" template func works
	LoanPct     int64  // 100 - EquityPct, pre-computed for the template
	StatusLabel string
}

type PropertyData struct {
	UserID string
	Email  string
	Title  string
	Route  string

	Properties    []PropertyView
	UnlinkedLoans []LoanView // active loans not attached to any property

	TotalPropertyValueCents int64
	TotalLoanBalanceCents   int64
	TotalEquityCents        int64
}

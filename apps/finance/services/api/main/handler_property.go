package main

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var propertyTmpl = parseTmpl("templates/base.html", "templates/property.html")

// ── Amortization helpers ──────────────────────────────────────────────────────

// loanMonthlyPayment computes the fixed EMI for a standard amortising loan.
// annualRatePct is e.g. 3.2 for 3.2%.
func loanMonthlyPayment(principalCents int64, annualRatePct float64, termMonths int) int64 {
	if termMonths <= 0 {
		return 0
	}
	if annualRatePct == 0 {
		return principalCents / int64(termMonths)
	}
	r := annualRatePct / 12 / 100
	factor := math.Pow(1+r, float64(termMonths))
	return int64(math.Round(float64(principalCents) * r * factor / (factor - 1)))
}

// loanRemainingMonths estimates months to pay off balance at a given monthly payment.
func loanRemainingMonths(balanceCents int64, annualRatePct float64, monthlyPaymentCents int64) int {
	if monthlyPaymentCents <= 0 || balanceCents <= 0 {
		return 0
	}
	if annualRatePct == 0 {
		return int(math.Ceil(float64(balanceCents) / float64(monthlyPaymentCents)))
	}
	r := annualRatePct / 12 / 100
	rB := r * float64(balanceCents)
	M := float64(monthlyPaymentCents)
	if rB >= M {
		return 999 // monthly payment doesn't cover interest
	}
	return int(math.Ceil(-math.Log(1-rB/M) / math.Log(1+r)))
}

func toLoanView(l Loan) LoanView {
	monthly := l.MonthlyPaymentCents
	if monthly == 0 {
		monthly = loanMonthlyPayment(l.PrincipalCents, l.InterestRatePct, l.TermMonths)
	}
	remaining := loanRemainingMonths(l.BalanceCents, l.InterestRatePct, monthly)
	payoff := time.Now().AddDate(0, remaining, 0)

	totalRemainingInterest := monthly*int64(remaining) - l.BalanceCents
	if totalRemainingInterest < 0 {
		totalRemainingInterest = 0
	}
	paid := l.PrincipalCents - l.BalanceCents
	if paid < 0 {
		paid = 0
	}
	var paidPct int64
	if l.PrincipalCents > 0 {
		paidPct = paid * 100 / l.PrincipalCents
	}
	return LoanView{
		Loan:                         l,
		EffectiveMonthlyPaymentCents: monthly,
		RemainingMonths:              remaining,
		PayoffDate:                   payoff,
		TotalRemainingInterestCents:  totalRemainingInterest,
		PaidSoFarCents:               paid,
		PaidPct:                      paidPct,
	}
}

func toPropertyView(p Property, allLoans []Loan) PropertyView {
	equityCents := p.CurrentValueCents
	var linked *LoanView
	for _, l := range allLoans {
		if l.PropertyID == p.ID && l.Status == LoanActive {
			v := toLoanView(l)
			linked = &v
			equityCents -= l.BalanceCents
		}
	}
	gain := p.CurrentValueCents - p.PurchasePriceCents
	gainPct := 0.0
	if p.PurchasePriceCents > 0 {
		gainPct = float64(gain) / float64(p.PurchasePriceCents) * 100
	}
	var equityPct int64
	if p.CurrentValueCents > 0 && equityCents > 0 {
		equityPct = equityCents * 100 / p.CurrentValueCents
	}
	labels := map[PropertyStatus]string{
		PropertyOwned:    "Owned",
		PropertyBuilding: "Building",
		PropertySold:     "Sold",
	}
	return PropertyView{
		Property:    p,
		LinkedLoan:  linked,
		EquityCents: equityCents,
		GainCents:   gain,
		GainPct:     gainPct,
		EquityPct:   equityPct,
		LoanPct:     100 - equityPct,
		StatusLabel: labels[p.Status],
	}
}

// loanBalanceAt returns the outstanding balance after n monthly payments.
// Formula: B_n = P*(1+r)^n - (M/r)*((1+r)^n - 1)
func loanBalanceAt(principalCents int64, annualRatePct float64, monthlyPaymentCents int64, monthsElapsed int) int64 {
	if monthsElapsed <= 0 {
		return principalCents
	}
	if annualRatePct == 0 {
		b := principalCents - monthlyPaymentCents*int64(monthsElapsed)
		if b < 0 {
			return 0
		}
		return b
	}
	r := annualRatePct / 12 / 100
	factor := math.Pow(1+r, float64(monthsElapsed))
	balance := float64(principalCents)*factor - (float64(monthlyPaymentCents)/r)*(factor-1)
	if balance < 0 {
		return 0
	}
	return int64(math.Round(balance))
}

// ── Handler ───────────────────────────────────────────────────────────────────

func (h *Handler) Properties(w http.ResponseWriter, r *http.Request) {
	auth := h.getAuth(r)
	if auth.UserID == "" {
		http.Redirect(w, r, "/auth/login?next=/property", http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.propertiesGET(w, r, auth)
	case http.MethodPost:
		h.propertiesPOST(w, r, auth)
	}
}

func (h *Handler) propertiesGET(w http.ResponseWriter, r *http.Request, auth authInfo) {
	ctx := r.Context()
	props, _ := h.store.getProperties(ctx, auth.UserID)
	loans, _ := h.store.getLoans(ctx, auth.UserID)

	propIDs := map[string]bool{}
	var views []PropertyView
	var totalValue, totalLoan int64

	for _, p := range props {
		propIDs[p.ID] = true
		v := toPropertyView(p, loans)
		views = append(views, v)
		if p.Status != PropertySold {
			totalValue += p.CurrentValueCents
			if v.LinkedLoan != nil {
				totalLoan += v.LinkedLoan.BalanceCents
			}
		}
	}

	var unlinked []LoanView
	for _, l := range loans {
		if l.Status == LoanActive && !propIDs[l.PropertyID] {
			v := toLoanView(l)
			unlinked = append(unlinked, v)
			totalLoan += l.BalanceCents
		}
	}

	render(w, propertyTmpl, PropertyData{
		UserID:                  auth.UserID,
		Email:                   auth.Email,
		Title:                   "Property",
		Route:                   "property",
		Properties:              views,
		UnlinkedLoans:           unlinked,
		TotalPropertyValueCents: totalValue,
		TotalLoanBalanceCents:   totalLoan,
		TotalEquityCents:        totalValue - totalLoan,
	})
}

func (h *Handler) propertiesPOST(w http.ResponseWriter, r *http.Request, auth authInfo) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	action := r.FormValue("action")

	switch action {
	case "add_property":
		purchasePrice := parseFormCents(r.FormValue("purchase_price"))
		currentValue := parseFormCents(r.FormValue("current_value"))
		if currentValue == 0 {
			currentValue = purchasePrice
		}
		appPct, _ := strconv.ParseFloat(r.FormValue("appreciation_pct"), 64)
		purchaseDate, _ := time.Parse("2006-01-02", r.FormValue("purchase_date"))

		p := &Property{
			ID:                 genID(),
			UserID:             auth.UserID,
			Name:               strings.TrimSpace(r.FormValue("name")),
			Address:            strings.TrimSpace(r.FormValue("address")),
			PurchasePriceCents: purchasePrice,
			CurrentValueCents:  currentValue,
			AppreciationPct:    appPct,
			PurchaseDate:       purchaseDate,
			Status:             PropertyStatus(r.FormValue("status")),
			Notes:              strings.TrimSpace(r.FormValue("notes")),
		}
		if p.Status == "" {
			p.Status = PropertyOwned
		}
		_ = h.store.createProperty(ctx, p)

	case "update_property":
		id := r.FormValue("id")
		purchasePrice := parseFormCents(r.FormValue("purchase_price"))
		currentValue := parseFormCents(r.FormValue("current_value"))
		appPct, _ := strconv.ParseFloat(r.FormValue("appreciation_pct"), 64)
		purchaseDate, _ := time.Parse("2006-01-02", r.FormValue("purchase_date"))
		_ = h.store.updateProperty(ctx, id, auth.UserID, bson.M{
			"name":                 strings.TrimSpace(r.FormValue("name")),
			"address":              strings.TrimSpace(r.FormValue("address")),
			"purchase_price_cents": purchasePrice,
			"current_value_cents":  currentValue,
			"appreciation_pct":     appPct,
			"purchase_date":        purchaseDate,
			"status":               PropertyStatus(r.FormValue("status")),
			"notes":                strings.TrimSpace(r.FormValue("notes")),
		})

	case "delete_property":
		_ = h.store.deleteProperty(ctx, r.FormValue("id"), auth.UserID)

	case "add_loan":
		principalCents := parseFormCents(r.FormValue("principal"))
		balanceCents := parseFormCents(r.FormValue("balance"))
		if balanceCents == 0 {
			balanceCents = principalCents
		}
		ratePct, _ := strconv.ParseFloat(r.FormValue("interest_rate"), 64)
		termMonths, _ := strconv.Atoi(r.FormValue("term_months"))
		monthlyCents := parseFormCents(r.FormValue("monthly_payment"))
		startDate, _ := time.Parse("2006-01-02", r.FormValue("start_date"))

		l := &Loan{
			ID:                  genID(),
			UserID:              auth.UserID,
			PropertyID:          r.FormValue("property_id"),
			Name:                strings.TrimSpace(r.FormValue("name")),
			Type:                LoanType(r.FormValue("loan_type")),
			PrincipalCents:      principalCents,
			BalanceCents:        balanceCents,
			InterestRatePct:     ratePct,
			TermMonths:          termMonths,
			StartDate:           startDate,
			MonthlyPaymentCents: monthlyCents,
			Status:              LoanActive,
			Notes:               strings.TrimSpace(r.FormValue("notes")),
		}
		if l.Type == "" {
			l.Type = LoanMortgage
		}
		_ = h.store.createLoan(ctx, l)

	case "update_loan_balance":
		id := r.FormValue("id")
		balanceCents := parseFormCents(r.FormValue("balance"))
		_ = h.store.updateLoan(ctx, id, auth.UserID, bson.M{
			"balance_cents": balanceCents,
		})

	case "payoff_loan":
		_ = h.store.updateLoan(ctx, r.FormValue("id"), auth.UserID, bson.M{
			"status": LoanPaidOff,
		})

	case "delete_loan":
		_ = h.store.deleteLoan(ctx, r.FormValue("id"), auth.UserID)
	}

	http.Redirect(w, r, "/property", http.StatusSeeOther)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func genID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// parseFormCents converts a plain euro amount string (e.g. "180000" or "180000.50") to cents.
func parseFormCents(s string) int64 {
	s = strings.ReplaceAll(s, ",", ".")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f * 100))
}

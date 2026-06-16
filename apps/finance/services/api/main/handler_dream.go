package main

import (
	"math"
	"net/http"
	"strconv"
	"time"
)

// Dream redirects /plan to the Planner tab inside /goals.
func (h *Handler) Dream(w http.ResponseWriter, r *http.Request) {
	target := "/goals?tab=planner"
	if q := r.URL.RawQuery; q != "" {
		target += "&" + q
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// runDreamSim computes the four-phase goal plan.
func runDreamSim(form DreamForm, props []PropertyView, loans []LoanView) *DreamSimResult {
	res := &DreamSimResult{Form: form}

	for i := range props {
		if props[i].ID == form.PropertyID {
			res.CurrentProperty = &props[i]
			break
		}
	}
	for i := range loans {
		if loans[i].ID == form.LoanID {
			res.CurrentLoan = &loans[i]
			break
		}
	}

	if res.CurrentLoan != nil {
		res.CurrentMonthlyCents = res.CurrentLoan.EffectiveMonthlyPaymentCents
	}

	// ── Phase 1: save the down payment ───────────────────────────────────────
	res.DownPaymentCents = int64(math.Round(float64(form.DreamCostCents) * form.DownPaymentPct / 100))
	if res.CurrentProperty != nil {
		res.AlreadyHaveCents = res.CurrentProperty.EquityCents
	}
	res.StillNeededCents = res.DownPaymentCents - res.AlreadyHaveCents
	if res.StillNeededCents < 0 {
		res.StillNeededCents = 0
	}
	if res.StillNeededCents > 0 && form.MonthlySavingsCents > 0 {
		res.Phase1Months = int(math.Ceil(float64(res.StillNeededCents) / float64(form.MonthlySavingsCents)))
	}
	res.Phase1EndDate = time.Now().AddDate(0, res.Phase1Months, 0)

	// ── Construction loan details ─────────────────────────────────────────────
	res.ConstructionLoanCents = form.DreamCostCents - res.DownPaymentCents
	res.ConstructionMonthly = loanMonthlyPayment(res.ConstructionLoanCents, form.ConstructionRatePct, form.ConstructionTermMonths)

	// ── Phase 2: acquisition period (both loans running) ─────────────────────
	res.Phase2Months = form.BuildMonths
	res.Phase2EndDate = res.Phase1EndDate.AddDate(0, res.Phase2Months, 0)
	res.Phase2MonthlyCents = res.CurrentMonthlyCents + res.ConstructionMonthly

	if res.CurrentLoan != nil {
		totalElapsed := res.Phase1Months + res.Phase2Months
		res.ExistingBalanceAtSale = loanBalanceAt(
			res.CurrentLoan.PrincipalCents,
			res.CurrentLoan.InterestRatePct,
			res.CurrentLoan.EffectiveMonthlyPaymentCents,
			totalElapsed,
		)
	}
	res.ConstructionBalAtSale = loanBalanceAt(
		res.ConstructionLoanCents,
		form.ConstructionRatePct,
		res.ConstructionMonthly,
		res.Phase2Months,
	)

	// ── Phase 3: sell current asset ───────────────────────────────────────────
	res.SalePriceCents = form.ExpectedSalePriceCents
	if res.SalePriceCents == 0 && res.CurrentProperty != nil {
		res.SalePriceCents = res.CurrentProperty.CurrentValueCents
	}
	res.MortgagePayoffCents = res.ExistingBalanceAtSale
	res.NetProceedsCents = res.SalePriceCents - res.MortgagePayoffCents
	if res.NetProceedsCents < 0 {
		res.NetProceedsCents = 0
		res.Warning = "Sale proceeds don't cover the remaining loan — you'll need to cover the gap."
	}
	res.RemainingBalanceCents = res.ConstructionBalAtSale - res.NetProceedsCents
	if res.RemainingBalanceCents < 0 {
		res.RemainingBalanceCents = 0
	}

	// ── Phase 4: goal achieved ────────────────────────────────────────────────
	if res.RemainingBalanceCents > 0 {
		res.Phase4Months = loanRemainingMonths(res.RemainingBalanceCents, form.ConstructionRatePct, res.ConstructionMonthly)
		res.Phase4MonthlyCents = res.ConstructionMonthly
		remainingTerm := form.ConstructionTermMonths - res.Phase2Months
		if remainingTerm > 0 && remainingTerm < res.Phase4Months {
			res.Phase4Months = remainingTerm
			res.Phase4MonthlyCents = loanMonthlyPayment(res.RemainingBalanceCents, form.ConstructionRatePct, remainingTerm)
		}
	}
	res.Phase4EndDate = res.Phase2EndDate.AddDate(0, res.Phase4Months, 0)

	// ── Totals ────────────────────────────────────────────────────────────────
	res.TotalMonths = res.Phase1Months + res.Phase2Months + res.Phase4Months
	res.TotalYears = res.TotalMonths / 12
	res.TotalRemMonths = res.TotalMonths % 12
	res.FinalDate = res.Phase4EndDate

	existingInterest := int64(0)
	if res.CurrentLoan != nil {
		totalPaid := res.CurrentMonthlyCents * int64(res.Phase1Months+res.Phase2Months)
		principal := res.CurrentLoan.BalanceCents - res.ExistingBalanceAtSale
		existingInterest = totalPaid - principal
		if existingInterest < 0 {
			existingInterest = 0
		}
	}
	constructionTotalPaid := res.ConstructionMonthly*int64(res.Phase2Months) + res.Phase4MonthlyCents*int64(res.Phase4Months)
	constructionInterest := constructionTotalPaid - res.ConstructionLoanCents
	if constructionInterest < 0 {
		constructionInterest = 0
	}
	res.TotalInterestCents = existingInterest + constructionInterest

	return res
}

func parseFloatParam(s string, def float64) float64 {
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return def
	}
	return v
}

func parseIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return def
	}
	return v
}

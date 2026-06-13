package main

import (
	"testing"
	"time"
)

func mustTime(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestComputeHoldings(t *testing.T) {
	tests := []struct {
		name   string
		trades []Trade
		prices map[string]int64
		want   []Holding
	}{
		{
			name: "single buy",
			trades: []Trade{
				{ISIN: "IE00B0M62X35", Name: "ETF A", Type: "buy", Quantity: 10, PriceCents: 5000, TotalCents: 50000, Date: mustTime("2024-01-01")},
			},
			prices: map[string]int64{"IE00B0M62X35": 5500},
			want: []Holding{
				{ISIN: "IE00B0M62X35", Name: "ETF A", SharesOwned: 10, AvgEntryCents: 5000, TotalCostCents: 50000, CurrentPriceCents: 5500, CurrentValueCents: 5500, UnrealizedPCLCents: -44500, UnrealizedPCLPct: -89},
			},
		},
		{
			name: "buy then sell partial",
			trades: []Trade{
				{ISIN: "IE00B0M62X35", Name: "ETF A", Type: "buy", Quantity: 10, TotalCents: 50000, Date: mustTime("2024-01-01")},
				{ISIN: "IE00B0M62X35", Name: "ETF A", Type: "sell", Quantity: 3, TotalCents: 18000, Date: mustTime("2024-06-01")},
			},
			prices: map[string]int64{"IE00B0M62X35": 5500},
			want: []Holding{
				{ISIN: "IE00B0M62X35", Name: "ETF A", SharesOwned: 7, AvgEntryCents: 5000, TotalCostCents: 35000, CurrentPriceCents: 5500, CurrentValueCents: 3850, UnrealizedPCLCents: -31150, UnrealizedPCLPct: -89},
			},
		},
		{
			name:   "no trades",
			trades: nil,
			prices: map[string]int64{},
			want:   nil,
		},
		{
			name: "all sold",
			trades: []Trade{
				{ISIN: "IE00B0M62X35", Name: "ETF A", Type: "buy", Quantity: 10, TotalCents: 50000, Date: mustTime("2024-01-01")},
				{ISIN: "IE00B0M62X35", Name: "ETF A", Type: "sell", Quantity: 10, TotalCents: 55000, Date: mustTime("2024-06-01")},
			},
			prices: map[string]int64{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeHoldings(tt.trades, tt.prices)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d holdings, want %d\ngot:  %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				h := got[i]
				w := tt.want[i]
				if h.ISIN != w.ISIN || h.Name != w.Name || h.SharesOwned != w.SharesOwned {
					t.Errorf("holding %d = {ISIN:%q Name:%q Shares:%f}, want {ISIN:%q Name:%q Shares:%f}",
						i, h.ISIN, h.Name, h.SharesOwned, w.ISIN, w.Name, w.SharesOwned)
				}
			}
		})
	}
}

func TestAggregatePortfolio(t *testing.T) {
	tests := []struct {
		name     string
		holdings []Holding
		want     PortfolioResult
	}{
		{
			name: "multiple holdings sorted by value",
			holdings: []Holding{
				{TotalCostCents: 10000, CurrentValueCents: 15000},
				{TotalCostCents: 5000, CurrentValueCents: 2000},
				{TotalCostCents: 20000, CurrentValueCents: 25000},
			},
			want: PortfolioResult{
				Holdings:  []Holding{{TotalCostCents: 20000, CurrentValueCents: 25000}, {TotalCostCents: 10000, CurrentValueCents: 15000}, {TotalCostCents: 5000, CurrentValueCents: 2000}},
				TotalCost: 35000,
				TotalVal:  42000,
				TotalPCL:  7000,
				PCLPct:    20,
			},
		},
		{
			name:     "empty holdings",
			holdings: nil,
			want:     PortfolioResult{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := aggregatePortfolio(tt.holdings)
			if got.TotalCost != tt.want.TotalCost {
				t.Errorf("TotalCost = %d, want %d", got.TotalCost, tt.want.TotalCost)
			}
			if got.TotalVal != tt.want.TotalVal {
				t.Errorf("TotalVal = %d, want %d", got.TotalVal, tt.want.TotalVal)
			}
			if got.TotalPCL != tt.want.TotalPCL {
				t.Errorf("TotalPCL = %d, want %d", got.TotalPCL, tt.want.TotalPCL)
			}
			if len(got.Holdings) != len(tt.want.Holdings) {
				t.Fatalf("got %d holdings, want %d", len(got.Holdings), len(tt.want.Holdings))
			}
			for i := range got.Holdings {
				if got.Holdings[i].CurrentValueCents != tt.want.Holdings[i].CurrentValueCents {
					t.Errorf("holding[%d].CurrentValueCents = %d, want %d", i, got.Holdings[i].CurrentValueCents, tt.want.Holdings[i].CurrentValueCents)
				}
			}
		})
	}
}

func TestHoldingsByISIN(t *testing.T) {
	trades := []Trade{
		{ISIN: "IE00B0M62X35"},
		{ISIN: "US0378331005"},
		{ISIN: "IE00B0M62X35"},
		{ISIN: "US0378331005"},
		{ISIN: "IE00B0M62X35"},
	}
	got := holdingsByISIN(trades)
	want := []string{"IE00B0M62X35", "US0378331005"}
	if len(got) != len(want) {
		t.Fatalf("got %d isins, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTopMerchants(t *testing.T) {
	txns := []Transaction{
		{Description: "Supermercado", AmountCents: -5000},
		{Description: "Supermercado", AmountCents: -3000},
		{Description: "Restaurante", AmountCents: -1500},
		{Description: "Uber", AmountCents: -800},
		{Description: "Uber", AmountCents: -1200},
	}
	tests := []struct {
		name  string
		limit int
		want  int
		first string
	}{
		{"limit 2", 2, 2, "Supermercado"},
		{"limit 10", 10, 3, "Supermercado"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := topMerchants(txns, tt.limit)
			if len(got) != tt.want {
				t.Errorf("len = %d, want %d", len(got), tt.want)
			}
			if len(got) > 0 && got[0].Category != tt.first {
				t.Errorf("first = %q, want %q", got[0].Category, tt.first)
			}
		})
	}
}

func TestAutoCategorize(t *testing.T) {
	tests := []struct {
		desc   string
		catMap map[string]string
		want   string
	}{
		{"supermercado pingodoce", nil, "Groceries"},
		{"uber trip", nil, "Transport"},
		{"steam purchase", nil, "Games"},
		{"renda casa", nil, "Housing"},
		{"edp eletricidade", nil, "Utilities"},
		{"mcdonald lunch", nil, "Food"},
		{"zara clothing", nil, "Clothing"},
		{"salario mensal", nil, "Income"},
		{"trade republic deposit", nil, "Investments"},
		{"farmacia benfica", nil, "Health"},
		{"unknown merchant", nil, "Others"},
		{"unknown merchant", map[string]string{"other": "Others"}, "Others"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := autoCategorize(tt.desc, tt.catMap)
			if got != tt.want {
				t.Errorf("autoCategorize(%q) = %q, want %q", tt.desc, got, tt.want)
			}
		})
	}
}

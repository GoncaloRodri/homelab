package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"time"
)

type TickerMapping struct {
	ISIN   string `bson:"_id" json:"isin"`
	Ticker string `bson:"ticker" json:"ticker"`
}

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				RegularMarketPrice float64 `json:"regularMarketPrice"`
			} `json:"meta"`
		} `json:"result"`
	} `json:"chart"`
}

func computeHoldings(trades []Trade, prices map[string]int64) []Holding {
	type acc struct {
		shares       float64
		cost         int64
		realizedPCL  int64
	}

	byISIN := make(map[string]*acc)
	var isinOrder []string

	for _, t := range trades {
		a, ok := byISIN[t.ISIN]
		if !ok {
			a = &acc{}
			byISIN[t.ISIN] = a
			isinOrder = append(isinOrder, t.ISIN)
		}

		if t.Type == "buy" || t.Type == "Buy" {
			a.shares += t.Quantity
			a.cost += t.TotalCents
		} else {
			if a.shares > 0 {
				avgCost := int64(float64(a.cost) / a.shares * t.Quantity)
				a.realizedPCL += t.TotalCents - avgCost
			}
			a.shares -= t.Quantity
			if a.shares < 0 {
				a.shares = 0
			}
		}
	}

	var holdings []Holding
	for _, isin := range isinOrder {
		a := byISIN[isin]
		if a.shares < 0.0001 {
			continue
		}

		var name string
		for _, t := range trades {
			if t.ISIN == isin {
				name = t.Name
				break
			}
		}

		avgEntry := int64(0)
		if a.shares > 0 {
			avgEntry = int64(float64(a.cost) / a.shares)
		}

		currentPrice := prices[isin]

		currentValue := int64(float64(currentPrice) * a.shares / 100)
		unrealizedPCL := currentValue - a.cost
		pct := 0.0
		if a.cost > 0 {
			pct = (float64(unrealizedPCL) / float64(a.cost)) * 100
		}

		holdings = append(holdings, Holding{
			ISIN:              isin,
			Name:              name,
			SharesOwned:       math.Round(a.shares*10000) / 10000,
			AvgEntryCents:     avgEntry,
			TotalCostCents:    a.cost,
			CurrentPriceCents: currentPrice,
			CurrentValueCents: currentValue,
			UnrealizedPCLCents: unrealizedPCL,
			UnrealizedPCLPct:  math.Round(pct*100) / 100,
		})
	}

	return holdings
}

type tickerStore struct {
	mappings []TickerMapping
}

func (s *tickerStore) resolve(isin string) string {
	for _, m := range s.mappings {
		if m.ISIN == isin {
			return m.Ticker
		}
	}
	return ""
}

var defaultTickerMappings = []TickerMapping{}

type TickerStore interface {
	Resolve(isin string) string
	Save(isin, ticker string) error
	Load() error
}

func fetchPrices(tickers []string) (map[string]int64, error) {
	if len(tickers) == 0 {
		return map[string]int64{}, nil
	}

	result := make(map[string]int64)
	client := &http.Client{Timeout: 10 * time.Second}

	for _, ticker := range tickers {
		if ticker == "" {
			continue
		}
		url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s", ticker)
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		var chart yahooChartResponse
		if err := json.Unmarshal(body, &chart); err != nil {
			continue
		}

		if len(chart.Chart.Result) > 0 {
			price := chart.Chart.Result[0].Meta.RegularMarketPrice
			result[ticker] = int64(price * 100)
		}
	}

	return result, nil
}

func holdingsByISIN(trades []Trade) []string {
	seen := make(map[string]bool)
	var result []string
	for _, t := range trades {
		if !seen[t.ISIN] {
			seen[t.ISIN] = true
			result = append(result, t.ISIN)
		}
	}
	return result
}

type PortfolioResult struct {
	Holdings  []Holding
	TotalCost int64
	TotalVal  int64
	TotalPCL  int64
	PCLPct    float64
}

func aggregatePortfolio(holdings []Holding) PortfolioResult {
	var pr PortfolioResult
	for _, h := range holdings {
		pr.Holdings = append(pr.Holdings, h)
		pr.TotalCost += h.TotalCostCents
		pr.TotalVal += h.CurrentValueCents
	}
	pr.TotalPCL = pr.TotalVal - pr.TotalCost
	if pr.TotalCost > 0 {
		pr.PCLPct = math.Round(float64(pr.TotalPCL)/float64(pr.TotalCost)*10000) / 100
	}
	sort.Slice(pr.Holdings, func(i, j int) bool {
		return pr.Holdings[i].CurrentValueCents > pr.Holdings[j].CurrentValueCents
	})
	return pr
}

type CategorySummary struct {
	Category string
	Cents    int64
	Count    int
}

func topMerchants(transactions []Transaction, limit int) []CategorySummary {
	type acc struct {
		cents int64
		count int
	}
	byDesc := make(map[string]*acc)
	for _, t := range transactions {
		a, ok := byDesc[t.Description]
		if !ok {
			a = &acc{}
			byDesc[t.Description] = a
		}
		a.cents += t.AmountCents
		a.count++
	}

	var result []CategorySummary
	for desc, a := range byDesc {
		result = append(result, CategorySummary{Category: desc, Cents: a.cents, Count: a.count})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Cents < result[j].Cents
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

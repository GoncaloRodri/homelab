package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

type CSVFormat string

const (
	FormatCGD          CSVFormat = "cgd"
	FormatTradeRepublic CSVFormat = "traderepublic"
	FormatGeneric      CSVFormat = "generic"
)

type CSVColumnMapping struct {
	DateCol        int
	DescriptionCol int
	AmountCol      int
	TypeCol        int // debit/credit column index; ≥0 means split debit/credit (AmountCol=debit, TypeCol=credit)
	HasHeader      bool
	DateFormat     string
	SkipRows       int
	DecimalSep     string // "." or ","
	// TypeCol is -1 when unused; set it explicitly in the mapping.
}

var CGDMapping = CSVColumnMapping{
	DateCol:        0,
	DescriptionCol: 2,
	AmountCol:      3,
	TypeCol:        4, // Crédito column; AmountCol (3) is Débito (negative), TypeCol (4) is Crédito (positive)
	HasHeader:      true,
	DateFormat:     "02-01-2006",
	DecimalSep:     ",",
}

var TradeRepublicMapping = CSVColumnMapping{
	DateCol:        1,
	DescriptionCol: 17,
	AmountCol:      10,
	TypeCol:        -1,
	HasHeader:      true,
	DateFormat:     "2006-01-02",
	DecimalSep:     ".",
}

func parseCSV(r io.Reader, mapping CSVColumnMapping) ([]CSVImportRow, error) {
	reader := csv.NewReader(r)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	if mapping.DecimalSep == "," {
		reader.Comma = ';'
	} else {
		reader.Comma = ','
	}

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}

	var rows []CSVImportRow
	for _, rec := range records {
		maxCol := mapping.AmountCol
		if mapping.TypeCol >= 0 && mapping.TypeCol > maxCol {
			maxCol = mapping.TypeCol
		}
		if len(rec) <= maxCol || len(rec) <= mapping.DateCol || len(rec) <= mapping.DescriptionCol {
			continue
		}

		dateStr := strings.TrimSpace(rec[mapping.DateCol])
		desc := strings.TrimSpace(rec[mapping.DescriptionCol])

		if dateStr == "" || desc == "" {
			continue
		}

		date, err := time.Parse(mapping.DateFormat, dateStr)
		if err != nil {
			date, err = time.Parse("2006-01-02", dateStr)
			if err != nil {
				date, err = time.Parse("02-01-2006", dateStr)
				if err != nil {
					continue
				}
			}
		}

		var amountStr string
		negative := false
		if mapping.TypeCol >= 0 {
			amountStr = strings.TrimSpace(rec[mapping.TypeCol])
			if amountStr != "" {
				negative = false // Crédito = positive
			} else {
				amountStr = strings.TrimSpace(rec[mapping.AmountCol])
				if amountStr == "" {
					continue
				}
				negative = true // Débito = negative
			}
		} else {
			amountStr = strings.TrimSpace(rec[mapping.AmountCol])
			if amountStr == "" {
				continue
			}
		}

		cents, err := parseCents(amountStr, mapping.DecimalSep)
		if err != nil {
			continue
		}
		if negative {
			cents = -cents
		}

		rows = append(rows, CSVImportRow{
			Date:        date.Format("2006-01-02"),
			Description: desc,
			AmountCents: cents,
		})
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("no valid rows found in CSV")
	}
	return rows, nil
}

func parseCents(s, decimalSep string) (int64, error) {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\u00a0", "")
	s = strings.ReplaceAll(s, "€", "")
	s = strings.TrimSpace(s)

	negative := false
	if strings.HasPrefix(s, "-") {
		negative = true
		s = s[1:]
	}
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		negative = true
		s = s[1 : len(s)-1]
	}

	if decimalSep == "," {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	} else {
		s = strings.ReplaceAll(s, ",", "")
	}

	amount, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse amount %q: %w", s, err)
	}

	cents := int64(math.Round(amount * 100))
	if negative {
		cents = -cents
	}
	if cents == 0 {
		cents = -cents
	}
	return cents, nil
}

type securitiesTradeRow struct {
	Date        string
	Name        string
	ISIN        string
	Type        string // buy/sell
	Quantity    float64
	PriceCents  int64
	TotalCents  int64
}

func parseSecuritiesCSV(r io.Reader) ([]securitiesTradeRow, error) {
	reader := csv.NewReader(r)
	reader.Comma = ','
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read securities csv: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("csv has no data rows")
	}

	headerMap := make(map[string]int)
	for i, h := range records[0] {
		headerMap[strings.ToLower(strings.TrimSpace(h))] = i
	}

	aliases := map[string]string{
		"symbol": "isin",
		"shares": "quantity",
		"amount": "total",
	}
	colMap := map[string]string{
		"date":     "date",
		"name":     "name",
		"isin":     "isin",
		"type":     "type",
		"quantity": "quantity",
		"price":    "price",
		"total":    "total",
		"currency": "currency",
	}
	for _, h := range records[0] {
		h = strings.ToLower(strings.TrimSpace(h))
		if mapped, ok := aliases[h]; ok {
			colMap[mapped] = h
		} else if _, ok := colMap[h]; ok {
			colMap[h] = h
		}
	}
	for _, r := range []string{"date", "name", "isin", "type", "quantity", "price", "total", "currency"} {
		if _, ok := headerMap[colMap[r]]; !ok {
			return nil, fmt.Errorf("missing required column %q in securities CSV", r)
		}
	}

	var trades []securitiesTradeRow
	for i := 1; i < len(records); i++ {
		rec := records[i]
		if len(rec) <= headerMap[colMap["date"]] {
			continue
		}

		t := securitiesTradeRow{
			Date: strings.TrimSpace(rec[headerMap[colMap["date"]]]),
			Name: strings.TrimSpace(rec[headerMap[colMap["name"]]]),
			ISIN: strings.TrimSpace(rec[headerMap[colMap["isin"]]]),
			Type: strings.ToLower(strings.TrimSpace(rec[headerMap[colMap["type"]]])),
		}

		if t.Date == "" || t.Name == "" || t.ISIN == "" {
			continue
		}

		qtyStr := strings.TrimSpace(rec[headerMap[colMap["quantity"]]])
		priceStr := strings.TrimSpace(rec[headerMap[colMap["price"]]])
		totalStr := strings.TrimSpace(rec[headerMap[colMap["total"]]])

		t.Quantity, _ = strconv.ParseFloat(qtyStr, 64)
		t.PriceCents, _ = parseCents(priceStr, ".")
		t.TotalCents, _ = parseCents(totalStr, ".")
		if t.TotalCents < 0 {
			t.TotalCents = -t.TotalCents
		}

		if t.Quantity == 0 && t.TotalCents == 0 {
			continue
		}

		trades = append(trades, t)
	}

	if len(trades) == 0 {
		return nil, fmt.Errorf("no valid trades found in securities CSV")
	}
	return trades, nil
}

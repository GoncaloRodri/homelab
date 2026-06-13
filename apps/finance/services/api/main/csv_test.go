package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseCents(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		decSep  string
		want    int64
		wantErr bool
	}{
		{"positive whole", "100", ".", 10000, false},
		{"positive decimal", "100.50", ".", 10050, false},
		{"negative dash", "-50.25", ".", -5025, false},
		{"negative parens", "(50.25)", ".", -5025, false},
		{"euro symbol", "€1.234,56", ",", 123456, false},
		{"euro negative parens", "(€1.234,56)", ",", -123456, false},
		{"thousands separator", "1,234.56", ".", 123456, false},
		{"space thousand", "1 234.56", ".", 123456, false},
		{"nb space thousand", "1\u00a0234.56", ".", 123456, false},
		{"zero", "0", ".", 0, false},
		{"negative zero", "-0", ".", 0, false},
		{"zero cents", "100.00", ".", 10000, false},
		{"comma decimal", "1234,56", ",", 123456, false},
		{"comma decimal with dot thousand", "1.234,56", ",", 123456, false},
		{"fractional cent rounds up", "19.99", ".", 1999, false},
		{"fractional cent rounds down", "10.01", ".", 1001, false},
		{"euro fractional cent", "-19,99", ",", -1999, false},
		{"empty string", "", ".", 0, true},
		{"invalid", "abc", ".", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCents(tt.input, tt.decSep)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCents(%q, %q) error = %v, wantErr %v", tt.input, tt.decSep, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseCents(%q, %q) = %d, want %d", tt.input, tt.decSep, got, tt.want)
			}
		})
	}
}

func TestParseCSV_CGD(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		wantRows int
		check    func(*testing.T, []CSVImportRow)
	}{
		{
			name:     "basic CGD",
			file:     "testdata/cgd_basic.csv",
			wantRows: 7,
			check: func(t *testing.T, rows []CSVImportRow) {
				if rows[0].Description != "Supermercado Pingo Doce" {
					t.Errorf("first desc = %q", rows[0].Description)
				}
				if rows[0].AmountCents != -5490 {
					t.Errorf("first amount = %d, want -5490 (debit)", rows[0].AmountCents)
				}
				if rows[0].Date != "2024-01-02" {
					t.Errorf("first date = %q", rows[0].Date)
				}
				if rows[4].AmountCents != 250000 {
					t.Errorf("salary (credit) = %d, want 250000", rows[4].AmountCents)
				}
				if rows[6].AmountCents != -8999 {
					t.Errorf("zara (debit) = %d, want -8999", rows[6].AmountCents)
				}
			},
		},
		{
			name:     "real CGD download",
			file:     "testdata/comprovativo.csv",
			wantRows: 36,
			check: func(t *testing.T, rows []CSVImportRow) {
				if rows[0].Description != "EA  ELECTRONIC ARTS" {
					t.Errorf("first desc = %q", rows[0].Description)
				}
				if rows[0].AmountCents != -2399 {
					t.Errorf("ea arts (debit) = %d, want -2399", rows[0].AmountCents)
				}
				if rows[2].AmountCents != 200000 {
					t.Errorf("trf marta (credit) = %d, want 200000", rows[2].AmountCents)
				}
				if rows[5].AmountCents != 200000 {
					t.Errorf("second marta (credit) = %d, want 200000", rows[5].AmountCents)
				}
				if rows[6].AmountCents != -11998 {
					t.Errorf("worten (debit) = %d, want -11998", rows[6].AmountCents)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := parseCSV(strings.NewReader(string(data)), CGDMapping)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tt.wantRows {
				t.Fatalf("got %d rows, want %d", len(rows), tt.wantRows)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func TestParseCSV_TradeRepublic(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		wantRows int
		check    func(*testing.T, []CSVImportRow)
	}{
		{
			name:     "TR transaction export",
			file:     "testdata/traderepublic_card.csv",
			wantRows: 5,
			check: func(t *testing.T, rows []CSVImportRow) {
				if rows[0].AmountCents != 5000 {
					t.Errorf("first transfer = %d, want 5000", rows[0].AmountCents)
				}
				if rows[4].AmountCents != 30000 {
					t.Errorf("second transfer = %d, want 30000", rows[4].AmountCents)
				}
				if rows[0].Date != "2025-12-11" {
					t.Errorf("first date = %q", rows[0].Date)
				}
				if rows[0].Description != "Incoming transfer from GONCALO GOMES RODRIGUES" {
					t.Errorf("first desc = %q", rows[0].Description)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := parseCSV(strings.NewReader(string(data)), TradeRepublicMapping)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tt.wantRows {
				t.Fatalf("got %d rows, want %d", len(rows), tt.wantRows)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func TestParseSecuritiesCSV_FromFile(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		wantTrades int
		check      func(*testing.T, []securitiesTradeRow)
	}{
		{
			name:       "TR securities CSV",
			file:       "testdata/traderepublic_securities.csv",
			wantTrades: 5,
			check: func(t *testing.T, trades []securitiesTradeRow) {
				if trades[0].ISIN != "IE00B3WJKG14" {
					t.Errorf("first ISIN = %q", trades[0].ISIN)
				}
				if trades[0].Type != "buy" || trades[0].TotalCents != 3000 {
					t.Errorf("first trade = %+v", trades[0])
				}
				if trades[2].ISIN != "IE00B5BMR087" || trades[2].TotalCents != 10000 {
					t.Errorf("third trade = %+v", trades[2])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			trades, err := parseSecuritiesCSV(strings.NewReader(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			if len(trades) != tt.wantTrades {
				t.Fatalf("got %d trades, want %d", len(trades), tt.wantTrades)
			}
			if tt.check != nil {
				tt.check(t, trades)
			}
		})
	}
}

func TestParseCSV_EdgeCases(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		_, err := parseCSV(strings.NewReader(""), CGDMapping)
		if err == nil {
			t.Fatal("expected error for empty input")
		}
	})

	t.Run("all rows invalid", func(t *testing.T) {
		csv := "Data;Desc;Amount\ninvalid;test;abc\n"
		_, err := parseCSV(strings.NewReader(csv), CGDMapping)
		if err == nil {
			t.Fatal("expected error for all invalid rows")
		}
	})

	t.Run("generic format detection", func(t *testing.T) {
		csv := "date,description,amount\n2024-01-01,Test,10.50\n"
		mapping := GenericMapping([]byte(csv))
		if mapping.DateFormat != "2006-01-02" {
			t.Errorf("date format = %q, want 2006-01-02", mapping.DateFormat)
		}
		rows, err := parseCSV(strings.NewReader(csv), mapping)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].AmountCents != 1050 {
			t.Errorf("generic parse = %+v", rows[0])
		}
	})
}

func TestParseCSV_TRFullExport(t *testing.T) {
	data, err := os.ReadFile("testdata/Transaction export.csv")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("transactions", func(t *testing.T) {
		rows, err := parseCSV(strings.NewReader(string(data)), TradeRepublicMapping)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 18 {
			t.Fatalf("got %d transaction rows, want 18", len(rows))
		}
		if rows[0].AmountCents != 5000 {
			t.Errorf("first transfer = %d, want 5000", rows[0].AmountCents)
		}
		if rows[7].AmountCents != -10000 {
			t.Errorf("row 7 = %d, want -10000 (buy -100.00)", rows[7].AmountCents)
		}
	})

	t.Run("securities", func(t *testing.T) {
		trades, err := parseSecuritiesCSV(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		if len(trades) != 8 {
			t.Fatalf("got %d trades, want 8", len(trades))
		}
		if trades[0].ISIN != "IE00B3WJKG14" || trades[0].Type != "buy" {
			t.Errorf("first trade = %+v", trades[0])
		}
	})
}

func TestParseSecuritiesCSV_EdgeCases(t *testing.T) {
	t.Run("missing required column", func(t *testing.T) {
		csv := "Date,Name,Type,Quantity\n2024-01-01,Test,Buy,1\n"
		_, err := parseSecuritiesCSV(strings.NewReader(csv))
		if err == nil {
			t.Fatal("expected error for missing columns")
		}
	})

	t.Run("all zero rows skipped", func(t *testing.T) {
		csv := "Date,Name,ISIN,Type,Quantity,Price,Total,Currency\n2024-01-01,,ISIN123,Buy,0,0,0,EUR\n"
		_, err := parseSecuritiesCSV(strings.NewReader(csv))
		if err == nil {
			t.Fatal("expected error for all-zero rows")
		}
	})

	t.Run("no data rows", func(t *testing.T) {
		csv := "Date,Name,ISIN,Type,Quantity,Price,Total,Currency\n"
		_, err := parseSecuritiesCSV(strings.NewReader(csv))
		if err == nil {
			t.Fatal("expected error for header-only CSV")
		}
	})
}

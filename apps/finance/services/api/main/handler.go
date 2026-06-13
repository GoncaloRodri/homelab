package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

//go:embed templates/*.html
var templateFS embed.FS

func parseTmpl(files ...string) *template.Template {
	return template.Must(template.New("").Funcs(template.FuncMap{
		"cents": func(c int64) string {
			sign := ""
			val := c
			if val < 0 {
				sign = "-"
				val = -val
			}
			eur := val / 100
			cent := val % 100
			return fmt.Sprintf("%s%d.%02d", sign, eur, cent)
		},
		"centsAbs": func(c int64) int64 {
			if c < 0 {
				return -c
			}
			return c
		},
		"pctSign": func(f float64) string {
			if f >= 0 {
				return "+"
			}
			return ""
		},
		"dateShort": func(t time.Time) string {
			return t.Format("02 Jan 2006")
		},
		"sub": func(a, b int64) int64 {
			return a - b
		},
		"div": func(a, b int64) float64 {
			if b == 0 {
				return 0
			}
			return float64(a) / float64(b)
		},
		"jsonKeys": func(m map[string]int64) string {
			var keys []string
			for k := range m {
				keys = append(keys, fmt.Sprintf("%q", k))
			}
			return "[" + strings.Join(keys, ",") + "]"
		},
		"abs": func(v int64) int64 {
			if v < 0 {
				return -v
			}
			return v
		},
		"add": func(a, b int64) int64 {
			return a + b
		},
		"mul": func(a, b float64) float64 {
			return a * b
		},
		"round": func(f float64) float64 {
			return math.Round(f)
		},
		"clampPct": func(spent, budget int64) int64 {
			if budget <= 0 {
				return 0
			}
			pct := int64(float64(spent) / float64(budget) * 100)
			if pct > 100 {
				return 100
			}
			if pct < 0 {
				return 0
			}
			return pct
		},
		"isOver": func(spent, budget int64) bool {
			return budget > 0 && spent > budget
		},
		"jsonVals": func(m map[string]int64) template.JS {
			var vals []string
			for _, v := range m {
				vals = append(vals, fmt.Sprintf("%d", v))
			}
			return template.JS("[" + strings.Join(vals, ",") + "]")
		},
	}).ParseFS(templateFS, files...))
}

var (
	baseTmpl        = parseTmpl("templates/base.html")
	dashboardTmpl   = parseTmpl("templates/base.html", "templates/dashboard.html")
	txnsTmpl        = parseTmpl("templates/base.html", "templates/transactions.html")
	importTmpl      = parseTmpl("templates/base.html", "templates/import.html")
	accountsTmpl    = parseTmpl("templates/base.html", "templates/accounts.html")
	categoriesTmpl  = parseTmpl("templates/base.html", "templates/categories.html")
	reportsTmpl     = parseTmpl("templates/base.html", "templates/reports.html")
	projectionsTmpl = parseTmpl("templates/base.html", "templates/projections.html")
	portfolioTmpl   = parseTmpl("templates/base.html", "templates/portfolio.html")
	sharingTmpl     = parseTmpl("templates/base.html", "templates/sharing.html")
)

type authInfo struct {
	UserID string
	Email  string
	Roles  string
}

func getAuth(r *http.Request) authInfo {
	return authInfo{
		UserID: r.Header.Get("X-Auth-User-Id"),
		Email:  r.Header.Get("X-Auth-Email"),
		Roles:  r.Header.Get("X-Auth-Roles"),
	}
}

type userError struct {
	Msg    string
	Status int
}

func (e *userError) Error() string {
	return e.Msg
}

func render(w http.ResponseWriter, tmpl *template.Template, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		slog.Error("template error", "err", err)
	}
}

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) authMW(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := getAuth(r)
		if a.UserID == "" {
			http.Redirect(w, r, "https://auth.homelab.local/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func (h *Handler) ownerOrViewerMW(next http.HandlerFunc) http.HandlerFunc {
	return h.authMW(func(w http.ResponseWriter, r *http.Request) {
		a := getAuth(r)
		ownerID := r.PathValue("user_id")
		if ownerID == "" {
			ownerID = a.UserID
		}
		if ownerID == a.UserID {
			next(w, r)
			return
		}
		perms, err := h.store.getPermissions(r.Context(), ownerID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		for _, p := range perms {
			if p.ViewerID == a.UserID {
				next(w, r)
				return
			}
		}
		render(w, baseTmpl, map[string]interface{}{
			"UserID":  a.UserID,
			"Email":   a.Email,
			"Title":   "Access Denied",
			"Content": template.HTML(`<div class="error-page"><h1>403 - Access Denied</h1><p>You do not have permission to view this user's finances.</p></div>`),
		})
	})
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	now := time.Now()
	thisStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	lastStart := thisStart.AddDate(0, -1, 0)
	lastEnd := thisStart.Add(-time.Nanosecond)

	txns, err := h.store.getTransactions(ctx, a.UserID, bson.M{})
	if err != nil {
		slog.Error("get transactions", "err", err)
		render(w, dashboardTmpl, &DashboardData{UserID: a.UserID, Email: a.Email, Title: "Dashboard", Route: "dashboard", IsOwner: true})
		return
	}

	cats, err := h.store.getCategories(ctx, a.UserID)
	if err != nil {
		slog.Error("get categories", "err", err)
	}
	catNames := make(map[string]string)
	for _, c := range cats {
		catNames[c.Name] = c.Name
	}

	thisMonth := &PeriodSummary{
		TotalCents:    0,
		ByCategory:    make(map[string]int64),
		CategoryNames: catNames,
	}
	lastMonth := &PeriodSummary{
		TotalCents:    0,
		ByCategory:    make(map[string]int64),
		CategoryNames: catNames,
	}

	var recent []Transaction
	var balPoints []BalancePoint
	balByDate := make(map[string]int64)
	var balDates []string

	for _, t := range txns {
		if t.Date.After(thisStart) || t.Date.Equal(thisStart) {
			thisMonth.TotalCents += t.AmountCents
			thisMonth.ByCategory[t.Category] += t.AmountCents
		} else if t.Date.After(lastStart) && t.Date.Before(lastEnd.Add(24*time.Hour)) {
			lastMonth.TotalCents += t.AmountCents
			lastMonth.ByCategory[t.Category] += t.AmountCents
		}

		if len(recent) < 10 {
			recent = append(recent, t)
		}

		day := t.Date.Format("2006-01-02")
		balByDate[day] += t.AmountCents
	}

	for d := range balByDate {
		balDates = append(balDates, d)
	}
	sortStrings(balDates)
	running := int64(0)
	for _, d := range balDates {
		running += balByDate[d]
		parsed, _ := time.Parse("2006-01-02", d)
		balPoints = append(balPoints, BalancePoint{Date: parsed, Cents: running})
	}
	if len(balPoints) > 90 {
		balPoints = balPoints[len(balPoints)-90:]
	}

	// compute income vs expense split for this month
	thisMonthIncome := int64(0)
	thisMonthExpense := int64(0)
	for _, amt := range thisMonth.ByCategory {
		if amt > 0 {
			thisMonthIncome += amt
		} else {
			thisMonthExpense += amt
		}
	}

	// budget data: map category name -> budget cents
	catBudgets := make(map[string]int64)
	catColors := make(map[string]string)
	for _, c := range cats {
		if c.BudgetCents > 0 {
			catBudgets[c.Name] = c.BudgetCents
		}
		catColors[c.Name] = c.Color
	}

	render(w, dashboardTmpl, map[string]interface{}{
		"UserID":             a.UserID,
		"Email":              a.Email,
		"Title":              "Dashboard",
		"Route":              "dashboard",
		"IsOwner":            true,
		"ThisMonth":          thisMonth,
		"LastMonth":          lastMonth,
		"RecentTxns":         recent,
		"BalanceTrend":       balPoints,
		"ThisMonthIncome":    thisMonthIncome,
		"ThisMonthExpense":   thisMonthExpense,
		"CategoryBudgets":    catBudgets,
		"CategoryColors":     catColors,
	})
}

func (h *Handler) Transactions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	var filter bson.M
	cat := r.URL.Query().Get("category")
	search := r.URL.Query().Get("search")
	daysStr := r.URL.Query().Get("days")

	if cat != "" {
		filter = bson.M{"category": cat}
	}
	if daysStr != "" {
		days := 30
		fmt.Sscanf(daysStr, "%d", &days)
		since := time.Now().AddDate(0, 0, -days)
		if filter == nil {
			filter = bson.M{}
		}
		filter["date"] = bson.M{"$gte": since}
	}

	txns, err := h.store.getTransactions(ctx, a.UserID, filter)
	if err != nil {
		slog.Error("get transactions", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if search != "" {
		search = strings.ToLower(search)
		var filtered []Transaction
		for _, t := range txns {
			if strings.Contains(strings.ToLower(t.Description), search) {
				filtered = append(filtered, t)
			}
		}
		txns = filtered
	}

	cats, _ := h.store.getCategories(ctx, a.UserID)
	accounts, _ := h.store.getAccounts(ctx, a.UserID)

	accountNames := make(map[string]string)
	for _, acc := range accounts {
		accountNames[acc.ID] = acc.Name
	}

	catColors := make(map[string]string)
	for _, c := range cats {
		catColors[c.Name] = c.Color
	}

	render(w, txnsTmpl, map[string]interface{}{
		"UserID":       a.UserID,
		"Email":        a.Email,
		"Title":        "Transactions",
		"Route":        "transactions",
		"IsOwner":      true,
		"Txns":         txns,
		"Categories":   cats,
		"Accounts":     accounts,
		"AccountNames": accountNames,
		"CategoryColors": catColors,
		"Cat":          cat,
		"Search":       search,
		"Days":         daysStr,
	})
}

func (h *Handler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	var body struct {
		AccountID   string `json:"account_id"`
		Date        string `json:"date"`
		Description string `json:"description"`
		AmountCents int64  `json:"amount_cents"`
		Category    string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	date, err := time.Parse("2006-01-02", body.Date)
	if err != nil {
		date = time.Now()
	}

	txn := Transaction{
		ID:          bson.NewObjectID().Hex(),
		UserID:      a.UserID,
		AccountID:   body.AccountID,
		Date:        date,
		Description: body.Description,
		AmountCents: body.AmountCents,
		Category:    body.Category,
		CreatedAt:   time.Now(),
	}

	if err := h.store.createTransactions(ctx, []Transaction{txn}); err != nil {
		slog.Error("create transaction", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(txn)
}

func (h *Handler) ImportPage(w http.ResponseWriter, r *http.Request) {
	a := getAuth(r)
	accounts, _ := h.store.getAccounts(r.Context(), a.UserID)
	render(w, importTmpl, map[string]interface{}{
		"UserID":   a.UserID,
		"Email":    a.Email,
		"Title":    "Import",
		"Route":    "import",
		"IsOwner":  true,
		"Accounts": accounts,
		"Preview":  nil,
	})
}

func (h *Handler) ImportPreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		slog.Error("import preview multipart",
			"err", err,
			"content-type", r.Header.Get("Content-Type"),
			"content-length", r.ContentLength,
		)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	accountID := r.FormValue("account_id")
	format := CSVFormat(r.FormValue("format"))

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read file error", http.StatusInternalServerError)
		return
	}

	var mapping CSVColumnMapping
	switch format {
	case FormatCGD:
		mapping = CGDMapping
	case FormatTradeRepublic:
		mapping = TradeRepublicMapping
	default:
		mapping = GenericMapping(data)
	}

	rows, err := parseCSV(strings.NewReader(string(data)), mapping)
	if err != nil {
		accounts, _ := h.store.getAccounts(ctx, a.UserID)
		render(w, importTmpl, map[string]interface{}{
			"UserID":   a.UserID,
			"Email":    a.Email,
			"Title":    "Import",
			"Route":    "import",
			"IsOwner":  true,
			"Accounts": accounts,
			"Error":    err.Error(),
		})
		return
	}

	accounts, _ := h.store.getAccounts(ctx, a.UserID)

	cats, _ := h.store.getCategories(ctx, a.UserID)
	var catList []string
	catMap := make(map[string]string)
	catColors := make(map[string]string)
	if len(cats) == 0 {
		catList = DefaultCategories
		for _, name := range DefaultCategories {
			catMap[strings.ToLower(name)] = name
			if c, ok := DefaultCategoryColors[name]; ok {
				catColors[name] = c
			}
		}
	} else {
		for _, c := range cats {
			catMap[strings.ToLower(c.Name)] = c.Name
			catList = append(catList, c.Name)
			if c.Color != "" {
				catColors[c.Name] = c.Color
			}
		}
	}

	for i := range rows {
		rows[i].Category = autoCategorize(rows[i].Description, catMap)
	}

	importPreview := &CSVImportPreview{
		AccountID: accountID,
		Rows:      rows,
		Total:     len(rows),
	}

	render(w, importTmpl, map[string]interface{}{
		"UserID":          a.UserID,
		"Email":           a.Email,
		"Title":           "Import",
		"Route":           "import",
		"IsOwner":         true,
		"Accounts":        accounts,
		"Preview":         importPreview,
		"Categories":      catList,
		"RawData":         string(data),
		"SelectedFormat":  string(format),
		"SelectedAccount": accountID,
		"CategoryColors":  catColors,
	})
}

func GenericMapping(data []byte) CSVColumnMapping {
	return CSVColumnMapping{
		DateCol:        0,
		DescriptionCol: 1,
		AmountCol:      2,
		TypeCol:        -1,
		HasHeader:      true,
		DateFormat:     "2006-01-02",
		DecimalSep:     ".",
	}
}

func (h *Handler) ImportConfirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	accountID := r.FormValue("account_id")
	format := CSVFormat(r.FormValue("format"))
	rawData := r.FormValue("raw_data")

	var mapping CSVColumnMapping
	switch format {
	case FormatCGD:
		mapping = CGDMapping
	case FormatTradeRepublic:
		mapping = TradeRepublicMapping
	default:
		mapping = GenericMapping([]byte(rawData))
	}

	rows, err := parseCSV(strings.NewReader(rawData), mapping)
	if err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusBadRequest)
		return
	}

	userCats := r.Form["categories"]

	now := time.Now()
	var txns []Transaction
	for i, row := range rows {
		date, _ := time.Parse("2006-01-02", row.Date)
		cat := "Others"
		if i < len(userCats) && userCats[i] != "" {
			cat = userCats[i]
		}

		txns = append(txns, Transaction{
			ID:          bson.NewObjectID().Hex(),
			UserID:      a.UserID,
			AccountID:   accountID,
			Date:        date,
			Description: row.Description,
			AmountCents: row.AmountCents,
			Category:    cat,
			CreatedAt:   now,
		})
	}

	if err := h.store.createTransactions(ctx, txns); err != nil {
		slog.Error("create transactions", "err", err)
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/transactions", http.StatusSeeOther)
}

func autoCategorize(desc string, catMap map[string]string) string {
	desc = strings.ToLower(desc)
	keywords := map[string]string{
		"supermercado": "Groceries", "mercado": "Groceries", "pingo": "Groceries",
		"continente": "Groceries", "lidl": "Groceries", "aldi": "Groceries",
		"auchan": "Groceries", "el corte": "Groceries", "jumbo": "Groceries",
		"restaurante": "Food", "restaurant": "Food", "cafetaria": "Food",
		"padaria": "Food", "pastelaria": "Food", "pizza": "Food",
		"mcdonald": "Food", "burger": "Food", "kfc": "Food",
		"steam": "Games", "playstation": "Games", "nintendo": "Games",
		"xbox": "Games", "epic games": "Games", "gog": "Games",
		"uber": "Transport", "bolt": "Transport", "metro": "Transport",
		"cp -": "Transport", "combust": "Transport", "gasolina": "Transport",
		"electric": "Transport", "parking": "Transport", "portagens": "Transport",
		"via verde": "Transport",
		"renda":     "Housing", "condom": "Housing", "agua": "Housing",
		"edp": "Utilities", "meo": "Utilities", "vodafone": "Utilities",
		"nos": "Utilities", "internet": "Utilities", "telecom": "Utilities",
		"farmacia": "Health", "hospital": "Health", "medico": "Health",
		"dentista": "Health", "seguro": "Health",
		"zara": "Clothing", "hm": "Clothing", "nike": "Clothing",
		"adidas": "Clothing", "primark": "Clothing",
		"salario": "Income", "wage": "Income", "salary": "Income",
		"pension": "Income", "rendimento": "Income",
		"trade republic": "Investments", "etf": "Investments", "degiro": "Investments",
		"xbox game pass": "Games",
	}

	for kw, cat := range keywords {
		if strings.Contains(desc, kw) {
			if name, ok := catMap[strings.ToLower(cat)]; ok {
				return name
			}
			return cat
		}
	}

	for name := range catMap {
		if strings.Contains(desc, name) {
			return catMap[name]
		}
	}

	if name, ok := catMap["other"]; ok {
		return name
	}
	return "Others"
}

func (h *Handler) UpdateTransaction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)
	id := r.PathValue("id")

	var body struct {
		Category    string `json:"category"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	update := bson.M{}
	if body.Category != "" {
		update["category"] = body.Category
	}
	if body.Description != "" {
		update["description"] = body.Description
	}

	if err := h.store.updateTransaction(ctx, id, a.UserID, update); err != nil {
		slog.Error("update transaction", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) DeleteTransaction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)
	id := r.PathValue("id")
	if err := h.store.deleteTransaction(ctx, id, a.UserID); err != nil {
		slog.Error("delete transaction", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Accounts(w http.ResponseWriter, r *http.Request) {
	a := getAuth(r)

	switch r.Method {
	case http.MethodGet:
		accounts, err := h.store.getAccounts(r.Context(), a.UserID)
		if err != nil {
			slog.Error("get accounts", "err", err)
		}
		render(w, accountsTmpl, map[string]interface{}{
			"UserID":   a.UserID,
			"Email":    a.Email,
			"Title":    "Accounts",
			"Route":    "accounts",
			"IsOwner":  true,
			"Accounts": accounts,
		})

	case http.MethodPost:
		name := r.FormValue("name")
		acctType := r.FormValue("type")
		if name == "" || acctType == "" {
			http.Error(w, "name and type required", http.StatusBadRequest)
			return
		}
		acct := &Account{
			ID:     bson.NewObjectID().Hex(),
			UserID: a.UserID,
			Name:   name,
			Type:   acctType,
		}
		if err := h.store.createAccount(r.Context(), acct); err != nil {
			slog.Error("create account", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/accounts", http.StatusSeeOther)

	case http.MethodDelete:
		id := r.PathValue("id")
		if err := h.store.deleteAccount(r.Context(), id, a.UserID); err != nil {
			slog.Error("delete account", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	a := getAuth(r)

	switch r.Method {
	case http.MethodGet:
		cats, err := h.store.getCategories(r.Context(), a.UserID)
		if err != nil {
			slog.Error("get categories", "err", err)
		}
		render(w, categoriesTmpl, map[string]interface{}{
			"UserID":     a.UserID,
			"Email":      a.Email,
			"Title":      "Categories",
			"Route":      "categories",
			"IsOwner":    true,
			"Categories": cats,
		})

	case http.MethodPost:
		name := r.FormValue("name")
		color := r.FormValue("color")
		if name == "" || color == "" {
			http.Error(w, "name and color required", http.StatusBadRequest)
			return
		}
		cat := &Category{
			ID:     bson.NewObjectID().Hex(),
			UserID: a.UserID,
			Name:   name,
			Color:  color,
		}
		if err := h.store.createCategory(r.Context(), cat); err != nil {
			slog.Error("create category", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/categories", http.StatusSeeOther)

	case http.MethodPut:
		id := r.PathValue("id")
		var body struct {
			Name        string `json:"name"`
			Color       string `json:"color"`
			BudgetCents int64  `json:"budget_cents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		cat := &Category{
			ID:          id,
			UserID:      a.UserID,
			Name:        body.Name,
			Color:       body.Color,
			BudgetCents: body.BudgetCents,
		}
		if err := h.store.updateCategory(r.Context(), cat); err != nil {
			slog.Error("update category", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	case http.MethodDelete:
		id := r.PathValue("id")
		if err := h.store.deleteCategory(r.Context(), id, a.UserID); err != nil {
			slog.Error("delete category", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) Reports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	txns, err := h.store.getTransactions(ctx, a.UserID, bson.M{})
	if err != nil {
		slog.Error("get transactions", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	cats, _ := h.store.getCategories(ctx, a.UserID)
	catNames := make(map[string]string)
	catColors := make(map[string]string)
	for _, c := range cats {
		catNames[c.Name] = c.Name
		catColors[c.Name] = c.Color
	}

	monthly := make(map[string]map[string]int64)
	for _, t := range txns {
		key := t.Date.Format("2006-01")
		if monthly[key] == nil {
			monthly[key] = make(map[string]int64)
		}
		monthly[key][t.Category] += t.AmountCents
	}

	now := time.Now()
	var monthlyData []MonthlyCategorySummary
	for i := 11; i >= 0; i-- {
		m := now.AddDate(0, -i, 0)
		key := m.Format("2006-01")
		data := MonthlyCategorySummary{
			Month:  m.Format("Jan 2006"),
			Totals: monthly[key],
		}
		if data.Totals == nil {
			data.Totals = make(map[string]int64)
		}
		monthlyData = append(monthlyData, data)
	}

	render(w, reportsTmpl, map[string]interface{}{
		"UserID":         a.UserID,
		"Email":          a.Email,
		"Title":          "Reports",
		"Route":          "reports",
		"IsOwner":        true,
		"MonthlyData":    monthlyData,
		"CategoryNames":  catNames,
		"CategoryColors": catColors,
		"Year":           now.Year(),
	})
}

func (h *Handler) Projections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	txns, err := h.store.getTransactions(ctx, a.UserID, bson.M{})
	if err != nil {
		slog.Error("get transactions", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	cats, _ := h.store.getCategories(ctx, a.UserID)
	catNames := make(map[string]string)
	for _, c := range cats {
		catNames[c.Name] = c.Name
	}

	now := time.Now()
	sixMonthsAgo := now.AddDate(0, -6, 0)

	spendByCat := make(map[string]int64)
	totalSpend := int64(0)
	monthCount := 0
	currentMonth := ""

	for _, t := range txns {
		if t.Date.Before(sixMonthsAgo) || t.AmountCents >= 0 {
			continue
		}
		m := t.Date.Format("2006-01")
		if m != currentMonth {
			monthCount++
			currentMonth = m
		}
		spendByCat[t.Category] += t.AmountCents
		totalSpend += t.AmountCents
	}

	if monthCount == 0 {
		monthCount = 1
	}

	monthlyAvg := make(map[string]float64)
	for cat, total := range spendByCat {
		avg := math.Round(float64(-total)/float64(monthCount)*100) / 100
		if avg > 0 {
			monthlyAvg[cat] = avg
		}
	}

	annualTotal := int64(math.Round(float64(-totalSpend) / float64(monthCount) * 12))

	render(w, projectionsTmpl, map[string]interface{}{
		"UserID":        a.UserID,
		"Email":         a.Email,
		"Title":         "Projections",
		"Route":         "projections",
		"IsOwner":       true,
		"MonthlyAvg":    monthlyAvg,
		"AnnualTotal":   annualTotal,
		"CategoryNames": catNames,
	})
}

func (h *Handler) Portfolio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	trades, err := h.store.getTrades(ctx, a.UserID)
	if err != nil {
		slog.Error("get trades", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	tickers := holdingsByISIN(trades)
	if len(tickers) == 0 {
		render(w, portfolioTmpl, &PortfolioData{
			UserID: a.UserID,
			Email:  a.Email,
			Title:  "Portfolio",
			Route:  "portfolio",
		})
		return
	}

	prices, err := fetchPrices(tickers)
	if err != nil {
		slog.Error("fetch prices", "err", err)
	}

	holdings := computeHoldings(trades, prices)
	pr := aggregatePortfolio(holdings)

	render(w, portfolioTmpl, &PortfolioData{
		UserID:          a.UserID,
		Email:           a.Email,
		Title:           "Portfolio",
		Route:           "portfolio",
		Holdings:        pr.Holdings,
		TotalValueCents: pr.TotalVal,
		TotalCostCents:  pr.TotalCost,
		TotalPCLCents:   pr.TotalPCL,
		TotalPCLPct:     pr.PCLPct,
	})
}

func (h *Handler) ImportSecurities(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}

	rows, err := parseSecuritiesCSV(strings.NewReader(string(data)))
	if err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now()
	var trades []Trade
	for _, row := range rows {
		date, _ := time.Parse("2006-01-02", row.Date)
		trades = append(trades, Trade{
			ID:         bson.NewObjectID().Hex(),
			UserID:     a.UserID,
			ISIN:       row.ISIN,
			Name:       row.Name,
			Type:       row.Type,
			Quantity:   row.Quantity,
			PriceCents: row.PriceCents,
			TotalCents: row.TotalCents,
			Date:       date,
			CreatedAt:  now,
		})
	}

	if err := h.store.createTrades(ctx, trades); err != nil {
		slog.Error("create trades", "err", err)
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/portfolio", http.StatusSeeOther)
}

func (h *Handler) Sharing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	switch r.Method {
	case http.MethodGet:
		perms, err := h.store.getPermissions(ctx, a.UserID)
		if err != nil {
			slog.Error("get permissions", "err", err)
		}

		granted, err := h.store.getGrantedViewers(ctx, a.UserID)
		if err != nil {
			slog.Error("get granted", "err", err)
		}

		viewerIDs := make(map[string]bool)
		for _, p := range perms {
			viewerIDs[p.ViewerID] = true
		}

		type userInfo struct {
			ID    string `bson:"_id" json:"id"`
			Email string `bson:"email" json:"email"`
		}

		var viewers []SharingUser
		for viewerID := range viewerIDs {
			viewers = append(viewers, SharingUser{ID: viewerID, Email: viewerID})
		}

		render(w, sharingTmpl, map[string]interface{}{
			"UserID":  a.UserID,
			"Email":   a.Email,
			"Title":   "Sharing",
			"Route":   "sharing",
			"IsOwner": true,
			"Grants":  perms,
			"Viewers": viewers,
			"Granted": granted,
		})

	case http.MethodPost:
		viewerID := r.FormValue("viewer_id")
		if viewerID == "" || viewerID == a.UserID {
			http.Error(w, "invalid viewer", http.StatusBadRequest)
			return
		}

		existing, _ := h.store.getPermissions(ctx, a.UserID)
		for _, p := range existing {
			if p.ViewerID == viewerID {
				http.Redirect(w, r, "/sharing", http.StatusSeeOther)
				return
			}
		}

		perm := &Permission{
			ID:        bson.NewObjectID().Hex(),
			OwnerID:   a.UserID,
			ViewerID:  viewerID,
			CreatedAt: time.Now(),
		}
		if err := h.store.createPermission(ctx, perm); err != nil {
			slog.Error("create permission", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/sharing", http.StatusSeeOther)

	case http.MethodDelete:
		viewerID := r.PathValue("viewer_id")
		if err := h.store.deletePermission(ctx, a.UserID, viewerID); err != nil {
			slog.Error("delete permission", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) SearchUsers(w http.ResponseWriter, r *http.Request) {
	a := getAuth(r)
	q := r.URL.Query().Get("q")
	if q == "" || len(q) < 2 {
		json.NewEncoder(w).Encode([]map[string]string{})
		return
	}

	resp, err := http.Get(fmt.Sprintf("http://users/admin/users?search=%s", q))
	if err != nil {
		json.NewEncoder(w).Encode([]map[string]string{})
		return
	}
	defer resp.Body.Close()

	var users []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		json.NewEncoder(w).Encode([]map[string]string{})
		return
	}

	var results []map[string]string
	for _, u := range users {
		id, _ := u["id"].(string)
		email, _ := u["email"].(string)
		if id != a.UserID {
			results = append(results, map[string]string{"id": id, "email": email})
		}
	}

	json.NewEncoder(w).Encode(results)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.Dashboard)
	mux.HandleFunc("GET /transactions", h.Transactions)
	mux.HandleFunc("GET /import", h.ImportPage)
	mux.HandleFunc("POST /import/preview", h.ImportPreview)
	mux.HandleFunc("POST /import/confirm", h.ImportConfirm)
	mux.HandleFunc("POST /import/securities", h.ImportSecurities)
	mux.HandleFunc("GET /accounts", h.Accounts)
	mux.HandleFunc("POST /accounts", h.Accounts)
	mux.HandleFunc("DELETE /accounts/{id}", h.Accounts)
	mux.HandleFunc("GET /categories", h.Categories)
	mux.HandleFunc("POST /categories", h.Categories)
	mux.HandleFunc("PUT /categories/{id}", h.Categories)
	mux.HandleFunc("DELETE /categories/{id}", h.Categories)
	mux.HandleFunc("GET /reports", h.Reports)
	mux.HandleFunc("GET /projections", h.Projections)
	mux.HandleFunc("GET /portfolio", h.Portfolio)
	mux.HandleFunc("GET /sharing", h.Sharing)
	mux.HandleFunc("POST /sharing", h.Sharing)
	mux.HandleFunc("DELETE /sharing/{viewer_id}", h.Sharing)
	mux.HandleFunc("GET /api/users/search", h.SearchUsers)
	mux.HandleFunc("POST /api/transactions", h.CreateTransaction)
	mux.HandleFunc("PUT /api/transactions/{id}", h.UpdateTransaction)
	mux.HandleFunc("DELETE /api/transactions/{id}", h.DeleteTransaction)
}

func sortStrings(s []string) {
	sort.Strings(s)
}

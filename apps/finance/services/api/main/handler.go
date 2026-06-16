package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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
		"dateInput": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("2006-01-02")
		},
		"statusColor": func(s string) string {
			switch s {
			case "approved", "paid", "delivered", "settled", "received", "reconciled", "done":
				return "rgba(74,222,128,0.12); color:var(--green)"
			case "submitted", "under_review", "review", "ordered", "disbursed", "pending_payment":
				return "rgba(99,179,237,0.12); color:#63b3ed"
			case "rejected", "cancelled", "disputed":
				return "rgba(239,68,68,0.12); color:var(--red)"
			case "info_requested", "settlement_due", "partial_settlement":
				return "rgba(251,191,36,0.1); color:#fbbf24"
			default:
				return "var(--bg3); color:var(--text3)"
			}
		},
		"avatarEmojis": func() []string {
			return []string{
				"👥", "🚀", "⚡", "🎯", "🏆", "💡", "🔥", "🌊", "🎨", "🛠️",
				"📊", "🎪", "🌿", "🦋", "🏄", "🎸", "🔬", "📡", "🏗️", "🎭",
				"🌍", "🦁", "🐉", "🦅", "🐋", "🌙", "⭐", "🍀", "🔮", "🎲",
			}
		},
		"teamAvatar": func(t OrgTeam) string {
			if t.Avatar != "" {
				return t.Avatar
			}
			return "👥"
		},
		"varColor": func(planned, actual int64) string {
			if planned == 0 {
				return "var(--text2)"
			}
			if actual > planned {
				return "var(--red)"
			}
			return "var(--green)"
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

// parseStandalone parses a single template file that has no {{define}} blocks.
// parseTmpl roots on "", but ParseFS stores content under the base filename,
// so Execute() would run the empty root. Here we root on the base filename so
// Execute() runs the actual content.
func parseStandalone(file string) *template.Template {
	name := file[strings.LastIndex(file, "/")+1:]
	return template.Must(template.New(name).ParseFS(templateFS, file))
}

var (
	homepageTmpl     = parseStandalone("templates/homepage.html")
	authLoginTmpl    = parseStandalone("templates/auth_login.html")
	authRegisterTmpl = parseStandalone("templates/auth_register.html")
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
	goalsTmpl       = parseTmpl("templates/base.html", "templates/goals.html")
	networthTmpl    = parseTmpl("templates/base.html", "templates/networth.html")
	simulatorTmpl   = parseTmpl("templates/base.html", "templates/simulator.html")
	taxTmpl         = parseTmpl("templates/base.html", "templates/tax.html")
	householdTmpl   = parseTmpl("templates/base.html", "templates/household.html")
	autoImportTmpl  = parseTmpl("templates/base.html", "templates/auto_import.html")
	peopleTmpl      = parseTmpl("templates/base.html", "templates/people.html")
	settingsTmpl    = parseTmpl("templates/base.html", "templates/settings.html")

	// Org — list/create/join stay on personal base; inner org pages use business base
	orgListTmpl   = parseTmpl("templates/base.html", "templates/org_list.html")
	orgCreateTmpl = parseTmpl("templates/base.html", "templates/org_create.html")
	orgJoinTmpl   = parseTmpl("templates/base.html", "templates/org_join.html")

	orgHomeTmpl          = parseTmpl("templates/base_org.html", "templates/org_home.html")
	orgTeamsTmpl         = parseTmpl("templates/base_org.html", "templates/org_teams.html")
	orgMembersTmpl       = parseTmpl("templates/base_org.html", "templates/org_members.html")
	orgInviteTmpl        = parseTmpl("templates/base_org.html", "templates/org_invite.html")
	orgEventsTmpl        = parseTmpl("templates/base_org.html", "templates/org_events.html")
	orgEventDetailTmpl   = parseTmpl("templates/base_org.html", "templates/org_event_detail.html")
	orgRequestsTmpl      = parseTmpl("templates/base_org.html", "templates/org_requests.html")
	orgRequestDetailTmpl = parseTmpl("templates/base_org.html", "templates/org_request_detail.html")
	orgLedgerTmpl        = parseTmpl("templates/base_org.html", "templates/org_ledger.html")
	orgBankImportTmpl    = parseTmpl("templates/base_org.html", "templates/org_bank_import.html")
	orgAnalysisTmpl      = parseTmpl("templates/base_org.html", "templates/org_analysis.html")
	orgReportTmpl        = parseTmpl("templates/base_org.html", "templates/org_report.html")
)

type authInfo struct {
	UserID string
	Email  string
	Roles  string
}

// getAuth resolves the current user from the session cookie first, then falls
// back to X-Auth-* headers (Traefik forward-auth / tests).
func (h *Handler) getAuth(r *http.Request) authInfo {
	if a, ok := h.authFromSession(r); ok {
		return a
	}
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

func renderOrg(w http.ResponseWriter, tmpl *template.Template, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base_org.html", data); err != nil {
		slog.Error("template error", "err", err)
	}
}

func renderRaw(w http.ResponseWriter, tmpl *template.Template, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("template error", "err", err)
	}
}

type storeIface interface {
	getAccounts(ctx context.Context, userID string) ([]Account, error)
	getAccount(ctx context.Context, id string) (*Account, error)
	createAccount(ctx context.Context, a *Account) error
	deleteAccount(ctx context.Context, id, userID string) error
	getCategories(ctx context.Context, userID string) ([]Category, error)
	createCategory(ctx context.Context, c *Category) error
	updateCategory(ctx context.Context, c *Category) error
	deleteCategory(ctx context.Context, id, userID string) error
	getTransactions(ctx context.Context, userID string, filter bson.M) ([]Transaction, error)
	getTransaction(ctx context.Context, id, userID string) (*Transaction, error)
	createTransactions(ctx context.Context, txns []Transaction) error
	updateTransaction(ctx context.Context, id, userID string, update bson.M) error
	deleteTransaction(ctx context.Context, id, userID string) error
	aggregateTransactions(ctx context.Context, userID string, pipeline bson.A) ([]bson.M, error)
	getTrades(ctx context.Context, userID string) ([]Trade, error)
	createTrades(ctx context.Context, trades []Trade) error
	deleteTrade(ctx context.Context, id, userID string) error
	getPermissions(ctx context.Context, ownerID string) ([]Permission, error)
	getGrantedViewers(ctx context.Context, viewerID string) ([]Permission, error)
	createPermission(ctx context.Context, p *Permission) error
	deletePermission(ctx context.Context, ownerID, viewerID string) error
	getGoals(ctx context.Context, userID string) ([]Goal, error)
	createGoal(ctx context.Context, g *Goal) error
	updateGoal(ctx context.Context, id, userID string, update bson.M) error
	deleteGoal(ctx context.Context, id, userID string) error
	seedCategories(ctx context.Context, userID string) error
	getTickerMappings(ctx context.Context, userID string) ([]TickerMapping, error)
	saveTickerMapping(ctx context.Context, userID, isin, ticker string) error
	getHousehold(ctx context.Context, userID string) (*Household, error)
	createHousehold(ctx context.Context, h *Household) error
	deleteHousehold(ctx context.Context, userID string) error
	getImportSchedules(ctx context.Context, userID string) ([]ImportSchedule, error)
	createImportSchedule(ctx context.Context, sched *ImportSchedule) error
	deleteImportSchedule(ctx context.Context, id, userID string) error

	// Property & Loan
	getProperties(ctx context.Context, userID string) ([]Property, error)
	getProperty(ctx context.Context, id, userID string) (*Property, error)
	createProperty(ctx context.Context, p *Property) error
	updateProperty(ctx context.Context, id, userID string, update bson.M) error
	deleteProperty(ctx context.Context, id, userID string) error
	getLoans(ctx context.Context, userID string) ([]Loan, error)
	getLoan(ctx context.Context, id, userID string) (*Loan, error)
	createLoan(ctx context.Context, l *Loan) error
	updateLoan(ctx context.Context, id, userID string, update bson.M) error
	deleteLoan(ctx context.Context, id, userID string) error

	// Org
	getOrgsForUser(ctx context.Context, userID string) ([]OrgWithRole, error)
	getOrg(ctx context.Context, orgID string) (*Org, error)
	getOrgBySlug(ctx context.Context, slug string) (*Org, error)
	createOrg(ctx context.Context, o *Org) error
	slugExists(ctx context.Context, slug string) (bool, error)
	getTeams(ctx context.Context, orgID string) ([]OrgTeam, error)
	getTeam(ctx context.Context, teamID, orgID string) (*OrgTeam, error)
	createTeam(ctx context.Context, t *OrgTeam) error
	deleteTeam(ctx context.Context, teamID, orgID string) error
	getMembers(ctx context.Context, orgID string) ([]OrgMember, error)
	getMember(ctx context.Context, orgID, userID string) (*OrgMember, error)
	createMember(ctx context.Context, m *OrgMember) error
	updateMemberRole(ctx context.Context, memberID, orgID string, role OrgRole) error
	removeMember(ctx context.Context, memberID, orgID string) error
	getInvites(ctx context.Context, orgID string) ([]OrgInvite, error)
	getInviteByToken(ctx context.Context, token string) (*OrgInvite, error)
	createInvite(ctx context.Context, inv *OrgInvite) error
	consumeInvite(ctx context.Context, inviteID string) error
	revokeInvite(ctx context.Context, inviteID, orgID string) error
	getFiscalYears(ctx context.Context, orgID string) ([]FiscalYear, error)
	getFiscalYear(ctx context.Context, yearID, orgID string) (*FiscalYear, error)
	getActiveFiscalYear(ctx context.Context, orgID string) (*FiscalYear, error)
	createFiscalYear(ctx context.Context, y *FiscalYear) error
	updateFiscalYearStatus(ctx context.Context, yearID, orgID string, status FiscalYearStatus, extraSet bson.M) error
	getEvents(ctx context.Context, orgID, fiscalYearID string) ([]OrgEvent, error)
	getEvent(ctx context.Context, eventID, orgID string) (*OrgEvent, error)
	createEvent(ctx context.Context, e *OrgEvent) error
	updateEvent(ctx context.Context, eventID, orgID string, update bson.M) error
	deleteEvent(ctx context.Context, eventID, orgID string) error
	addGoalItem(ctx context.Context, eventID, orgID string, goal EventGoal) error
	toggleGoalItem(ctx context.Context, eventID, orgID, goalID string, done bool, doneBy string) error
	deleteGoalItem(ctx context.Context, eventID, orgID, goalID string) error
	getBudgetLines(ctx context.Context, eventID, orgID string) ([]BudgetLine, error)
	createBudgetLine(ctx context.Context, l *BudgetLine) error
	deleteBudgetLine(ctx context.Context, lineID, orgID string) error
	getEventComments(ctx context.Context, eventID, orgID string) ([]EventComment, error)
	createEventComment(ctx context.Context, c *EventComment) error
	getTxRequests(ctx context.Context, orgID string, filter bson.M) ([]TxRequest, error)
	getTxRequest(ctx context.Context, reqID, orgID string) (*TxRequest, error)
	createTxRequest(ctx context.Context, r *TxRequest) error
	appendStatusLog(ctx context.Context, reqID, orgID string, entry StatusLogEntry) error
	updateTxRequest(ctx context.Context, reqID, orgID string, update bson.M) error
	getLedgerEntries(ctx context.Context, orgID, fiscalYearID string, extra bson.M) ([]OrgLedgerEntry, error)
	createLedgerEntry(ctx context.Context, e *OrgLedgerEntry) error
	updateLedgerEntry(ctx context.Context, id, orgID string, update bson.M) error
	getAttachments(ctx context.Context, requestID, orgID string) ([]OrgAttachment, error)
	createAttachment(ctx context.Context, a *OrgAttachment) error

	// Auth
	createAuthUser(ctx context.Context, u *AuthUser) error
	findAuthUserByEmail(ctx context.Context, email string) (*AuthUser, error)
	findAuthUserByProvider(ctx context.Context, provider, providerID string) (*AuthUser, error)
	createAuthSession(ctx context.Context, sess *AuthSession) error
	getAuthSession(ctx context.Context, id string) (*AuthSession, error)
	deleteAuthSession(ctx context.Context, id string) error
}

type Handler struct {
	store        storeIface
	secret       string // HMAC key for session tokens
	googleID     string
	googleSecret string
	baseURL      string // used to build OAuth redirect URLs and detect HTTPS
	loginRL      *loginRateLimiter
}

func NewHandler(store *Store, secret, googleID, googleSecret, baseURL string) *Handler {
	return &Handler{
		store:        store,
		secret:       secret,
		googleID:     googleID,
		googleSecret: googleSecret,
		baseURL:      baseURL,
		loginRL:      newLoginRateLimiter(),
	}
}

// securityHeaders adds defence-in-depth HTTP headers to every response.
func (h *Handler) securityHeaders(next http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' cdn.jsdelivr.net", // Chart.js + inline scripts in templates
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
	}, "; ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", csp)
		if h.isSecure() {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) authMW(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := h.getAuth(r)
		if a.UserID == "" {
			http.Redirect(w, r, "/auth/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		next(w, r)
	}
}

func (h *Handler) ownerOrViewerMW(next http.HandlerFunc) http.HandlerFunc {
	return h.authMW(func(w http.ResponseWriter, r *http.Request) {
		a := h.getAuth(r)
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

func (h *Handler) Homepage(w http.ResponseWriter, r *http.Request) {
	a := h.getAuth(r)
	renderRaw(w, homepageTmpl, map[string]interface{}{
		"Email":  a.Email,
		"UserID": a.UserID,
	})
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)

	now := time.Now()
	thisStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	lastStart := thisStart.AddDate(0, -1, 0)
	threeMonthsAgo := thisStart.AddDate(0, -3, 0)

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
	catColors := make(map[string]string)
	catBudgets := make(map[string]int64)
	catNames := make(map[string]string)
	for _, c := range cats {
		catNames[c.Name] = c.Name
		catColors[c.Name] = c.Color
		// exclude fixed categories from budget health — they're committed costs, not variable spend
		if c.BudgetCents > 0 && !FixedCategories[c.Name] {
			catBudgets[c.Name] = c.BudgetCents
		}
	}

	thisMonth := &PeriodSummary{ByCategory: make(map[string]int64), CategoryNames: catNames}
	lastMonth := &PeriodSummary{ByCategory: make(map[string]int64), CategoryNames: catNames}

	// fixed spending by category over the last 3 months (for recurring detection)
	fixedByMonth := make(map[string]map[int]int64) // category -> month-offset -> total

	var recent []Transaction
	var balPoints []BalancePoint
	balByDate := make(map[string]int64)
	var balDates []string

	for _, t := range txns {
		isThisMonth := !t.Date.Before(thisStart)
		isLastMonth := !t.Date.Before(lastStart) && t.Date.Before(thisStart)
		isRecent3 := !t.Date.Before(threeMonthsAgo) && t.Date.Before(thisStart)

		if isThisMonth {
			thisMonth.TotalCents += t.AmountCents
			thisMonth.ByCategory[t.Category] += t.AmountCents
		} else if isLastMonth {
			lastMonth.TotalCents += t.AmountCents
			lastMonth.ByCategory[t.Category] += t.AmountCents
		}

		// accumulate fixed category spending over last 3 months
		if isRecent3 && FixedCategories[t.Category] && t.AmountCents < 0 {
			mo := int(t.Date.Month())
			if fixedByMonth[t.Category] == nil {
				fixedByMonth[t.Category] = make(map[int]int64)
			}
			fixedByMonth[t.Category][mo] += -t.AmountCents
		}

		if len(recent) < 5 {
			recent = append(recent, t)
		}

		day := t.Date.Format("2006-01-02")
		balByDate[day] += t.AmountCents
		balDates = appendIfMissing(balDates, day)
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

	// income / expense split
	thisMonthIncome := int64(0)
	thisMonthExpense := int64(0)
	for _, amt := range thisMonth.ByCategory {
		if amt > 0 {
			thisMonthIncome += amt
		} else {
			thisMonthExpense += -amt
		}
	}
	lastMonthIncome := int64(0)
	lastMonthSavings := int64(0)
	for _, amt := range lastMonth.ByCategory {
		if amt > 0 {
			lastMonthIncome += amt
		}
	}
	lastMonthSavings = lastMonth.TotalCents
	if lastMonthSavings < 0 {
		lastMonthSavings = 0
	}

	// detect recurring fixed expenses (average over last 3 months)
	var recurringExpenses []RecurringExpense
	totalFixedCents := int64(0)
	for cat, byMonth := range fixedByMonth {
		total := int64(0)
		for _, v := range byMonth {
			total += v
		}
		avg := total / int64(len(byMonth))
		recurringExpenses = append(recurringExpenses, RecurringExpense{Category: cat, MonthlyCents: avg})
		totalFixedCents += avg
	}
	sort.Slice(recurringExpenses, func(i, j int) bool {
		return recurringExpenses[i].MonthlyCents > recurringExpenses[j].MonthlyCents
	})

	// disposable income = income - fixed recurring
	disposableIncome := thisMonthIncome - totalFixedCents

	// deduct committed goal contributions from disposable and add to fixed costs list
	committedGoalsCents := int64(0)
	if goals, err := h.store.getGoals(ctx, a.UserID); err == nil {
		now2 := time.Now()
		for _, g := range goals {
			if !g.Committed {
				continue
			}
			remaining := g.TargetCents - g.SavedCents
			if remaining <= 0 {
				continue
			}
			ml := int64(monthsBetween(now2, g.Deadline))
			if ml < 1 {
				ml = 1
			}
			monthly := remaining / ml
			committedGoalsCents += monthly
			recurringExpenses = append(recurringExpenses, RecurringExpense{
				Category:     g.Name,
				MonthlyCents: monthly,
				IsGoal:       true,
			})
		}
	}
	disposableIncome -= committedGoalsCents
	totalCommittedCents := totalFixedCents + committedGoalsCents

	// variable spend so far this month (non-fixed categories, expenses only)
	variableSpent := int64(0)
	for cat, amt := range thisMonth.ByCategory {
		if !FixedCategories[cat] && amt < 0 {
			variableSpent += -amt
		}
	}

	availableToSpend := disposableIncome - variableSpent
	if availableToSpend < 0 {
		availableToSpend = 0
	}

	// month progress
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
	monthProgressPct := int(float64(now.Day()) / float64(daysInMonth) * 100)

	// % of disposable already spent
	monthSpentPct := 0
	if disposableIncome > 0 {
		monthSpentPct = int(float64(variableSpent) / float64(disposableIncome) * 100)
		if monthSpentPct > 100 {
			monthSpentPct = 100
		}
	}

	// safety buffer = 2 weeks of average daily variable spend over last month
	lastMonthVariableSpent := int64(0)
	for cat, amt := range lastMonth.ByCategory {
		if !FixedCategories[cat] && amt < 0 {
			lastMonthVariableSpent += -amt
		}
	}
	safetyBuffer := lastMonthVariableSpent / 2

	// bank should be = upcoming fixed costs (not yet paid this month) + safety buffer
	fixedPaidThisMonth := int64(0)
	for cat, amt := range thisMonth.ByCategory {
		if FixedCategories[cat] && amt < 0 {
			fixedPaidThisMonth += -amt
		}
	}
	bankShouldBe := (totalFixedCents - fixedPaidThisMonth) + safetyBuffer

	// savings rate
	savingsRatePct := 0
	if thisMonthIncome > 0 {
		saved := thisMonthIncome - thisMonthExpense
		if saved > 0 {
			savingsRatePct = int(float64(saved) / float64(thisMonthIncome) * 100)
		}
	}
	lastMonthSavingsRatePct := 0
	if lastMonthIncome > 0 && lastMonthSavings > 0 {
		lastMonthSavingsRatePct = int(float64(lastMonthSavings) / float64(lastMonthIncome) * 100)
	}

	// portfolio snapshot — degrade to cost basis if live prices are unavailable
	var portfolioValueCents, portfolioPCLCents int64
	var portfolioHoldings []Holding
	var portfolioPricesAvailable bool
	if trades, err := h.store.getTrades(ctx, a.UserID); err == nil && len(trades) > 0 {
		prices, _ := fetchPricesByISIN(uniqueISINs(trades), nil)
		holdings := computeHoldings(trades, prices)
		pr := aggregatePortfolio(holdings)
		portfolioHoldings = pr.Holdings

		// check whether any prices came back
		for _, p := range prices {
			if p > 0 {
				portfolioPricesAvailable = true
				break
			}
		}

		if portfolioPricesAvailable {
			portfolioValueCents = pr.TotalVal
			portfolioPCLCents = pr.TotalPCL
		} else {
			// fall back to cost basis so the card is still useful
			portfolioValueCents = pr.TotalCost
		}
	}

	// ── Property equity (for net worth card on dashboard) ───────────────
	var dashPropertyEquity int64
	if dProps, err2 := h.store.getProperties(ctx, a.UserID); err2 == nil {
		dLoans, _ := h.store.getLoans(ctx, a.UserID)
		for _, p := range dProps {
			if p.Status != PropertySold {
				dashPropertyEquity += p.CurrentValueCents
			}
		}
		for _, l := range dLoans {
			if l.Status == LoanActive {
				dashPropertyEquity -= l.BalanceCents
			}
		}
	}

	// ── Alerts ──────────────────────────────────────────────────────────
	var alerts []Alert

	// budget overspend alerts — compare per-category spend vs budget
	for cat, budget := range catBudgets {
		spent := -thisMonth.ByCategory[cat] // expenses are negative
		if spent <= 0 || budget <= 0 {
			continue
		}
		pct := int(float64(spent) / float64(budget) * 100)
		if pct >= 100 {
			alerts = append(alerts, Alert{
				Level:   AlertDanger,
				Message: fmt.Sprintf("You've exceeded your %s budget (€%.0f of €%.0f — %d%%).", cat, float64(spent)/100, float64(budget)/100, pct),
			})
		} else if pct >= 80 && monthProgressPct < 80 {
			alerts = append(alerts, Alert{
				Level:   AlertWarn,
				Message: fmt.Sprintf("You've used %d%% of your %s budget but only %d%% of the month has passed.", pct, cat, monthProgressPct),
			})
		}
	}

	// goal deadline risk alerts
	if goalList, err := h.store.getGoals(ctx, a.UserID); err == nil {
		threeAgo := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -3, 0)
		moSavings := make(map[int]int64)
		for _, t := range txns {
			if !t.Date.Before(threeAgo) && t.Date.Before(thisStart) {
				moSavings[int(t.Date.Month())] += t.AmountCents
			}
		}
		var totalS int64
		for _, s := range moSavings {
			if s > 0 {
				totalS += s
			}
		}
		avgS := int64(0)
		if len(moSavings) > 0 {
			avgS = totalS / int64(len(moSavings))
		}
		for _, g := range goalList {
			remaining := g.TargetCents - g.SavedCents
			if remaining <= 0 {
				continue
			}
			ml := int64(monthsBetween(now, g.Deadline))
			if ml < 1 {
				ml = 1
			}
			needed := remaining / ml
			if avgS < needed {
				monthsOff := int64(0)
				if avgS > 0 {
					monthsOff = remaining/avgS - ml
				}
				msg := fmt.Sprintf("You're on track to miss your \"%s\" goal", g.Name)
				if monthsOff > 0 {
					msg += fmt.Sprintf(" by %d month(s)", monthsOff)
				}
				msg += fmt.Sprintf(" — need €%.0f/mo but saving ~€%.0f/mo.", float64(needed)/100, float64(avgS)/100)
				alerts = append(alerts, Alert{Level: AlertWarn, Message: msg})
			}
		}
	}

	// overall spend pace alert
	if monthProgressPct > 0 && monthSpentPct > monthProgressPct+20 {
		alerts = append(alerts, Alert{
			Level:   AlertWarn,
			Message: fmt.Sprintf("You've spent %d%% of your disposable income but only %d%% of the month has passed — you're ahead of pace.", monthSpentPct, monthProgressPct),
		})
	}

	render(w, dashboardTmpl, &DashboardData{
		UserID:                  a.UserID,
		Email:                   a.Email,
		Title:                   "Dashboard",
		Route:                   "dashboard",
		IsOwner:                 true,
		ThisMonth:               thisMonth,
		LastMonth:               lastMonth,
		RecentTxns:              recent,
		BalanceTrend:            balPoints,
		ThisMonthIncome:         thisMonthIncome,
		ThisMonthExpense:        thisMonthExpense,
		CategoryBudgets:         catBudgets,
		CategoryColors:          catColors,
		AvailableToSpend:        availableToSpend,
		DisposableIncome:        disposableIncome,
		MonthProgressPct:        monthProgressPct,
		MonthSpentPct:           monthSpentPct,
		RecurringExpenses:       recurringExpenses,
		BankShouldBe:            bankShouldBe,
		SafetyBufferCents:       safetyBuffer,
		TotalCommittedCents:     totalCommittedCents,
		SavingsRatePct:          savingsRatePct,
		LastMonthSavingsRatePct: lastMonthSavingsRatePct,
		PortfolioValueCents:          portfolioValueCents,
		PortfolioPCLCents:            portfolioPCLCents,
		PortfolioHoldings:            portfolioHoldings,
		PortfolioPricesAvailable:     portfolioPricesAvailable,
		NetWorthCents:                portfolioValueCents + running + dashPropertyEquity,
		Alerts:                       alerts,
	})
}

func (h *Handler) Transactions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)

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
		"UserID":         a.UserID,
		"Email":          a.Email,
		"Title":          "Transactions",
		"Route":          "transactions",
		"IsOwner":        true,
		"Txns":           txns,
		"Categories":     cats,
		"Accounts":       accounts,
		"AccountNames":   accountNames,
		"CategoryColors": catColors,
		"Cat":            cat,
		"Search":         search,
		"Days":           daysStr,
		"Notice":         r.URL.Query().Get("notice"),
	})
}

func (h *Handler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)

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
	a := h.getAuth(r)
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
	a := h.getAuth(r)

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

	// compute fingerprints and detect duplicates
	var fingerprints []string
	for i := range rows {
		rows[i].Category = autoCategorize(rows[i].Description, catMap)
		rows[i].Fingerprint = txnFingerprint(rows[i].Date, rows[i].Description, rows[i].AmountCents, accountID)
		fingerprints = append(fingerprints, rows[i].Fingerprint)
	}
	existing, _ := h.store.getTransactions(ctx, a.UserID, bson.M{"bank_ref": bson.M{"$in": fingerprints}})
	existingRefs := map[string]bool{}
	for _, t := range existing {
		existingRefs[t.BankRef] = true
	}
	duplicateCount := 0
	for i := range rows {
		if existingRefs[rows[i].Fingerprint] {
			rows[i].Duplicate = true
			duplicateCount++
		}
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
		"DuplicateCount":  duplicateCount,
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
	a := h.getAuth(r)

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

	// compute fingerprints and skip duplicates
	var fingerprints []string
	for _, row := range rows {
		fingerprints = append(fingerprints, txnFingerprint(row.Date, row.Description, row.AmountCents, accountID))
	}
	existing, _ := h.store.getTransactions(ctx, a.UserID, bson.M{"bank_ref": bson.M{"$in": fingerprints}})
	existingRefs := map[string]bool{}
	for _, t := range existing {
		existingRefs[t.BankRef] = true
	}

	now := time.Now()
	var txns []Transaction
	for i, row := range rows {
		fp := fingerprints[i]
		if existingRefs[fp] {
			continue
		}
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
			BankRef:     fp,
			CreatedAt:   now,
		})
	}

	if len(txns) == 0 {
		http.Redirect(w, r, "/transactions?notice=all_duplicates", http.StatusSeeOther)
		return
	}

	if err := h.store.createTransactions(ctx, txns); err != nil {
		slog.Error("create transactions", "err", err)
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/transactions", http.StatusSeeOther)
}

func txnFingerprint(date, description string, amountCents int64, accountID string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%s", date, description, amountCents, accountID)))
	return hex.EncodeToString(h[:])[:16]
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
	a := h.getAuth(r)
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
	a := h.getAuth(r)
	id := r.PathValue("id")
	if err := h.store.deleteTransaction(ctx, id, a.UserID); err != nil {
		slog.Error("delete transaction", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Accounts(w http.ResponseWriter, r *http.Request) {
	a := h.getAuth(r)

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
	a := h.getAuth(r)

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
	a := h.getAuth(r)

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
	a := h.getAuth(r)

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
	monthlyTotal := float64(annualTotal) / 12

	// pre-compute pace percentage per category for the template (avoids float/int type issues)
	type catProjection struct {
		Name        string
		MonthlyAvg  float64
		AnnualTotal float64
		PacePct     int
	}
	cats2, _ := h.store.getCategories(ctx, a.UserID)
	catColors2 := make(map[string]string)
	for _, c := range cats2 {
		catColors2[c.Name] = c.Color
	}

	var projections []catProjection
	for cat, avg := range monthlyAvg {
		pct := 0
		if monthlyTotal > 0 {
			pct = int(math.Round(avg / monthlyTotal * 100))
			if pct > 100 {
				pct = 100
			}
		}
		projections = append(projections, catProjection{
			Name:        cat,
			MonthlyAvg:  avg,
			AnnualTotal: avg * 12,
			PacePct:     pct,
		})
	}
	// sort by monthly avg descending
	sort.Slice(projections, func(i, j int) bool {
		return projections[i].MonthlyAvg > projections[j].MonthlyAvg
	})

	render(w, projectionsTmpl, map[string]interface{}{
		"UserID":        a.UserID,
		"Email":         a.Email,
		"Title":         "Projections",
		"Route":         "projections",
		"IsOwner":       true,
		"Projections":   projections,
		"AnnualTotal":   annualTotal,
		"CategoryColors": catColors2,
	})
}

func (h *Handler) Portfolio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)

	trades, err := h.store.getTrades(ctx, a.UserID)
	if err != nil {
		slog.Error("get trades", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	isins := uniqueISINs(trades)
	if len(isins) == 0 {
		render(w, portfolioTmpl, &PortfolioData{
			UserID: a.UserID,
			Email:  a.Email,
			Title:  "Portfolio",
			Route:  "portfolio",
		})
		return
	}

	// load user-saved ISIN→ticker overrides
	customMappings, _ := h.store.getTickerMappings(ctx, a.UserID)
	custom := make(map[string]string, len(customMappings))
	for _, m := range customMappings {
		custom[m.ISIN] = m.Ticker
	}

	prices, err := fetchPricesByISIN(isins, custom)
	if err != nil {
		slog.Error("fetch prices", "err", err)
	}

	// collect ISINs for which we got no price
	var missingPrices []string
	for _, isin := range isins {
		if prices[isin] == 0 {
			missingPrices = append(missingPrices, isin)
		}
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
		MissingPrices:   missingPrices,
	})
}

func (h *Handler) SaveTickerMapping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)
	isin := strings.TrimSpace(r.FormValue("isin"))
	ticker := strings.TrimSpace(r.FormValue("ticker"))
	if isin == "" || ticker == "" {
		http.Error(w, "isin and ticker required", http.StatusBadRequest)
		return
	}
	if err := h.store.saveTickerMapping(ctx, a.UserID, isin, ticker); err != nil {
		slog.Error("save ticker mapping", "err", err)
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/portfolio", http.StatusSeeOther)
}

func (h *Handler) ImportSecurities(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)

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
	a := h.getAuth(r)

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
	a := h.getAuth(r)
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

func (h *Handler) Goals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		action := r.FormValue("action")

		if action == "delete" {
			id := r.FormValue("id")
			h.store.deleteGoal(ctx, id, a.UserID)
			http.Redirect(w, r, "/goals", http.StatusSeeOther)
			return
		}

		if action == "commit" || action == "uncommit" {
			id := r.FormValue("id")
			h.store.updateGoal(ctx, id, a.UserID, bson.M{"committed": action == "commit"})
			http.Redirect(w, r, "/goals", http.StatusSeeOther)
			return
		}

		// create goal
		name := r.FormValue("name")
		goalType := GoalType(r.FormValue("type"))
		targetStr := r.FormValue("target_euros")
		deadlineStr := r.FormValue("deadline")

		targetEuros := parseFloat(targetStr)
		deadline, _ := time.Parse("2006-01", deadlineStr)

		g := &Goal{
			ID:          bson.NewObjectID().Hex(),
			UserID:      a.UserID,
			Name:        name,
			Type:        goalType,
			TargetCents: int64(targetEuros * 100),
			Deadline:    deadline,
			CreatedAt:   time.Now(),
		}
		if err := h.store.createGoal(ctx, g); err != nil {
			slog.Error("create goal", "err", err)
		}
		http.Redirect(w, r, "/goals", http.StatusSeeOther)
		return
	}

	goals, err := h.store.getGoals(ctx, a.UserID)
	if err != nil {
		slog.Error("get goals", "err", err)
	}

	// compute average monthly savings over last 3 months
	txns, _ := h.store.getTransactions(ctx, a.UserID, bson.M{})
	now := time.Now()
	threeMonthsAgo := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -3, 0)
	monthlySavings := make(map[int]int64)
	for _, t := range txns {
		if !t.Date.Before(threeMonthsAgo) && t.Date.Before(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())) {
			monthlySavings[int(t.Date.Month())] += t.AmountCents
		}
	}
	var totalSavings int64
	for _, s := range monthlySavings {
		if s > 0 {
			totalSavings += s
		}
	}
	avgMonthlySavings := int64(0)
	if len(monthlySavings) > 0 {
		avgMonthlySavings = totalSavings / int64(len(monthlySavings))
	}

	// compute disposable income from this month's transactions
	thisStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	thisMonthIncome := int64(0)
	fixedThisMonth := int64(0)
	for _, t := range txns {
		if t.Date.Before(thisStart) {
			continue
		}
		if t.AmountCents > 0 {
			thisMonthIncome += t.AmountCents
		}
		if FixedCategories[t.Category] && t.AmountCents < 0 {
			fixedThisMonth += -t.AmountCents
		}
	}
	disposable := thisMonthIncome - fixedThisMonth

	// build goal plans
	var plans []GoalPlan
	for _, g := range goals {
		remaining := g.TargetCents - g.SavedCents
		if remaining < 0 {
			remaining = 0
		}

		monthsLeft := int64(monthsBetween(now, g.Deadline))
		if monthsLeft < 1 {
			monthsLeft = 1
		}

		monthlyCents := remaining / monthsLeft

		monthsAtRate := int64(0)
		if avgMonthlySavings > 0 {
			monthsAtRate = remaining / avgMonthlySavings
		}

		progressPct := int64(0)
		if g.TargetCents > 0 {
			progressPct = int64(float64(g.SavedCents) / float64(g.TargetCents) * 100)
			if progressPct > 100 {
				progressPct = 100
			}
		}

		plans = append(plans, GoalPlan{
			Goal:                g,
			MonthsLeft:          monthsLeft,
			MonthlyCents:        monthlyCents,
			ImpactOnDisposable:  disposable - monthlyCents,
			MonthsAtCurrentRate: monthsAtRate,
			Feasible:            avgMonthlySavings >= monthlyCents,
			ProgressPct:         progressPct,
		})
	}

	// sum committed goal contributions and detect conflicts
	committedTotal := int64(0)
	for _, p := range plans {
		if p.Committed {
			committedTotal += p.MonthlyCents
		}
	}
	remainingDisposable := disposable - committedTotal

	conflictWarning := ""
	if committedTotal > disposable {
		// find which committed goals are in conflict
		var conflictNames []string
		for _, p := range plans {
			if p.Committed {
				conflictNames = append(conflictNames, p.Name)
			}
		}
		conflictWarning = fmt.Sprintf(
			"Your committed goals require €%.0f/month but your disposable income is €%.0f/month. Consider pushing back a deadline or removing a goal.",
			float64(committedTotal)/100, float64(disposable)/100,
		)
		_ = conflictNames
	}

	render(w, goalsTmpl, &GoalsData{
		UserID:                a.UserID,
		Email:                 a.Email,
		Title:                 "Goals",
		Route:                 "goals",
		Goals:                 plans,
		AvgMonthlySavings:     avgMonthlySavings,
		DisposableIncome:      disposable,
		CommittedMonthlyCents: committedTotal,
		RemainingDisposable:   remainingDisposable,
		ConflictWarning:       conflictWarning,
	})
}

func monthsBetween(from, to time.Time) int {
	months := (to.Year()-from.Year())*12 + int(to.Month()) - int(from.Month())
	if months < 0 {
		return 0
	}
	return months
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func (h *Handler) Simulator(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)

	txns, _ := h.store.getTransactions(ctx, a.UserID, bson.M{})
	goals, _ := h.store.getGoals(ctx, a.UserID)

	now := time.Now()
	thisStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	// this month income + fixed costs
	thisMonthIncome := int64(0)
	totalFixed := int64(0)
	fixedByMonth := make(map[string]map[int]int64)
	threeAgo := thisStart.AddDate(0, -3, 0)
	monthlySavings := make(map[string]struct{ income, saved int64 })

	for _, t := range txns {
		mk := t.Date.Format("2006-01")
		if t.Date.Before(thisStart) {
			// savings history: accumulate per month
			ms := monthlySavings[mk]
			if t.AmountCents > 0 {
				ms.income += t.AmountCents
			}
			ms.saved += t.AmountCents
			monthlySavings[mk] = ms

			// fixed category detection over last 3 months
			if !t.Date.Before(threeAgo) && FixedCategories[t.Category] && t.AmountCents < 0 {
				mo := int(t.Date.Month())
				if fixedByMonth[t.Category] == nil {
					fixedByMonth[t.Category] = make(map[int]int64)
				}
				fixedByMonth[t.Category][mo] += -t.AmountCents
			}
		} else {
			if t.AmountCents > 0 {
				thisMonthIncome += t.AmountCents
			}
		}
	}

	// avg monthly fixed from last 3 months
	for _, byMo := range fixedByMonth {
		total := int64(0)
		for _, v := range byMo {
			total += v
		}
		totalFixed += total / int64(len(byMo))
	}

	// committed goal monthly totals
	goalsCents := int64(0)
	var simGoals []SimulatorGoal
	for _, g := range goals {
		remaining := g.TargetCents - g.SavedCents
		if remaining <= 0 {
			continue
		}
		ml := int64(monthsBetween(now, g.Deadline))
		if ml < 1 {
			ml = 1
		}
		monthly := remaining / ml
		if g.Committed {
			goalsCents += monthly
		}
		simGoals = append(simGoals, SimulatorGoal{
			Name:         g.Name,
			MonthlyCents: monthly,
			MonthsLeft:   ml,
			Committed:    g.Committed,
		})
	}

	disposable := thisMonthIncome - totalFixed - goalsCents

	// average monthly savings over last 3 complete months
	var avgSavings int64
	count := 0
	for _, ms := range monthlySavings {
		if ms.income > 0 && ms.saved > 0 {
			avgSavings += ms.saved
			count++
		}
	}
	if count > 0 {
		avgSavings /= int64(count)
	}

	// savings rate history — sorted months
	var monthKeys []string
	for mk := range monthlySavings {
		monthKeys = append(monthKeys, mk)
	}
	sortStrings(monthKeys)
	var history []SavingsPoint
	for _, mk := range monthKeys {
		ms := monthlySavings[mk]
		if ms.income <= 0 {
			continue
		}
		rate := int(float64(ms.saved) / float64(ms.income) * 100)
		if rate < -100 {
			rate = -100
		}
		history = append(history, SavingsPoint{
			Month:       mk,
			IncomeCents: ms.income,
			SavedCents:  ms.saved,
			RatePct:     rate,
		})
	}

	render(w, simulatorTmpl, &SimulatorData{
		UserID:          a.UserID,
		Email:           a.Email,
		Title:           "What If",
		Route:           "simulator",
		IncomeCents:     thisMonthIncome,
		FixedCents:      totalFixed,
		GoalsCents:      goalsCents,
		DisposableCents: disposable,
		AvgSavingsCents: avgSavings,
		Goals:           simGoals,
		SavingsHistory:  history,
	})
}

func (h *Handler) NetWorth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)

	txns, err := h.store.getTransactions(ctx, a.UserID, bson.M{})
	if err != nil {
		slog.Error("networth get transactions", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// build a running total per month for cash (non-credit accounts treated as assets,
	// credit accounts as liabilities)
	// We don't have account-type info on transactions, so we use signing convention:
	// Income category = income (+), everything else = expense (−).
	// For a simple net-worth history: sum all transaction amounts cumulatively per month.
	type monthKey = string
	monthCash := make(map[monthKey]int64)
	var months []string
	seen := make(map[monthKey]bool)

	// cumulative running balance across all txns sorted by date (store returns desc; reverse)
	// running cumulative balance — reset is not possible so we track running sum
	runningBalance := int64(0)
	// txns are sorted desc by date; reverse to process oldest first
	for i := len(txns) - 1; i >= 0; i-- {
		t := txns[i]
		runningBalance += t.AmountCents
		mk := t.Date.Format("2006-01")
		monthCash[mk] = runningBalance
		if !seen[mk] {
			seen[mk] = true
			months = append(months, mk)
		}
	}
	sortStrings(months)

	// current cash = running balance at end of all transactions
	cashCents := runningBalance

	// portfolio
	var portfolioCents int64
	var pricesAvailable bool
	if trades, err2 := h.store.getTrades(ctx, a.UserID); err2 == nil && len(trades) > 0 {
		prices, _ := fetchPricesByISIN(uniqueISINs(trades), nil)
		holdings := computeHoldings(trades, prices)
		pr := aggregatePortfolio(holdings)
		for _, p := range prices {
			if p > 0 {
				pricesAvailable = true
				break
			}
		}
		if pricesAvailable {
			portfolioCents = pr.TotalVal
		} else {
			portfolioCents = pr.TotalCost
		}
	}

	// ── Property equity ──────────────────────────────────────────────────────────
	var propertyValueCents, loanBalanceCents int64
	props, _ := h.store.getProperties(ctx, a.UserID)
	loans, _ := h.store.getLoans(ctx, a.UserID)

	for _, p := range props {
		if p.Status != PropertySold {
			propertyValueCents += p.CurrentValueCents
		}
	}
	for _, l := range loans {
		if l.Status == LoanActive {
			loanBalanceCents += l.BalanceCents
		}
	}
	propertyEquityCents := propertyValueCents - loanBalanceCents
	netWorthCents := cashCents + portfolioCents + propertyEquityCents

	// build history points — cash snapshot + portfolio + property equity (amortised)
	var history []NetWorthPoint
	for _, m := range months {
		cash := monthCash[m]

		// For each month in history, compute what the loan balance was at that point
		// using standard amortisation: B_n = P*(1+r)^n - (M/r)*((1+r)^n - 1)
		histLoanBalance := int64(0)
		for _, l := range loans {
			if l.Status != LoanActive {
				continue
			}
			t, _ := time.Parse("2006-01", m)
			monthsElapsed := monthsBetween(l.StartDate, t)
			if monthsElapsed < 0 {
				// loan didn't exist yet — exclude its balance from this month
				continue
			}
			monthly := l.MonthlyPaymentCents
			if monthly == 0 {
				monthly = loanMonthlyPayment(l.PrincipalCents, l.InterestRatePct, l.TermMonths)
			}
			histLoanBalance += loanBalanceAt(l.PrincipalCents, l.InterestRatePct, monthly, monthsElapsed)
		}
		// property value is static (current estimate — we don't have historical valuations)
		histEquity := propertyValueCents - histLoanBalance

		history = append(history, NetWorthPoint{
			Month:      m,
			AssetCents: cash + portfolioCents + propertyValueCents,
			LiabCents:  histLoanBalance,
			NetCents:   cash + portfolioCents + histEquity,
		})
	}

	render(w, networthTmpl, &NetWorthData{
		UserID:                   a.UserID,
		Email:                    a.Email,
		Title:                    "Net Worth",
		Route:                    "networth",
		CashCents:                cashCents,
		PortfolioCents:           portfolioCents,
		CreditCents:              0,
		PropertyValueCents:       propertyValueCents,
		LoanBalanceCents:         loanBalanceCents,
		PropertyEquityCents:      propertyEquityCents,
		NetWorthCents:            netWorthCents,
		PortfolioPricesAvailable: pricesAvailable,
		History:                  history,
	})
}

// ── Tax Summary ───────────────────────────────────────────────────────────────

func (h *Handler) Tax(w http.ResponseWriter, r *http.Request) {
	auth := h.getAuth(r)
	ctx := r.Context()

	// year selector
	yearStr := r.URL.Query().Get("year")
	now := time.Now()
	year := now.Year()
	if yearStr != "" {
		if y, err := strconv.Atoi(yearStr); err == nil && y >= 2000 && y <= now.Year() {
			year = y
		}
	}

	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)

	// income transactions
	incomeTxns, err := h.store.getTransactions(ctx, auth.UserID, bson.M{
		"category": "Income",
		"date":     bson.M{"$gte": start, "$lt": end},
	})
	if err != nil {
		http.Error(w, "failed to load income", http.StatusInternalServerError)
		return
	}
	var grossIncome int64
	for _, t := range incomeTxns {
		grossIncome += t.AmountCents
	}

	// expense transactions by category (deductible candidates = all expenses)
	expTxns, err := h.store.getTransactions(ctx, auth.UserID, bson.M{
		"amount_cents": bson.M{"$lt": 0},
		"date":         bson.M{"$gte": start, "$lt": end},
	})
	if err != nil {
		http.Error(w, "failed to load expenses", http.StatusInternalServerError)
		return
	}
	deductMap := map[string]int64{}
	for _, t := range expTxns {
		deductMap[t.Category] += -t.AmountCents
	}
	var deductibles []TaxDeductible
	var totalDeduct int64
	// order by category name for stable output
	catNames := make([]string, 0, len(deductMap))
	for c := range deductMap {
		catNames = append(catNames, c)
	}
	sort.Strings(catNames)
	for _, cat := range catNames {
		amt := deductMap[cat]
		deductibles = append(deductibles, TaxDeductible{Category: cat, TotalCents: amt})
		totalDeduct += amt
	}

	// capital gains from trades in the selected year
	trades, err := h.store.getTrades(ctx, auth.UserID)
	if err != nil {
		http.Error(w, "failed to load trades", http.StatusInternalServerError)
		return
	}

	// FIFO matching: for each ISIN track buy queue, match sells
	type buyLot struct {
		qty        float64
		priceCents int64
	}
	buyQueues := map[string][]buyLot{}
	nameByISIN := map[string]string{}

	// process buys in date order first (already sorted by date from store, but sort to be safe)
	sort.Slice(trades, func(i, j int) bool { return trades[i].Date.Before(trades[j].Date) })

	var capEntries []CapitalGainEntry
	var capGains, capLosses int64

	for _, t := range trades {
		if t.Date.Before(start) {
			// still build the buy queue from prior years so we can match sells
			if t.Type == "buy" {
				buyQueues[t.ISIN] = append(buyQueues[t.ISIN], buyLot{t.Quantity, t.PriceCents})
				nameByISIN[t.ISIN] = t.Name
			} else if t.Type == "sell" {
				// consume from queue silently
				q := t.Quantity
				for q > 0 && len(buyQueues[t.ISIN]) > 0 {
					lot := &buyQueues[t.ISIN][0]
					if lot.qty <= q {
						q -= lot.qty
						buyQueues[t.ISIN] = buyQueues[t.ISIN][1:]
					} else {
						lot.qty -= q
						q = 0
					}
				}
			}
			continue
		}
		if t.Date.After(end) {
			continue
		}
		nameByISIN[t.ISIN] = t.Name
		if t.Type == "buy" {
			buyQueues[t.ISIN] = append(buyQueues[t.ISIN], buyLot{t.Quantity, t.PriceCents})
		} else if t.Type == "sell" {
			// FIFO match
			remaining := t.Quantity
			var costCents int64
			for remaining > 0 && len(buyQueues[t.ISIN]) > 0 {
				lot := &buyQueues[t.ISIN][0]
				matched := lot.qty
				if matched > remaining {
					matched = remaining
				}
				costCents += int64(matched * float64(lot.priceCents))
				lot.qty -= matched
				remaining -= matched
				if lot.qty == 0 {
					buyQueues[t.ISIN] = buyQueues[t.ISIN][1:]
				}
			}
			gainCents := t.TotalCents - costCents
			gainPct := 0.0
			if costCents > 0 {
				gainPct = float64(gainCents) / float64(costCents) * 100
			}
			entry := CapitalGainEntry{
				ISIN:      t.ISIN,
				Name:      nameByISIN[t.ISIN],
				BuyCents:  costCents,
				SellCents: t.TotalCents,
				GainCents: gainCents,
				GainPct:   math.Round(gainPct*100) / 100,
			}
			capEntries = append(capEntries, entry)
			if gainCents > 0 {
				capGains += gainCents
			} else {
				capLosses += -gainCents
			}
		}
	}

	// available years: from first transaction year to current year
	allTxns, _ := h.store.getTransactions(ctx, auth.UserID, bson.M{})
	availYears := []int{}
	minYear := now.Year()
	for _, t := range allTxns {
		if t.Date.Year() < minYear {
			minYear = t.Date.Year()
		}
	}
	for y := minYear; y <= now.Year(); y++ {
		availYears = append(availYears, y)
	}
	if len(availYears) == 0 {
		availYears = []int{now.Year()}
	}

	render(w, taxTmpl, &TaxData{
		UserID:             auth.UserID,
		Email:              auth.Email,
		Title:              "Tax Summary",
		Route:              "/tax",
		Year:               year,
		GrossIncomeCents:   grossIncome,
		CapitalGainsCents:  capGains,
		CapitalLossesCents: capLosses,
		NetCapitalCents:    capGains - capLosses,
		Deductibles:        deductibles,
		TotalDeductCents:   totalDeduct,
		CapitalEntries:     capEntries,
		AvailableYears:     availYears,
	})
}

func (h *Handler) TaxExport(w http.ResponseWriter, r *http.Request) {
	// Reuse Tax logic output as CSV — redirect with same year param
	auth := h.getAuth(r)
	ctx := r.Context()

	yearStr := r.URL.Query().Get("year")
	now := time.Now()
	year := now.Year()
	if yearStr != "" {
		if y, err := strconv.Atoi(yearStr); err == nil {
			year = y
		}
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)

	expTxns, _ := h.store.getTransactions(ctx, auth.UserID, bson.M{
		"amount_cents": bson.M{"$lt": 0},
		"date":         bson.M{"$gte": start, "$lt": end},
	})

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="tax_%d.csv"`, year))
	fmt.Fprintf(w, "Date,Description,Category,Amount\n")
	for _, t := range expTxns {
		fmt.Fprintf(w, "%s,%q,%s,%.2f\n",
			t.Date.Format("2006-01-02"),
			t.Description,
			t.Category,
			float64(-t.AmountCents)/100,
		)
	}
}

// ── Household ─────────────────────────────────────────────────────────────────

func (h *Handler) Household(w http.ResponseWriter, r *http.Request) {
	auth := h.getAuth(r)
	ctx := r.Context()
	now := time.Now()

	data := &HouseholdData{
		UserID: auth.UserID,
		Email:  auth.Email,
		Title:  "Household",
		Route:  "/household",
	}

	if r.Method == http.MethodPost {
		partnerEmail := strings.TrimSpace(r.FormValue("partner_email"))
		if partnerEmail == "" {
			http.Error(w, "partner email required", http.StatusBadRequest)
			return
		}
		// resolve partner user ID via permissions search (reuse SearchUsers pattern)
		// For now store by email as ID placeholder — real lookup needs identity service
		hh := &Household{
			ID:        bson.NewObjectID().Hex(),
			OwnerID:   auth.UserID,
			PartnerID: partnerEmail, // stored as email; resolved on read
			CreatedAt: now,
		}
		if err := h.store.createHousehold(ctx, hh); err != nil {
			http.Error(w, "failed to create household", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/household", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodDelete {
		_ = h.store.deleteHousehold(ctx, auth.UserID)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	hh, err := h.store.getHousehold(ctx, auth.UserID)
	if err == nil && hh != nil {
		data.HasHousehold = true
		data.IsOwner = hh.OwnerID == auth.UserID
		partnerID := hh.PartnerID
		if hh.OwnerID == auth.UserID {
			partnerID = hh.PartnerID
		} else {
			partnerID = hh.OwnerID
		}
		data.PartnerID = partnerID
		data.PartnerEmail = partnerID // email stored as ID for now

		// compute combined view for current month
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		nextMonth := monthStart.AddDate(0, 1, 0)

		myTxns, _ := h.store.getTransactions(ctx, auth.UserID, bson.M{
			"date": bson.M{"$gte": monthStart, "$lt": nextMonth},
		})
		partnerTxns, _ := h.store.getTransactions(ctx, partnerID, bson.M{
			"date": bson.M{"$gte": monthStart, "$lt": nextMonth},
		})

		for _, t := range myTxns {
			if t.AmountCents > 0 {
				data.MyIncomeCents += t.AmountCents
			} else {
				data.CombinedExpenseCents += -t.AmountCents
			}
		}
		for _, t := range partnerTxns {
			if t.AmountCents > 0 {
				data.PartnerIncomeCents += t.AmountCents
			} else {
				data.CombinedExpenseCents += -t.AmountCents
			}
		}
		data.CombinedIncomeCents = data.MyIncomeCents + data.PartnerIncomeCents
		data.CombinedDisposable = data.CombinedIncomeCents - data.CombinedExpenseCents

		myGoals, _ := h.store.getGoals(ctx, auth.UserID)
		partnerGoals, _ := h.store.getGoals(ctx, partnerID)
		for _, g := range myGoals {
			data.MyGoals = append(data.MyGoals, GoalPlan{Goal: g})
		}
		for _, g := range partnerGoals {
			data.PartnerGoals = append(data.PartnerGoals, GoalPlan{Goal: g})
		}
		data.SharedGoals = append(data.MyGoals, data.PartnerGoals...)
	}

	render(w, householdTmpl, data)
}

// ── Auto Import ───────────────────────────────────────────────────────────────

func (h *Handler) AutoImport(w http.ResponseWriter, r *http.Request) {
	auth := h.getAuth(r)
	accounts, _ := h.store.getAccounts(r.Context(), auth.UserID)
	render(w, autoImportTmpl, &AutoImportData{
		UserID:   auth.UserID,
		Email:    auth.Email,
		Title:    "Import Guide",
		Route:    "/auto-import",
		Accounts: accounts,
	})
}

// ── People (Sharing + Household merged) ───────────────────────────────────────

func (h *Handler) People(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "sharing"
	}

	// Handle mutations — redirect back preserving tab
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		switch r.FormValue("_action") {
		case "share":
			viewerID := r.FormValue("viewer_id")
			if viewerID != "" && viewerID != a.UserID {
				existing, _ := h.store.getPermissions(ctx, a.UserID)
				already := false
				for _, p := range existing {
					if p.ViewerID == viewerID {
						already = true
						break
					}
				}
				if !already {
					_ = h.store.createPermission(ctx, &Permission{
						ID:        bson.NewObjectID().Hex(),
						OwnerID:   a.UserID,
						ViewerID:  viewerID,
						CreatedAt: time.Now(),
					})
				}
			}
			http.Redirect(w, r, "/people?tab=sharing", http.StatusSeeOther)
			return
		case "household":
			partnerEmail := strings.TrimSpace(r.FormValue("partner_email"))
			if partnerEmail != "" {
				_ = h.store.createHousehold(ctx, &Household{
					ID:        bson.NewObjectID().Hex(),
					OwnerID:   a.UserID,
					PartnerID: partnerEmail,
					CreatedAt: time.Now(),
				})
			}
			http.Redirect(w, r, "/people?tab=household", http.StatusSeeOther)
			return
		}
	}

	if r.Method == http.MethodDelete {
		switch r.URL.Query().Get("kind") {
		case "share":
			_ = h.store.deletePermission(ctx, a.UserID, r.PathValue("id"))
		case "household":
			_ = h.store.deleteHousehold(ctx, a.UserID)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	data := &PeopleData{
		UserID: a.UserID,
		Email:  a.Email,
		Title:  "People",
		Route:  "people",
		Tab:    tab,
	}

	// Sharing data
	perms, _ := h.store.getPermissions(ctx, a.UserID)
	granted, _ := h.store.getGrantedViewers(ctx, a.UserID)
	viewerIDs := map[string]bool{}
	for _, p := range perms {
		viewerIDs[p.ViewerID] = true
	}
	for id := range viewerIDs {
		data.Viewers = append(data.Viewers, SharingUser{ID: id, Email: id})
	}
	data.Grants = perms
	data.Granted = granted

	// Household data
	now := time.Now()
	hh, err := h.store.getHousehold(ctx, a.UserID)
	if err == nil && hh != nil {
		data.HasHousehold = true
		data.IsOwner = hh.OwnerID == a.UserID
		partnerID := hh.PartnerID
		if hh.OwnerID != a.UserID {
			partnerID = hh.OwnerID
		}
		data.PartnerID = partnerID
		data.PartnerEmail = partnerID

		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		nextMonth := monthStart.AddDate(0, 1, 0)
		myTxns, _ := h.store.getTransactions(ctx, a.UserID, bson.M{"date": bson.M{"$gte": monthStart, "$lt": nextMonth}})
		partnerTxns, _ := h.store.getTransactions(ctx, partnerID, bson.M{"date": bson.M{"$gte": monthStart, "$lt": nextMonth}})
		for _, t := range myTxns {
			if t.AmountCents > 0 {
				data.MyIncomeCents += t.AmountCents
			} else {
				data.CombinedExpenseCents += -t.AmountCents
			}
		}
		for _, t := range partnerTxns {
			if t.AmountCents > 0 {
				data.PartnerIncomeCents += t.AmountCents
			} else {
				data.CombinedExpenseCents += -t.AmountCents
			}
		}
		data.CombinedIncomeCents = data.MyIncomeCents + data.PartnerIncomeCents
		data.CombinedDisposable = data.CombinedIncomeCents - data.CombinedExpenseCents
		myGoals, _ := h.store.getGoals(ctx, a.UserID)
		partnerGoals, _ := h.store.getGoals(ctx, partnerID)
		for _, g := range myGoals {
			data.MyGoals = append(data.MyGoals, GoalPlan{Goal: g})
		}
		for _, g := range partnerGoals {
			data.PartnerGoals = append(data.PartnerGoals, GoalPlan{Goal: g})
		}
	}

	render(w, peopleTmpl, data)
}

// ── Settings (Accounts + Categories merged) ────────────────────────────────────

func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := h.getAuth(r)
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "accounts"
	}

	accounts, _ := h.store.getAccounts(ctx, a.UserID)
	categories, _ := h.store.getCategories(ctx, a.UserID)

	render(w, settingsTmpl, &SettingsData{
		UserID:     a.UserID,
		Email:      a.Email,
		Title:      "Settings",
		Route:      "settings",
		Tab:        tab,
		Accounts:   accounts,
		Categories: categories,
	})
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Auth (no authMW — these are public by definition)
	mux.HandleFunc("GET /auth/login", h.AuthLogin)
	mux.HandleFunc("POST /auth/login", h.AuthLogin)
	mux.HandleFunc("GET /auth/register", h.AuthRegister)
	mux.HandleFunc("POST /auth/register", h.AuthRegister)
	mux.HandleFunc("POST /auth/logout", h.AuthLogout)
	mux.HandleFunc("GET /auth/oauth/google", h.AuthGoogleStart)
	mux.HandleFunc("GET /auth/oauth/google/callback", h.AuthGoogleCallback)

	mux.HandleFunc("GET /{$}", h.Homepage)
	mux.HandleFunc("GET /dashboard", h.authMW(h.Dashboard))
	mux.HandleFunc("GET /transactions", h.Transactions)
	mux.HandleFunc("GET /import", h.ImportPage)
	mux.HandleFunc("POST /import/preview", h.ImportPreview)
	mux.HandleFunc("POST /import/confirm", h.ImportConfirm)
	mux.HandleFunc("POST /import/securities", h.ImportSecurities)
	mux.HandleFunc("POST /portfolio/ticker", h.SaveTickerMapping)
	mux.HandleFunc("POST /accounts", h.Accounts)
	mux.HandleFunc("DELETE /accounts/{id}", h.Accounts)
	mux.HandleFunc("POST /categories", h.Categories)
	mux.HandleFunc("PUT /categories/{id}", h.Categories)
	mux.HandleFunc("DELETE /categories/{id}", h.Categories)
	mux.HandleFunc("GET /reports", h.Reports)
	mux.HandleFunc("GET /projections", h.Projections)
	mux.HandleFunc("GET /portfolio", h.Portfolio)
	mux.HandleFunc("GET /goals", h.Goals)
	mux.HandleFunc("POST /goals", h.Goals)
	mux.HandleFunc("GET /networth", h.NetWorth)
	mux.HandleFunc("GET /simulator", h.Simulator)
	// legacy redirects so old bookmarks / links keep working
	mux.HandleFunc("GET /sharing", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/people?tab=sharing", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /household", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/people?tab=household", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /accounts", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/settings?tab=accounts", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /categories", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/settings?tab=categories", http.StatusMovedPermanently)
	})
	// people page
	mux.HandleFunc("GET /people", h.People)
	mux.HandleFunc("POST /people", h.People)
	mux.HandleFunc("DELETE /people/{id}", h.People)
	// settings page
	mux.HandleFunc("GET /settings", h.Settings)
	mux.HandleFunc("GET /api/users/search", h.SearchUsers)
	mux.HandleFunc("POST /api/transactions", h.CreateTransaction)
	mux.HandleFunc("PUT /api/transactions/{id}", h.UpdateTransaction)
	mux.HandleFunc("DELETE /api/transactions/{id}", h.DeleteTransaction)
	mux.HandleFunc("GET /tax", h.Tax)
	mux.HandleFunc("GET /tax/export.csv", h.TaxExport)
	mux.HandleFunc("GET /auto-import", h.AutoImport)
	mux.HandleFunc("GET /property", h.Properties)
	mux.HandleFunc("POST /property", h.Properties)
	mux.HandleFunc("GET /plan", h.Dream)

	h.RegisterOrgRoutes(mux)
}

func sortStrings(s []string) {
	sort.Strings(s)
}

func appendIfMissing(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

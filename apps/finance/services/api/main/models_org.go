package main

import "time"

// ── Org RBAC ─────────────────────────────────────────────────────────────────

type OrgRole string

const (
	OrgRoleAdmin   OrgRole = "admin"
	OrgRoleFinance OrgRole = "finance"
	OrgRoleMember  OrgRole = "member"
	OrgRoleViewer  OrgRole = "viewer"
)

type TeamType string

const (
	TeamTypeInternal TeamType = "internal"
	TeamTypeGuest    TeamType = "guest"
)

// ── Core entities ─────────────────────────────────────────────────────────────

type Org struct {
	ID          string    `bson:"_id"          json:"id"`
	Name        string    `bson:"name"         json:"name"`
	Slug        string    `bson:"slug"         json:"slug"`
	OwnerUserID string    `bson:"owner_user_id" json:"owner_user_id"`
	CreatedAt   time.Time `bson:"created_at"   json:"created_at"`
}

type OrgTeam struct {
	ID        string    `bson:"_id"        json:"id"`
	OrgID     string    `bson:"org_id"     json:"org_id"`
	Name      string    `bson:"name"       json:"name"`
	Type      TeamType  `bson:"type"       json:"type"`
	Avatar    string    `bson:"avatar"     json:"avatar"` // single emoji
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

// OrgMember links a user to an org with a role and optional team subset.
// All financial approvals go through org-level finance/admin regardless of team.
// Guest team members are scoped to their own team's data only (visibility scope,
// not approval scope).
type OrgMember struct {
	ID        string    `bson:"_id"        json:"id"`
	OrgID     string    `bson:"org_id"     json:"org_id"`
	UserID    string    `bson:"user_id"    json:"user_id"`
	Email     string    `bson:"email"      json:"email"`
	Role      OrgRole   `bson:"role"       json:"role"`
	TeamIDs   []string  `bson:"team_ids"   json:"team_ids"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

// OrgInvite is a pending invitation. Token is a random hex string.
// Email delivery is a TODO (Phase 5) — for now the link is displayed to the inviter.
type OrgInvite struct {
	ID        string    `bson:"_id"        json:"id"`
	OrgID     string    `bson:"org_id"     json:"org_id"`
	Email     string    `bson:"email"      json:"email"`
	Role      OrgRole   `bson:"role"       json:"role"`
	TeamIDs   []string  `bson:"team_ids"   json:"team_ids"`
	Token     string    `bson:"token"      json:"token"`
	ExpiresAt time.Time `bson:"expires_at" json:"expires_at"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UsedAt    time.Time `bson:"used_at,omitempty" json:"used_at,omitempty"`
}

// ── Fiscal year ───────────────────────────────────────────────────────────────

type FiscalYearStatus string

const (
	FiscalYearDraft  FiscalYearStatus = "draft"
	FiscalYearActive FiscalYearStatus = "active"
	FiscalYearClosed FiscalYearStatus = "closed"
)

type FiscalYear struct {
	ID        string           `bson:"_id"        json:"id"`
	OrgID     string           `bson:"org_id"     json:"org_id"`
	Label     string           `bson:"label"      json:"label"` // e.g. "2025"
	Status    FiscalYearStatus `bson:"status"     json:"status"`
	StartDate time.Time        `bson:"start_date" json:"start_date"`
	EndDate   time.Time        `bson:"end_date"   json:"end_date"`
	CreatedAt time.Time        `bson:"created_at" json:"created_at"`
	StartedAt time.Time        `bson:"started_at,omitempty" json:"started_at,omitempty"`
	ClosedAt  time.Time        `bson:"closed_at,omitempty"  json:"closed_at,omitempty"`
}

// ── Planning: Events & Budget ─────────────────────────────────────────────────

type EventStatus string

const (
	EventDraft    EventStatus = "draft"
	EventReview   EventStatus = "review"
	EventApproved EventStatus = "approved"
	EventRejected EventStatus = "rejected"
)

// EventGoal is a single checkable item in an event's goal list.
// Done can be toggled by any org member while the fiscal year is active.
type EventGoal struct {
	ID      string    `bson:"id"                 json:"id"`
	Text    string    `bson:"text"               json:"text"`
	Done    bool      `bson:"done"               json:"done"`
	DoneBy  string    `bson:"done_by,omitempty"  json:"done_by,omitempty"`
	DoneAt  time.Time `bson:"done_at,omitempty"  json:"done_at,omitempty"`
}

type OrgEvent struct {
	ID           string      `bson:"_id"           json:"id"`
	OrgID        string      `bson:"org_id"        json:"org_id"`
	FiscalYearID string      `bson:"fiscal_year_id" json:"fiscal_year_id"`
	TeamIDs      []string    `bson:"team_ids"      json:"team_ids"`
	Name         string      `bson:"name"          json:"name"`
	Description  string      `bson:"description"   json:"description"`
	Goals        string      `bson:"goals"         json:"goals"`
	GoalItems    []EventGoal `bson:"goal_items"    json:"goal_items"`
	DateStart    time.Time   `bson:"date_start"    json:"date_start"`
	DateEnd      time.Time   `bson:"date_end"      json:"date_end"`
	Status       EventStatus `bson:"status"        json:"status"`
	CreatedBy    string      `bson:"created_by"    json:"created_by"`
	CreatedAt    time.Time   `bson:"created_at"    json:"created_at"`
}

type BudgetLineType string

const (
	BudgetIncome  BudgetLineType = "income"
	BudgetExpense BudgetLineType = "expense"
)

type BudgetLine struct {
	ID           string         `bson:"_id"           json:"id"`
	EventID      string         `bson:"event_id"      json:"event_id"`
	OrgID        string         `bson:"org_id"        json:"org_id"`
	Category     string         `bson:"category"      json:"category"`
	Type         BudgetLineType `bson:"type"          json:"type"`
	PlannedCents int64          `bson:"planned_cents" json:"planned_cents"`
	Description  string         `bson:"description"   json:"description"`
	CreatedAt    time.Time      `bson:"created_at"    json:"created_at"`
}

// EventComment is used for two purposes distinguished by Kind:
//   - "review"   — admin → team during planning (request changes / feedback)
//   - "feedback" — team post-mortem after year closes (included in year-end report)
type EventCommentKind string

const (
	CommentReview   EventCommentKind = "review"
	CommentFeedback EventCommentKind = "feedback"
)

type EventComment struct {
	ID        string           `bson:"_id"        json:"id"`
	EventID   string           `bson:"event_id"   json:"event_id"`
	OrgID     string           `bson:"org_id"     json:"org_id"`
	UserID    string           `bson:"user_id"    json:"user_id"`
	UserEmail string           `bson:"user_email" json:"user_email"`
	Kind      EventCommentKind `bson:"kind"       json:"kind"`
	Body      string           `bson:"body"       json:"body"`
	CreatedAt time.Time        `bson:"created_at" json:"created_at"`
}

// ── Execution: Transaction Requests ──────────────────────────────────────────

type TxRequestType string

const (
	TxReimbursement  TxRequestType = "reimbursement"
	TxPurchaseOrder  TxRequestType = "purchase_order"
	TxCashAdvance    TxRequestType = "cash_advance"
	TxIncome         TxRequestType = "income"
	TxBudgetTransfer TxRequestType = "budget_transfer"
)

type TxRequestStatus string

const (
	TxDraft              TxRequestStatus = "draft"
	TxSubmitted          TxRequestStatus = "submitted"
	TxInfoRequested      TxRequestStatus = "info_requested"
	TxUnderReview        TxRequestStatus = "under_review"
	TxApproved           TxRequestStatus = "approved"
	TxRejected           TxRequestStatus = "rejected"
	TxCancelled          TxRequestStatus = "cancelled"
	// Reimbursement
	TxPaid               TxRequestStatus = "paid"
	// Purchase Order
	TxOrdered            TxRequestStatus = "ordered"
	TxDelivered          TxRequestStatus = "delivered"
	TxDisputed           TxRequestStatus = "disputed"
	// Cash Advance
	TxDisbursed          TxRequestStatus = "disbursed"
	TxSettlementDue      TxRequestStatus = "settlement_due"
	TxSettled            TxRequestStatus = "settled"
	TxPartialSettlement  TxRequestStatus = "partial_settlement"
	// Income
	TxPendingPayment     TxRequestStatus = "pending_payment"
	TxReceived           TxRequestStatus = "received"
	// Shared terminal
	TxReconciled         TxRequestStatus = "reconciled"
	// Budget transfer terminal
	TxDone               TxRequestStatus = "done"
)

// StatusLogEntry is appended on every status change. Never mutated.
// When status = info_requested, Comment is required.
type StatusLogEntry struct {
	Status    TxRequestStatus `bson:"status"             json:"status"`
	ChangedBy string          `bson:"changed_by"         json:"changed_by"`
	ChangedAt time.Time       `bson:"changed_at"         json:"changed_at"`
	Comment   string          `bson:"comment,omitempty"  json:"comment,omitempty"`
}

// PODelivery is filled in by the requester when a Purchase Order arrives.
type PODelivery struct {
	ActualAmountCents   int64     `bson:"actual_amount_cents"  json:"actual_amount_cents"`
	ActualVendor        string    `bson:"actual_vendor"        json:"actual_vendor"`
	DeliveredAt         time.Time `bson:"delivered_at"         json:"delivered_at"`
	InvoiceAttachmentID []string  `bson:"invoice_attachment_ids" json:"invoice_attachment_ids"`
	StoreChanged        bool      `bson:"store_changed"        json:"store_changed"`
	ChangeNote          string    `bson:"change_note,omitempty" json:"change_note,omitempty"`
}

// CashSettlement is filled in by the requester when settling a Cash Advance.
type CashSettlement struct {
	AmountSpentCents    int64    `bson:"amount_spent_cents"    json:"amount_spent_cents"`
	AmountReturnedCents int64    `bson:"amount_returned_cents" json:"amount_returned_cents"`
	ReceiptAttachmentIDs []string `bson:"receipt_attachment_ids" json:"receipt_attachment_ids"`
	SettledAt           time.Time `bson:"settled_at"           json:"settled_at"`
}

type TxRequest struct {
	ID             string          `bson:"_id"              json:"id"`
	OrgID          string          `bson:"org_id"           json:"org_id"`
	FiscalYearID   string          `bson:"fiscal_year_id"   json:"fiscal_year_id"`
	EventID        string          `bson:"event_id"         json:"event_id"`
	BudgetLineID   string          `bson:"budget_line_id"   json:"budget_line_id"`
	TeamID         string          `bson:"team_id"          json:"team_id"`
	SubmittedBy    string          `bson:"submitted_by"     json:"submitted_by"`
	SubmitterEmail string          `bson:"submitter_email"  json:"submitter_email"`
	Type           TxRequestType   `bson:"type"             json:"type"`
	Description    string          `bson:"description"      json:"description"`
	AmountCents    int64           `bson:"amount_cents"     json:"amount_cents"`
	Vendor         string          `bson:"vendor,omitempty" json:"vendor,omitempty"`
	PayerName      string          `bson:"payer_name,omitempty"  json:"payer_name,omitempty"`
	DueDate        time.Time       `bson:"due_date,omitempty"    json:"due_date,omitempty"`
	PaymentMethod  string          `bson:"payment_method,omitempty" json:"payment_method,omitempty"`
	// PO-specific
	Delivery       *PODelivery     `bson:"delivery,omitempty"   json:"delivery,omitempty"`
	// Cash advance specific
	Settlement     *CashSettlement `bson:"settlement,omitempty" json:"settlement,omitempty"`
	// Budget transfer specific
	FromBudgetLineID string        `bson:"from_budget_line_id,omitempty" json:"from_budget_line_id,omitempty"`
	ToBudgetLineID   string        `bson:"to_budget_line_id,omitempty"   json:"to_budget_line_id,omitempty"`

	AttachmentIDs  []string        `bson:"attachment_ids"   json:"attachment_ids"`
	StatusLog      []StatusLogEntry `bson:"status_log"      json:"status_log"`
	CreatedAt      time.Time       `bson:"created_at"       json:"created_at"`
}

// CurrentStatus returns the latest status from the log.
func (r *TxRequest) CurrentStatus() TxRequestStatus {
	if len(r.StatusLog) == 0 {
		return TxDraft
	}
	return r.StatusLog[len(r.StatusLog)-1].Status
}

// OrgAttachment stores file metadata; the file lives on disk.
type OrgAttachment struct {
	ID          string    `bson:"_id"          json:"id"`
	OrgID       string    `bson:"org_id"       json:"org_id"`
	RequestID   string    `bson:"request_id"   json:"request_id"`
	UploadedBy  string    `bson:"uploaded_by"  json:"uploaded_by"`
	UploadedAt  time.Time `bson:"uploaded_at"  json:"uploaded_at"`
	Filename    string    `bson:"filename"     json:"filename"`
	MimeType    string    `bson:"mime_type"    json:"mime_type"`
	SizeBytes   int64     `bson:"size_bytes"   json:"size_bytes"`
	// Path on disk: /data/org-files/{org_id}/{request_id}/{id}
	StoragePath string    `bson:"storage_path" json:"storage_path"`
}

// OrgLedgerEntry is created from an approved TxRequest.
// bank_ref is set when the entry is matched to a bank CSV row (reconciliation).
type OrgLedgerEntry struct {
	ID           string    `bson:"_id"              json:"id"`
	OrgID        string    `bson:"org_id"           json:"org_id"`
	FiscalYearID string    `bson:"fiscal_year_id"   json:"fiscal_year_id"`
	EventID      string    `bson:"event_id"         json:"event_id"`
	BudgetLineID string    `bson:"budget_line_id"   json:"budget_line_id"`
	TeamID       string    `bson:"team_id"          json:"team_id"`
	RequestID    string    `bson:"request_id,omitempty" json:"request_id,omitempty"`
	AmountCents  int64     `bson:"amount_cents"     json:"amount_cents"`
	Description  string    `bson:"description"      json:"description"`
	Date         time.Time `bson:"date"             json:"date"`
	BankRef      string    `bson:"bank_ref,omitempty" json:"bank_ref,omitempty"`
	Reconciled   bool      `bson:"reconciled"       json:"reconciled"`
	CreatedAt    time.Time `bson:"created_at"       json:"created_at"`
}

// ── Page data structs ─────────────────────────────────────────────────────────

type OrgListData struct {
	UserID string
	Email  string
	Title  string
	Route  string
	Orgs   []OrgWithRole
}

type OrgWithRole struct {
	Org  Org
	Role OrgRole
}

type OrgHomeData struct {
	UserID     string
	Email      string
	Title      string
	Route      string
	Org        Org
	MyRole     OrgRole
	MyTeamIDs  []string
	FiscalYears []FiscalYear
	ActiveYear *FiscalYear
	Teams      []OrgTeam
	Members    []OrgMember
}

type OrgTeamsData struct {
	UserID  string
	Email   string
	Title   string
	Route   string
	Org     Org
	MyRole  OrgRole
	Teams   []OrgTeam
	Members []OrgMember // for showing team membership counts
}

type OrgMembersData struct {
	UserID  string
	Email   string
	Title   string
	Route   string
	Org     Org
	MyRole  OrgRole
	Members []OrgMember
	Teams   []OrgTeam
	Invites []OrgInvite
}

type OrgInviteData struct {
	UserID  string
	Email   string
	Title   string
	Route   string
	Org     Org
	MyRole  OrgRole
	Teams   []OrgTeam
	Error   string
	Link    string // generated invite link shown after creation
}

type OrgEventsData struct {
	UserID     string
	Email      string
	Title      string
	Route      string
	Org        Org
	MyRole     OrgRole
	FiscalYear FiscalYear
	Events     []OrgEventSummary
	Teams      []OrgTeam
}

type OrgEventSummary struct {
	Event        OrgEvent
	TotalIncome  int64
	TotalExpense int64
	Teams        []OrgTeam
}

type OrgEventDetailData struct {
	UserID       string
	Email        string
	Title        string
	Route        string
	Org          Org
	MyRole       OrgRole
	FiscalYear   FiscalYear
	Event        OrgEvent
	BudgetLines  []BudgetLine
	Comments     []EventComment
	Teams        []OrgTeam
	EventTeams   []OrgTeam
	TotalIncome  int64
	TotalExpense int64
	Error        string
}

// ── Phase 3 page data ────────────────────────────────────────────────────────

type OrgRequestsData struct {
	UserID     string
	Email      string
	Title      string
	Route      string
	Org        Org
	MyRole     OrgRole
	Requests   []TxRequest
	Events     []OrgEvent
	Teams      []OrgTeam
	StatusFilter string
}

type OrgRequestDetailData struct {
	UserID      string
	Email       string
	Title       string
	Route       string
	Org         Org
	MyRole      OrgRole
	Request     TxRequest
	Event       *OrgEvent
	BudgetLine  *BudgetLine
	Team        *OrgTeam
	FiscalYear  *FiscalYear
	Attachments []OrgAttachment
	Error       string
	// populated on new-request GET
	NewEvents []OrgEvent
	NewTeams  []OrgTeam
}

type OrgLedgerData struct {
	UserID      string
	Email       string
	Title       string
	Route       string
	Org         Org
	MyRole      OrgRole
	FiscalYear  *FiscalYear
	FiscalYears []FiscalYear
	Entries     []OrgLedgerEntry
	Events      map[string]OrgEvent
	Teams       map[string]OrgTeam
	TotalIncome int64
	TotalExpense int64
}

type OrgBankImportData struct {
	UserID     string
	Email      string
	Title      string
	Route      string
	Org        Org
	MyRole     OrgRole
	FiscalYear *FiscalYear
	Rows       []BankImportRow
	Error      string
	Imported   int
}

type BankImportRow struct {
	Date        string
	Description string
	AmountCents int64
	Reference   string
	Matched     bool
	MatchedID   string
}

// ── Phase 4 page data ────────────────────────────────────────────────────────

type OrgAnalysisData struct {
	UserID      string
	Email       string
	Title       string
	Route       string
	Org         Org
	MyRole      OrgRole
	FiscalYear  FiscalYear
	FiscalYears []FiscalYear
	EventRows   []AnalysisEventRow
	TeamRows    []AnalysisTeamRow
	TotalPlannedIncome   int64
	TotalActualIncome    int64
	TotalPlannedExpense  int64
	TotalActualExpense   int64
}

type AnalysisEventRow struct {
	Event          OrgEvent
	PlannedIncome  int64
	ActualIncome   int64
	PlannedExpense int64
	ActualExpense  int64
}

type AnalysisTeamRow struct {
	Team           OrgTeam
	PlannedIncome  int64
	ActualIncome   int64
	PlannedExpense int64
	ActualExpense  int64
}

// ── Phase 5 page data ────────────────────────────────────────────────────────

type OrgReportData struct {
	UserID      string
	Email       string
	Title       string
	Route       string
	Org         Org
	MyRole      OrgRole
	FiscalYear  FiscalYear
	FiscalYears []FiscalYear
	EventReports []EventReport
	TotalPlannedIncome   int64
	TotalActualIncome    int64
	TotalPlannedExpense  int64
	TotalActualExpense   int64
}

type EventReport struct {
	Event          OrgEvent
	BudgetLines    []BudgetLine
	Comments       []EventComment // kind=feedback only
	PlannedIncome  int64
	ActualIncome   int64
	PlannedExpense int64
	ActualExpense  int64
	Teams          []OrgTeam
}

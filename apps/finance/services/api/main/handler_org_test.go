package main

// Org, Property, Dream, and Auth-helper tests.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testOrg() (*Org, *OrgMember, *FiscalYear) {
	org := &Org{ID: "org1", Name: "ACME Corp", Slug: "acme", OwnerUserID: "user1", CreatedAt: time.Now()}
	member := &OrgMember{ID: "m1", OrgID: "org1", UserID: "user1", Email: "test@example.com", Role: OrgRoleAdmin, CreatedAt: time.Now()}
	fy := &FiscalYear{ID: "fy1", OrgID: "org1", Label: "2025", Status: FiscalYearActive,
		StartDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), CreatedAt: time.Now()}
	return org, member, fy
}

func newOrgStore() *mockStore {
	org, member, fy := testOrg()
	return &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{*fy},
	}
}

func orgReq(method, path, slug string, form url.Values) *http.Request {
	r := authReq(method, path, form)
	r.SetPathValue("slug", slug)
	return r
}

// ── OrgList ──────────────────────────────────────────────────────────────────

func TestOrgList_Empty(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.OrgList(w, authReq("GET", "/orgs", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgList_WithOrg(t *testing.T) {
	h := newHandler(newOrgStore())
	w := httptest.NewRecorder()
	h.OrgList(w, authReq("GET", "/orgs", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgCreate ────────────────────────────────────────────────────────────────

func TestOrgCreate_GET(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.OrgCreate(w, authReq("GET", "/orgs/new", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgCreate_POST_Success(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"name": {"Test Corp"}, "slug": {"test-corp"}}
	w := httptest.NewRecorder()
	h.OrgCreate(w, authReq("POST", "/orgs/new", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgCreate_POST_EmptyName(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"name": {""}, "slug": {"test"}}
	w := httptest.NewRecorder()
	h.OrgCreate(w, authReq("POST", "/orgs/new", form))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgCreate_POST_BadSlug(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"name": {"Test Corp"}, "slug": {"TEST CORP!"}}
	w := httptest.NewRecorder()
	h.OrgCreate(w, authReq("POST", "/orgs/new", form))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgHome ──────────────────────────────────────────────────────────────────

func TestOrgHome_NotFound(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.OrgHome(w, orgReq("GET", "/orgs/acme", "missing-slug", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrgHome_WithOrg(t *testing.T) {
	h := newHandler(newOrgStore())
	w := httptest.NewRecorder()
	h.OrgHome(w, orgReq("GET", "/orgs/acme", "acme", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ACME Corp") {
		t.Error("expected org name in response")
	}
}

// ── OrgTeams ─────────────────────────────────────────────────────────────────

func TestOrgTeams_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	w := httptest.NewRecorder()
	h.OrgTeams(w, orgReq("GET", "/orgs/acme/teams", "acme", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgTeamCreate(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"name": {"Engineering"}, "type": {"internal"}}
	w := httptest.NewRecorder()
	h.OrgTeamCreate(w, orgReq("POST", "/orgs/acme/teams", "acme", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgTeamCreate_MissingName(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"name": {""}}
	w := httptest.NewRecorder()
	h.OrgTeamCreate(w, orgReq("POST", "/orgs/acme/teams", "acme", form))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestOrgTeamDelete(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("DELETE", "/orgs/acme/teams/t1", "acme", nil)
	r.SetPathValue("team_id", "t1")
	w := httptest.NewRecorder()
	h.OrgTeamDelete(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgMembers ───────────────────────────────────────────────────────────────

func TestOrgMembers_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	w := httptest.NewRecorder()
	h.OrgMembers(w, orgReq("GET", "/orgs/acme/members", "acme", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgMemberRoleUpdate(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"role": {"member"}}
	r := orgReq("POST", "/orgs/acme/members/m2/role", "acme", form)
	r.SetPathValue("member_id", "m2")
	w := httptest.NewRecorder()
	h.OrgMemberRoleUpdate(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgMemberRoleUpdate_InvalidRole(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"role": {"superuser"}}
	r := orgReq("POST", "/orgs/acme/members/m2/role", "acme", form)
	r.SetPathValue("member_id", "m2")
	w := httptest.NewRecorder()
	h.OrgMemberRoleUpdate(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestOrgMemberRemove(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("DELETE", "/orgs/acme/members/m2", "acme", nil)
	r.SetPathValue("member_id", "m2")
	w := httptest.NewRecorder()
	h.OrgMemberRemove(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgMemberRemove_Self(t *testing.T) {
	h := newHandler(newOrgStore())
	// Self-remove check: memberID must match me.ID (which is "m1"), not the user ID
	r := orgReq("DELETE", "/orgs/acme/members/m1", "acme", nil)
	r.SetPathValue("member_id", "m1")
	w := httptest.NewRecorder()
	h.OrgMemberRemove(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── OrgInviteNew ─────────────────────────────────────────────────────────────

func TestOrgInviteNew_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	w := httptest.NewRecorder()
	h.OrgInviteNew(w, orgReq("GET", "/orgs/acme/invite", "acme", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgInviteNew_POST(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"email": {"new@example.com"}, "role": {"member"}}
	w := httptest.NewRecorder()
	h.OrgInviteNew(w, orgReq("POST", "/orgs/acme/invite", "acme", form))
	if w.Code != http.StatusOK && w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 200 or 303", w.Code)
	}
}

func TestOrgInviteNew_POST_MissingEmail(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"email": {""}, "role": {"member"}}
	w := httptest.NewRecorder()
	h.OrgInviteNew(w, orgReq("POST", "/orgs/acme/invite", "acme", form))
	// Handler re-renders the form with 200 (template may fail to render fully)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgInviteRevoke(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("DELETE", "/orgs/acme/invites/inv1", "acme", nil)
	r.SetPathValue("invite_id", "inv1")
	w := httptest.NewRecorder()
	h.OrgInviteRevoke(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgInviteRevoke_Forbidden(t *testing.T) {
	org, _, fy := testOrg()
	viewer := &OrgMember{ID: "m2", OrgID: "org1", UserID: "user1", Role: OrgRoleViewer}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": viewer},
		fiscalYears:  []FiscalYear{*fy},
	}
	h := newHandler(store)
	r := orgReq("DELETE", "/orgs/acme/invites/inv1", "acme", nil)
	r.SetPathValue("invite_id", "inv1")
	w := httptest.NewRecorder()
	h.OrgInviteRevoke(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// ── OrgJoin ──────────────────────────────────────────────────────────────────

func TestOrgJoin_InvalidToken(t *testing.T) {
	h := newHandler(&mockStore{})
	r := authReq("GET", "/orgs/join/bad-token", nil)
	r.SetPathValue("token", "bad-token")
	w := httptest.NewRecorder()
	h.OrgJoin(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrgJoin_ValidToken_GET(t *testing.T) {
	org, _, _ := testOrg()
	store := &mockStore{
		orgsByID: map[string]*Org{"org1": org},
		invitesByToken: map[string]*OrgInvite{
			"token123": {ID: "inv1", OrgID: "org1", Email: "new@example.com",
				Role: OrgRoleMember, Token: "token123", ExpiresAt: time.Now().Add(24 * time.Hour)},
		},
	}
	h := newHandler(store)
	r := authReq("GET", "/orgs/join/token123", nil)
	r.SetPathValue("token", "token123")
	w := httptest.NewRecorder()
	h.OrgJoin(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgJoin_POST_CreatesMember(t *testing.T) {
	org, _, _ := testOrg()
	store := &mockStore{
		orgsByID: map[string]*Org{"org1": org},
		invitesByToken: map[string]*OrgInvite{
			"join-token": {ID: "inv1", OrgID: "org1", Email: "newuser@example.com",
				Role: OrgRoleMember, Token: "join-token", ExpiresAt: time.Now().Add(24 * time.Hour)},
		},
	}
	h := newHandler(store)
	r := authReq("POST", "/orgs/join/join-token", nil)
	r.SetPathValue("token", "join-token")
	w := httptest.NewRecorder()
	h.OrgJoin(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgFiscalYears ───────────────────────────────────────────────────────────

func TestOrgFiscalYearCreate(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"label": {"2026"}, "start_date": {"2026-01-01"}, "end_date": {"2026-12-31"}}
	w := httptest.NewRecorder()
	h.OrgFiscalYearCreate(w, orgReq("POST", "/orgs/acme/years", "acme", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgFiscalYearCreate_MissingLabel(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"label": {""}, "start_date": {"2026-01-01"}, "end_date": {"2026-12-31"}}
	w := httptest.NewRecorder()
	h.OrgFiscalYearCreate(w, orgReq("POST", "/orgs/acme/years", "acme", form))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestOrgFiscalYearCreate_MissingDates(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"label": {"2026"}, "start_date": {""}, "end_date": {""}}
	w := httptest.NewRecorder()
	h.OrgFiscalYearCreate(w, orgReq("POST", "/orgs/acme/years", "acme", form))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestOrgFiscalYearActivate_ConflictWhenActiveExists(t *testing.T) {
	// newOrgStore already has an active FY — activating another should return 409
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/years/fy1/activate", "acme", url.Values{"year_id": {"fy1"}})
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgFiscalYearActivate(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (already active)", w.Code)
	}
}

func TestOrgFiscalYearActivate_NoDraftYears(t *testing.T) {
	org, member, _ := testOrg()
	// Store with a draft fiscal year (no active year)
	draft := FiscalYear{ID: "fy2", OrgID: "org1", Label: "2026", Status: FiscalYearDraft,
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{draft},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy2/activate", "acme", nil)
	r.SetPathValue("year_id", "fy2")
	w := httptest.NewRecorder()
	h.OrgFiscalYearActivate(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgFiscalYearClose(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/years/fy1/close", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgFiscalYearClose(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgFiscalYearClose_Succeeds(t *testing.T) {
	// OrgFiscalYearClose takes year_id from path and just closes it — no active check
	org, _, _ := testOrg()
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": {ID: "m1", OrgID: "org1", UserID: "user1", Role: OrgRoleAdmin}},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/any-id/close", "acme", nil)
	r.SetPathValue("year_id", "any-id")
	w := httptest.NewRecorder()
	h.OrgFiscalYearClose(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgEventList / New ────────────────────────────────────────────────────────

func TestOrgEventList_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/events", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventList(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgEventNew_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/events/new", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventNew(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgEventNew_POST(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{
		"name": {"Annual Budget"}, "description": {"Review"},
		"date_start": {"2025-06-01"}, "date_end": {"2025-06-30"}, "year_id": {"fy1"},
	}
	r := orgReq("POST", "/orgs/acme/events/new", "acme", form)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventNew(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestOrgEventNew_POST_MissingName(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"name": {""}, "date_start": {"2025-06-01"}, "date_end": {"2025-06-30"}, "year_id": {"fy1"}}
	r := orgReq("POST", "/orgs/acme/events/new", "acme", form)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventNew(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── OrgEventDetail ───────────────────────────────────────────────────────────

func TestOrgEventDetail_NotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/events/evt99", "acme", nil)
	r.SetPathValue("event_id", "evt99")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventDetail(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrgEventDetail_Found(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Budget Review", Status: EventDraft, CreatedAt: time.Now()},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/events/evt1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventDetail(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// ── OrgEventEdit / Delete / Submit / Review / Feedback ───────────────────────

func TestOrgEventEdit_POST(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Budget Review", Status: EventDraft}}
	h := newHandler(store)
	form := url.Values{"name": {"Updated Budget"}, "description": {"Review"}, "date_start": {"2025-06-01"}, "date_end": {"2025-06-30"}}
	r := orgReq("POST", "/orgs/acme/events/evt1/edit", "acme", form)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventEdit(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestOrgEventEdit_NotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/events/bad/edit", "acme", nil)
	r.SetPathValue("event_id", "bad")
	w := httptest.NewRecorder()
	h.OrgEventEdit(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrgEventDelete(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Status: EventDraft}}
	h := newHandler(store)
	r := orgReq("DELETE", "/orgs/acme/events/evt1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgEventDelete(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgEventSubmit(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventDraft, FiscalYearID: "fy1"}}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/events/evt1/submit", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgEventSubmit(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgEventSubmit_NotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/events/noexist/submit", "acme", nil)
	r.SetPathValue("event_id", "noexist")
	w := httptest.NewRecorder()
	h.OrgEventSubmit(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrgEventReview_Approve(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventReview, FiscalYearID: "fy1"}}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/events/evt1/review", "acme", url.Values{"action": {"approve"}})
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgEventReview(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgEventReview_Reject(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventReview, FiscalYearID: "fy1"}}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/events/evt1/review", "acme", url.Values{"action": {"reject"}, "comment": {"Too expensive"}})
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgEventReview(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgEventReview_CommentOnly(t *testing.T) {
	// An unknown action (not "approve"/"reject") posts a comment and stays in review → 303
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventReview, FiscalYearID: "fy1"}}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/events/evt1/review", "acme", url.Values{"action": {"comment"}, "comment": {"looks good"}})
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventReview(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgEventFeedback(t *testing.T) {
	// Feedback requires a closed fiscal year
	org, member, _ := testOrg()
	closedFY := FiscalYear{ID: "fy2", OrgID: "org1", Label: "2024", Status: FiscalYearClosed,
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{closedFY},
		orgEvents:    []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventApproved, FiscalYearID: "fy2"}},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/events/evt1/feedback", "acme", url.Values{"comment": {"Looks good"}})
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy2")
	w := httptest.NewRecorder()
	h.OrgEventFeedback(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestOrgEventFeedback_NotClosedYear(t *testing.T) {
	// Feedback on active year returns 409
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/events/evt1/feedback", "acme", url.Values{"comment": {"ok"}})
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventFeedback(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (year not closed)", w.Code)
	}
}

// ── OrgGoal CRUD ─────────────────────────────────────────────────────────────

func TestOrgGoalAdd(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventDraft, FiscalYearID: "fy1"}}
	h := newHandler(store)
	// Handler uses "text" field, not "title"
	form := url.Values{"text": {"Buy Equipment"}}
	r := orgReq("POST", "/orgs/acme/events/evt1/goals", "acme", form)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgGoalAdd(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestOrgGoalAdd_EmptyText(t *testing.T) {
	// Handler checks text first (400), before looking up the event
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/events/evt1/goals", "acme", url.Values{"text": {""}})
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgGoalAdd(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestOrgGoalToggle(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventApproved, FiscalYearID: "fy1"}}
	h := newHandler(store)
	// done="1" to mark as done; handler also needs year_id (must be active)
	r := orgReq("POST", "/orgs/acme/events/evt1/goals/g1/toggle", "acme", url.Values{"done": {"1"}})
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("goal_id", "g1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgGoalToggle(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

func TestOrgGoalDelete(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventDraft, FiscalYearID: "fy1"}}
	h := newHandler(store)
	r := orgReq("DELETE", "/orgs/acme/events/evt1/goals/g1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("goal_id", "g1")
	w := httptest.NewRecorder()
	h.OrgGoalDelete(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgBudgetLine ─────────────────────────────────────────────────────────────

func TestOrgBudgetLineCreate(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventDraft, FiscalYearID: "fy1"}}
	h := newHandler(store)
	form := url.Values{"description": {"Office Rent"}, "amount": {"3000"}, "category": {"Facilities"}, "type": {"expense"}}
	r := orgReq("POST", "/orgs/acme/events/evt1/budget", "acme", form)
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgBudgetLineCreate(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestOrgBudgetLineCreate_EventNotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/events/bad/budget", "acme", url.Values{"description": {"X"}, "amount": {"100"}})
	r.SetPathValue("event_id", "bad")
	w := httptest.NewRecorder()
	h.OrgBudgetLineCreate(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrgBudgetLineDelete(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventDraft, FiscalYearID: "fy1"}}
	h := newHandler(store)
	r := orgReq("DELETE", "/orgs/acme/events/evt1/budget/bl1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("line_id", "bl1")
	w := httptest.NewRecorder()
	h.OrgBudgetLineDelete(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── OrgRequestList / New / Detail / Action ────────────────────────────────────

func TestOrgRequestList_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/requests", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgRequestList(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgRequestNew_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/requests/new", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgRequestNew_POST(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{"description": {"New server"}, "amount": {"2500"}, "type": {"purchase_order"}, "date": {"2025-04-01"}}
	r := orgReq("POST", "/orgs/acme/requests/new", "acme", form)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestOrgRequestDetail_NotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/requests/req99", "acme", nil)
	r.SetPathValue("req_id", "req99") // handler uses "req_id" path value
	w := httptest.NewRecorder()
	h.OrgRequestDetail(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrgRequestAction_NotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/requests/req99/action", "acme", url.Values{"action": {"approve"}})
	r.SetPathValue("req_id", "req99") // handler uses "req_id" path value
	w := httptest.NewRecorder()
	h.OrgRequestAction(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── OrgLedger ────────────────────────────────────────────────────────────────

func TestOrgLedger_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/ledger", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgLedger(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgLedger_GET_WithQueryYear(t *testing.T) {
	// OrgLedger is GET-only; pass year via query param
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/ledger?year_id=fy1", "acme", nil)
	w := httptest.NewRecorder()
	h.OrgLedger(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgBankImport ─────────────────────────────────────────────────────────────

func TestOrgBankImport_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/bank-import", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgBankImport(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestOrgBankImport_POST(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{
		"csv_data": {"Date,Description,Amount\n2025-01-15,Office Supplies,-150.00\n"},
		"year_id":  {"fy1"},
	}
	r := orgReq("POST", "/orgs/acme/bank-import", "acme", form)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgBankImport(w, r)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── OrgAnalysis ──────────────────────────────────────────────────────────────

func TestOrgAnalysis_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/analysis", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgAnalysis(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestOrgAnalysis_WithEvents(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Q1 Plan", Status: EventApproved, CreatedAt: time.Now()},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/analysis", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgAnalysis(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgReport ────────────────────────────────────────────────────────────────

func TestOrgReport_GET(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/report", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgReport(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── requireOrgMember / requireOrgRole ────────────────────────────────────────

func TestRequireOrgMember_NotMember(t *testing.T) {
	org := &Org{ID: "org2", Name: "Other Corp", Slug: "other"}
	store := &mockStore{
		orgsBySlug: map[string]*Org{"other": org},
		orgsByID:   map[string]*Org{"org2": org},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.OrgHome(w, orgReq("GET", "/orgs/other", "other", nil))
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRequireOrgRole_InsufficientRole(t *testing.T) {
	org, _, fy := testOrg()
	viewer := &OrgMember{ID: "m2", OrgID: "org1", UserID: "user1", Role: OrgRoleViewer}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": viewer},
		fiscalYears:  []FiscalYear{*fy},
	}
	h := newHandler(store)
	w := httptest.NewRecorder()
	h.OrgTeamCreate(w, orgReq("POST", "/orgs/acme/teams", "acme", url.Values{"name": {"Dev Team"}}))
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// ── canManageOrg ──────────────────────────────────────────────────────────────

func TestCanManageOrg(t *testing.T) {
	if !canManageOrg(OrgRoleAdmin) {
		t.Error("admin should manage")
	}
	if !canManageOrg(OrgRoleFinance) {
		t.Error("finance should manage")
	}
	if canManageOrg(OrgRoleMember) {
		t.Error("member should NOT manage")
	}
	if canManageOrg(OrgRoleViewer) {
		t.Error("viewer should NOT manage")
	}
}

// ── models_org.go: CurrentStatus ─────────────────────────────────────────────

func TestTxRequestCurrentStatus(t *testing.T) {
	req := &TxRequest{}
	if req.CurrentStatus() != TxDraft {
		t.Errorf("empty log should return TxDraft, got %v", req.CurrentStatus())
	}
	req.StatusLog = []StatusLogEntry{{Status: TxSubmitted}}
	if req.CurrentStatus() != TxSubmitted {
		t.Errorf("want TxSubmitted, got %v", req.CurrentStatus())
	}
	req.StatusLog = append(req.StatusLog, StatusLogEntry{Status: TxApproved})
	if req.CurrentStatus() != TxApproved {
		t.Errorf("want TxApproved, got %v", req.CurrentStatus())
	}
}

// ── handler_property.go pure functions ───────────────────────────────────────

func TestLoanMonthlyPayment(t *testing.T) {
	if p := loanMonthlyPayment(100000, 5.0, 0); p != 0 {
		t.Errorf("zero term = %d, want 0", p)
	}
	if p := loanMonthlyPayment(120000, 0, 12); p != 10000 {
		t.Errorf("zero rate = %d, want 10000", p)
	}
	p := loanMonthlyPayment(10000000, 5.0, 240)
	if p < 60000 || p > 70000 {
		t.Errorf("payment = %d, want ~66000", p)
	}
}

func TestLoanRemainingMonths(t *testing.T) {
	if m := loanRemainingMonths(0, 5.0, 1000); m != 0 {
		t.Errorf("zero balance = %d, want 0", m)
	}
	if m := loanRemainingMonths(100000, 5.0, 0); m != 0 {
		t.Errorf("zero payment = %d, want 0", m)
	}
	if m := loanRemainingMonths(120000, 0, 10000); m != 12 {
		t.Errorf("zero rate months = %d, want 12", m)
	}
	if m := loanRemainingMonths(1000000, 24.0, 100); m != 999 {
		t.Errorf("insufficient payment = %d, want 999", m)
	}
}

func TestParseFormCents(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"180000", 18000000},
		{"1500.50", 150050},
		{"1500,50", 150050},
		{"abc", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := parseFormCents(tt.in); got != tt.want {
			t.Errorf("parseFormCents(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestGenID(t *testing.T) {
	a, b := genID(), genID()
	if a == b {
		t.Error("genID should return unique values")
	}
	if len(a) != 24 {
		t.Errorf("genID length = %d, want 24", len(a))
	}
}

func TestLoanBalanceAt(t *testing.T) {
	if b := loanBalanceAt(10000000, 5.0, 66000, 0); b != 10000000 {
		t.Errorf("balance at 0 = %d, want 10000000", b)
	}
	if b := loanBalanceAt(10000000, 5.0, 66000, 240); b < 0 || b > 500000 {
		t.Errorf("balance at end = %d, want near 0", b)
	}
}

func TestProperties_GET(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Properties(w, authReq("GET", "/property", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestProperties_POST_AddProperty(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"action": {"add_property"}, "name": {"My Flat"}, "address": {"123 Main St"}, "current_value": {"250000"}, "purchase_price": {"200000"}}
	w := httptest.NewRecorder()
	h.Properties(w, authReq("POST", "/property", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestProperties_POST_AddLoan(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"action": {"add_loan"}, "lender": {"Bank X"}, "balance": {"150000"}, "rate": {"3.5"}, "term_months": {"240"}, "monthly": {"800"}}
	w := httptest.NewRecorder()
	h.Properties(w, authReq("POST", "/property", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestProperties_POST_UpdateProperty(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"action": {"update_property"}, "id": {"prop1"}, "current_value": {"260000"}, "status": {"active"}}
	w := httptest.NewRecorder()
	h.Properties(w, authReq("POST", "/property", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestProperties_POST_DeleteProperty(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"action": {"delete_property"}, "id": {"prop1"}}
	w := httptest.NewRecorder()
	h.Properties(w, authReq("POST", "/property", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestProperties_POST_DeleteLoan(t *testing.T) {
	h := newHandler(&mockStore{})
	form := url.Values{"action": {"delete_loan"}, "id": {"loan1"}}
	w := httptest.NewRecorder()
	h.Properties(w, authReq("POST", "/property", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestToLoanView(t *testing.T) {
	loan := Loan{ID: "l1", UserID: "user1", Name: "Bank X", BalanceCents: 15000000, InterestRatePct: 3.5, TermMonths: 240, MonthlyPaymentCents: 80000, Status: LoanActive}
	v := toLoanView(loan)
	if v.ID != "l1" {
		t.Errorf("ID = %q, want l1", v.ID)
	}
	if v.BalanceCents != 15000000 {
		t.Errorf("BalanceCents = %d, want 15000000", v.BalanceCents)
	}
}

func TestToPropertyView_NoLoan(t *testing.T) {
	prop := Property{ID: "p1", UserID: "user1", Name: "My Flat", CurrentValueCents: 25000000, Status: PropertyOwned}
	v := toPropertyView(prop, nil)
	if v.ID != "p1" {
		t.Errorf("ID = %q, want p1", v.ID)
	}
	if v.EquityCents != 25000000 {
		t.Errorf("EquityCents = %d, want 25000000", v.EquityCents)
	}
}

func TestToPropertyView_WithLoan(t *testing.T) {
	prop := Property{ID: "p1", UserID: "user1", Name: "My Flat", CurrentValueCents: 25000000, Status: PropertyOwned}
	loans := []Loan{{ID: "l1", UserID: "user1", PropertyID: "p1", BalanceCents: 10000000, InterestRatePct: 3.0, TermMonths: 180, MonthlyPaymentCents: 70000, Status: LoanActive}}
	v := toPropertyView(prop, loans)
	if v.LinkedLoan == nil {
		t.Error("expected loan to be linked")
	}
	if v.EquityCents != 15000000 {
		t.Errorf("EquityCents = %d, want 15000000", v.EquityCents)
	}
}

// ── handler_dream.go ─────────────────────────────────────────────────────────

func TestDream_Redirect(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.Dream(w, authReq("GET", "/plan", nil))
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "/goals") {
		t.Error("expected redirect to /goals")
	}
}

func TestParseFloatParam(t *testing.T) {
	if v := parseFloatParam("", 1.5); v != 1.5 {
		t.Errorf("empty = %f, want 1.5", v)
	}
	if v := parseFloatParam("3.14", 0); v != 3.14 {
		t.Errorf("valid = %f, want 3.14", v)
	}
	if v := parseFloatParam("abc", 2.0); v != 2.0 {
		t.Errorf("invalid = %f, want 2.0", v)
	}
	if v := parseFloatParam("-1", 5.0); v != 5.0 {
		t.Errorf("negative = %f, want 5.0", v)
	}
}

func TestParseIntParam(t *testing.T) {
	if v := parseIntParam("", 10); v != 10 {
		t.Errorf("empty = %d, want 10", v)
	}
	if v := parseIntParam("42", 0); v != 42 {
		t.Errorf("valid = %d, want 42", v)
	}
	if v := parseIntParam("abc", 5); v != 5 {
		t.Errorf("invalid = %d, want 5", v)
	}
	if v := parseIntParam("-3", 5); v != 5 {
		t.Errorf("negative = %d, want 5", v)
	}
}

func TestRunDreamSim_Empty(t *testing.T) {
	if result := runDreamSim(DreamForm{}, nil, nil); result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunDreamSim_WithValues(t *testing.T) {
	form := DreamForm{
		DreamCostCents: 30000000, DownPaymentPct: 20.0,
		ConstructionRatePct: 4.5, ConstructionTermYears: 20, MonthlySavingsCents: 200000,
	}
	if result := runDreamSim(form, nil, nil); result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunPurchaseSim(t *testing.T) {
	if result := runPurchaseSim("Holiday", 500000, 50000, "2026-12"); result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunPurchaseSim_PastDeadline(t *testing.T) {
	if result := runPurchaseSim("Old Goal", 100000, 1000, "2020-01"); result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunPurchaseSim_AlreadySaved(t *testing.T) {
	if result := runPurchaseSim("Done", 100000, 500000, "2026-12"); result == nil {
		t.Fatal("expected non-nil result")
	}
}

// ── handler_auth.go helpers ───────────────────────────────────────────────────

func TestSignAndVerifySessionID(t *testing.T) {
	h := newHandler(&mockStore{})
	id := "sess-abc-123"
	token := h.signSessionID(id)
	got, ok := h.verifySessionToken(token)
	if !ok {
		t.Error("expected valid token")
	}
	if got != id {
		t.Errorf("got %q, want %q", got, id)
	}
}

func TestVerifySessionToken_Invalid(t *testing.T) {
	h := newHandler(&mockStore{})
	if _, ok := h.verifySessionToken("no-dot-here"); ok {
		t.Error("expected invalid: no dot")
	}
	if _, ok := h.verifySessionToken("abc.wrongsig"); ok {
		t.Error("expected invalid: wrong sig")
	}
}

func TestRateLimiter_AllowAndFail(t *testing.T) {
	rl := newLoginRateLimiter()
	ip := "10.0.0.1"
	if !rl.allow(ip) {
		t.Error("expected allow before any failures")
	}
	for i := 0; i < rlMaxFailures; i++ {
		rl.failure(ip)
	}
	if rl.allow(ip) {
		t.Error("expected blocked after max failures")
	}
	rl.success(ip)
	if !rl.allow(ip) {
		t.Error("expected allow after success")
	}
}

func TestAuthLoginPost_EmptyFields(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthLogin(w, authReq("POST", "/auth/login", url.Values{"email": {""}, "password": {""}}))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthLoginPost_UserNotFound(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthLogin(w, authReq("POST", "/auth/login", url.Values{"email": {"nobody@example.com"}, "password": {"pass123"}}))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthRegisterPost_InvalidEmail(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthRegister(w, authReq("POST", "/auth/register", url.Values{"email": {"not-an-email"}, "password": {"secret123"}}))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthRegisterPost_ShortPassword(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthRegister(w, authReq("POST", "/auth/register", url.Values{"email": {"user@example.com"}, "password": {"short"}}))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthLogout_Redirect(t *testing.T) {
	h := newHandler(&mockStore{})
	w := httptest.NewRecorder()
	h.AuthLogout(w, httptest.NewRequest("POST", "/auth/logout", nil))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "login") {
		t.Error("expected redirect to login")
	}
}

// ── i18n ─────────────────────────────────────────────────────────────────────

func TestTranslator_MissingKey(t *testing.T) {
	v := newT("en").Get("no.such.key.xyz")
	if v != "" && !strings.Contains(v, "no.such.key") {
		t.Errorf("missing key = %q, expected empty or key name", v)
	}
}

func TestTranslator_Lang(t *testing.T) {
	if l := newT("en").Lang(); l != "en" {
		t.Errorf("Lang() = %q, want en", l)
	}
	if l := newT("pt").Lang(); l != "pt" {
		t.Errorf("Lang() = %q, want pt", l)
	}
}

func TestTranslator_UnsupportedLang(t *testing.T) {
	if tx := newT("xx"); tx == nil {
		t.Fatal("expected non-nil translator")
	}
}

func TestTranslator_FallsBackToEN(t *testing.T) {
	enVal := newT("en").Get("nav.dashboard")
	ptVal := newT("pt").Get("nav.dashboard")
	if enVal == "" {
		t.Error("EN nav.dashboard should not be empty")
	}
	if ptVal == "" {
		t.Error("PT nav.dashboard should not be empty")
	}
}

// ── securityHeaders middleware ────────────────────────────────────────────────

func TestSecurityHeaders(t *testing.T) {
	h := newHandler(&mockStore{})
	called := false
	mw := h.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Error("handler not called through securityHeaders")
	}
}

// ── parseEuroAmount ────────────────────────────────────────────────────────────

func TestParseEuroAmount(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"150", 15000, false},
		{"150.50", 15050, false},
		{"150,50", 15050, false},  // comma as decimal
		{"150.5", 15050, false},   // one decimal digit
		{"150.505", 15050, false}, // truncates to 2 decimals
		{"0", 0, false},
		{"", 0, false},
		{"abc", 0, true},
		{"-50", -5000, false}, // negative amounts are valid (expenses)
	}
	for _, tt := range tests {
		got, err := parseEuroAmount(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseEuroAmount(%q) expected error, got %d", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseEuroAmount(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseEuroAmount(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseInt64(t *testing.T) {
	if v, err := parseInt64(""); v != 0 || err != nil {
		t.Errorf("empty = %d/%v, want 0/nil", v, err)
	}
	if v, err := parseInt64("42"); v != 42 || err != nil {
		t.Errorf("42 = %d/%v", v, err)
	}
	if v, err := parseInt64("-10"); v != -10 || err != nil {
		t.Errorf("-10 = %d/%v", v, err)
	}
	if _, err := parseInt64("abc"); err == nil {
		t.Error("expected error for 'abc'")
	}
}

func TestMaxHelper(t *testing.T) {
	if max(3, 5) != 5 {
		t.Error("max(3,5) should be 5")
	}
	if max(5, 3) != 5 {
		t.Error("max(5,3) should be 5")
	}
	if max(4, 4) != 4 {
		t.Error("max(4,4) should be 4")
	}
}

// ── csvReader / parseBankCSV ───────────────────────────────────────────────────

func TestCsvReader(t *testing.T) {
	data := "Name,Value\nAlice,42\nBob,99\n"
	rows, err := csvReader(strings.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("rows = %d, want 3", len(rows))
	}
}

func TestParseBankCSV_Valid(t *testing.T) {
	// Note: '+' prefix on amounts fails parseEuroAmount, use plain numbers
	data := "date,description,amount\n2025-01-15,Office Supplies,-150.00\n2025-01-20,Income,500.00\n"
	rows, err := parseBankCSV(strings.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2", len(rows))
	}
}

func TestParseBankCSV_Empty(t *testing.T) {
	_, err := parseBankCSV(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty CSV")
	}
}

func TestParseBankCSV_HeaderOnly(t *testing.T) {
	// Only 1 record total (the header) → "CSV is empty" error
	_, err := parseBankCSV(strings.NewReader("date,description,amount\n"))
	if err == nil {
		t.Error("header-only (1 record) should return 'CSV is empty' error")
	}
}

func TestParseBankCSV_MissingColumns(t *testing.T) {
	_, err := parseBankCSV(strings.NewReader("name,value\nAlice,42\n"))
	if err == nil {
		t.Error("expected error for CSV missing required columns")
	}
}

// ── OrgRequestDelivery / Settle: not-found paths ──────────────────────────────

func TestOrgRequestDelivery_NotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/requests/bad/delivery", "acme", url.Values{"actual_amount": {"150"}})
	r.SetPathValue("req_id", "bad")
	w := httptest.NewRecorder()
	h.OrgRequestDelivery(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrgRequestSettle_NotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/requests/bad/settle", "acme", nil)
	r.SetPathValue("req_id", "bad")
	w := httptest.NewRecorder()
	h.OrgRequestSettle(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrgRequestUpload_NotFound(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("POST", "/orgs/acme/requests/bad/upload", "acme", nil)
	r.SetPathValue("req_id", "bad")
	w := httptest.NewRecorder()
	h.OrgRequestUpload(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── OrgRequestAction: found + submit ─────────────────────────────────────────

func TestOrgRequestAction_SubmitDraft(t *testing.T) {
	store := newOrgStore()
	req := TxRequest{
		ID: "req1", OrgID: "org1", SubmittedBy: "user1",
		Type: TxPurchaseOrder, AmountCents: 50000,
		StatusLog: []StatusLogEntry{{Status: TxDraft}},
	}
	store.txRequests = []TxRequest{req}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/requests/req1/action", "acme", url.Values{"action": {"submit"}})
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestAction(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestOrgRequestAction_Cancel(t *testing.T) {
	store := newOrgStore()
	req := TxRequest{
		ID: "req1", OrgID: "org1", SubmittedBy: "user1",
		Type: TxPurchaseOrder, AmountCents: 50000,
		StatusLog: []StatusLogEntry{{Status: TxDraft}},
	}
	store.txRequests = []TxRequest{req}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/requests/req1/action", "acme", url.Values{"action": {"cancel"}})
	r.SetPathValue("req_id", "req1")
	w := httptest.NewRecorder()
	h.OrgRequestAction(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %s", w.Code, w.Body.String())
	}
}

// ── OrgEventList with year_id ─────────────────────────────────────────────────

func TestOrgEventList_WithYearID(t *testing.T) {
	h := newHandler(newOrgStore())
	r := orgReq("GET", "/orgs/acme/years/fy1/events", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventList(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgReport with events ─────────────────────────────────────────────────────

func TestOrgReport_WithEvents(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{
		{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Name: "Q1", Status: EventApproved},
	}
	h := newHandler(store)
	r := orgReq("GET", "/orgs/acme/report", "acme", nil)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgReport(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── OrgRequestNew: invalid type ───────────────────────────────────────────────

func TestOrgRequestNew_InvalidType(t *testing.T) {
	// OrgRequestNew validates "type" field — description emptiness is allowed
	h := newHandler(newOrgStore())
	form := url.Values{"description": {""}, "amount": {"100"}, "type": {"unknown_type"}}
	r := orgReq("POST", "/orgs/acme/requests/new", "acme", form)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgRequestNew(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── OrgFiscalYearActivate: all events approved ────────────────────────────────

func TestOrgFiscalYearActivate_UnapprovedEvent(t *testing.T) {
	org, member, _ := testOrg()
	draft := FiscalYear{ID: "fy2", OrgID: "org1", Label: "2026", Status: FiscalYearDraft,
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{draft},
		orgEvents:    []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy2", Name: "Pending", Status: EventDraft}},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/years/fy2/activate", "acme", nil)
	r.SetPathValue("year_id", "fy2")
	w := httptest.NewRecorder()
	h.OrgFiscalYearActivate(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (unapproved event)", w.Code)
	}
}

// ── OrgBankImport POST: valid CSV import ──────────────────────────────────────

func TestOrgBankImport_POST_ValidCSV(t *testing.T) {
	h := newHandler(newOrgStore())
	form := url.Values{
		"csv_data": {"date,description,amount\n2025-01-15,Coffee,-15.00\n2025-01-20,Revenue,1000.00\n"},
	}
	r := orgReq("POST", "/orgs/acme/bank-import", "acme", form)
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgBankImport(w, r)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500: %s", w.Body.String())
	}
}

// ── OrgJoin: expired token ────────────────────────────────────────────────────

func TestOrgJoin_ExpiredToken(t *testing.T) {
	// The mock returns the invite regardless of expiry (real store would filter).
	// Handler renders the join page for any found invite — expiry is enforced in store.
	org, _, _ := testOrg()
	store := &mockStore{
		orgsByID: map[string]*Org{"org1": org},
		invitesByToken: map[string]*OrgInvite{
			"expired": {
				ID: "inv1", OrgID: "org1", Email: "old@example.com",
				Role: OrgRoleMember, Token: "expired",
				ExpiresAt: time.Now().Add(-24 * time.Hour),
			},
		},
	}
	h := newHandler(store)
	r := authReq("GET", "/orgs/join/expired", nil)
	r.SetPathValue("token", "expired")
	w := httptest.NewRecorder()
	h.OrgJoin(w, r)
	// Mock returns the invite (no expiry check in mock) → handler renders join page
	if w.Code == http.StatusInternalServerError {
		t.Error("unexpected 500")
	}
}

// ── OrgEventDelete: non-draft event ──────────────────────────────────────────

func TestOrgEventDelete_NonDraft(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Status: EventReview}}
	h := newHandler(store)
	r := orgReq("DELETE", "/orgs/acme/events/evt1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgEventDelete(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (can't delete non-draft)", w.Code)
	}
}

// ── OrgEventSubmit: non-draft event ──────────────────────────────────────────

func TestOrgEventSubmit_NonDraft(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Status: EventReview}}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/events/evt1/submit", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgEventSubmit(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (can't submit non-draft)", w.Code)
	}
}

// ── OrgGoalDelete: approved event ────────────────────────────────────────────

func TestOrgGoalDelete_ApprovedEvent(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", FiscalYearID: "fy1", Status: EventApproved}}
	h := newHandler(store)
	r := orgReq("DELETE", "/orgs/acme/events/evt1/goals/g1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("goal_id", "g1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgGoalDelete(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (can't delete goals from approved event)", w.Code)
	}
}

// ── OrgBudgetLineCreate: approved event ──────────────────────────────────────

func TestOrgBudgetLineCreate_ApprovedEvent(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventApproved, FiscalYearID: "fy1"}}
	h := newHandler(store)
	form := url.Values{"description": {"X"}, "amount": {"100"}, "type": {"expense"}}
	r := orgReq("POST", "/orgs/acme/events/evt1/budget", "acme", form)
	r.SetPathValue("event_id", "evt1")
	w := httptest.NewRecorder()
	h.OrgBudgetLineCreate(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

// ── OrgBudgetLineDelete: approved event ──────────────────────────────────────

func TestOrgBudgetLineDelete_ApprovedEvent(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventApproved, FiscalYearID: "fy1"}}
	h := newHandler(store)
	r := orgReq("DELETE", "/orgs/acme/events/evt1/budget/bl1", "acme", nil)
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("line_id", "bl1")
	w := httptest.NewRecorder()
	h.OrgBudgetLineDelete(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

// ── OrgEventReview: not-under-review ─────────────────────────────────────────

func TestOrgEventReview_NotUnderReview(t *testing.T) {
	store := newOrgStore()
	store.orgEvents = []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventDraft, FiscalYearID: "fy1"}}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/events/evt1/review", "acme", url.Values{"action": {"approve"}})
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgEventReview(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (event not under review)", w.Code)
	}
}

// ── OrgGoalToggle: non-active year ───────────────────────────────────────────

func TestOrgGoalToggle_InactiveYear(t *testing.T) {
	org, member, _ := testOrg()
	closed := FiscalYear{ID: "fy1", OrgID: "org1", Label: "2025", Status: FiscalYearClosed,
		StartDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)}
	store := &mockStore{
		orgsBySlug:   map[string]*Org{"acme": org},
		orgsByID:     map[string]*Org{"org1": org},
		membersByKey: map[string]*OrgMember{"org1:user1": member},
		fiscalYears:  []FiscalYear{closed},
		orgEvents:    []OrgEvent{{ID: "evt1", OrgID: "org1", Status: EventApproved, FiscalYearID: "fy1"}},
	}
	h := newHandler(store)
	r := orgReq("POST", "/orgs/acme/events/evt1/goals/g1/toggle", "acme", url.Values{"done": {"1"}})
	r.SetPathValue("event_id", "evt1")
	r.SetPathValue("goal_id", "g1")
	r.SetPathValue("year_id", "fy1")
	w := httptest.NewRecorder()
	h.OrgGoalToggle(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (closed year)", w.Code)
	}
}

// ── OrgCreate: duplicate slug ─────────────────────────────────────────────────

func TestOrgCreate_POST_SlugCheck(t *testing.T) {
	// Mock's slugExists always returns false, so any slug succeeds
	h := newHandler(&mockStore{})
	form := url.Values{"name": {"Test Corp"}, "slug": {"test-corp"}}
	w := httptest.NewRecorder()
	h.OrgCreate(w, authReq("POST", "/orgs/new", form))
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", w.Code)
	}
}

// ── fmt sentinel ──────────────────────────────────────────────────────────────

var _ = fmt.Sprintf

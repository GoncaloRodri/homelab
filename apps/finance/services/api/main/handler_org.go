package main

import (
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// slugRe validates org slugs: lowercase letters, digits, hyphens.
var slugRe = regexp.MustCompile(`^[a-z0-9-]{2,40}$`)

// orgTmpl and friends are registered in handler.go parseTmpl block (below).

// ── Middleware helpers ────────────────────────────────────────────────────────

// orgMW loads the org and the caller's membership. Injects them via context values
// wrapped in the request, or returns 403/404. next receives the same request.
//
// It sets two request-scoped values accessible via orgFromCtx / memberFromCtx:
// these are passed as function arguments here for simplicity (no context key needed).
func (h *Handler) requireOrgMember(next func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember)) http.HandlerFunc {
	return h.authMW(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		a := getAuth(r)
		slug := r.PathValue("slug")

		org, err := h.store.getOrgBySlug(ctx, slug)
		if err != nil {
			http.Error(w, "organisation not found", http.StatusNotFound)
			return
		}
		me, err := h.store.getMember(ctx, org.ID, a.UserID)
		if err != nil {
			http.Error(w, "you are not a member of this organisation", http.StatusForbidden)
			return
		}
		next(w, r, org, me)
	})
}

func (h *Handler) requireOrgRole(roles ...OrgRole) func(next func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember)) http.HandlerFunc {
	return func(next func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember)) http.HandlerFunc {
		return h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
			for _, role := range roles {
				if me.Role == role {
					next(w, r, org, me)
					return
				}
			}
			http.Error(w, "insufficient permissions", http.StatusForbidden)
		})
	}
}

// canManageOrg is true for admin and finance.
func canManageOrg(role OrgRole) bool {
	return role == OrgRoleAdmin || role == OrgRoleFinance
}

// ── Org list & creation ───────────────────────────────────────────────────────

func (h *Handler) OrgList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	orgs, err := h.store.getOrgsForUser(ctx, a.UserID)
	if err != nil {
		slog.Error("get orgs for user", "err", err)
		orgs = nil
	}

	render(w, orgListTmpl, &OrgListData{
		UserID: a.UserID,
		Email:  a.Email,
		Title:  "Organisations",
		Route:  "orgs",
		Orgs:   orgs,
	})
}

func (h *Handler) OrgCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)

	if r.Method == http.MethodGet {
		render(w, orgCreateTmpl, map[string]any{
			"Title":  "New Organisation",
			"Route":  "orgs",
			"UserID": a.UserID,
			"Email":  a.Email,
		})
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	slug := strings.TrimSpace(strings.ToLower(r.FormValue("slug")))

	errMsg := ""
	switch {
	case name == "":
		errMsg = "Name is required."
	case !slugRe.MatchString(slug):
		errMsg = "Slug must be 2–40 lowercase letters, digits or hyphens."
	}

	if errMsg == "" {
		exists, _ := h.store.slugExists(ctx, slug)
		if exists {
			errMsg = "That slug is already taken — choose another."
		}
	}

	if errMsg != "" {
		render(w, orgCreateTmpl, map[string]any{
			"Title":  "New Organisation",
			"Route":  "orgs",
			"UserID": a.UserID,
			"Email":  a.Email,
			"Error":  errMsg,
			"Name":   name,
			"Slug":   slug,
		})
		return
	}

	org := &Org{
		ID:          bson.NewObjectID().Hex(),
		Name:        name,
		Slug:        slug,
		OwnerUserID: a.UserID,
		CreatedAt:   time.Now(),
	}
	if err := h.store.createOrg(ctx, org); err != nil {
		slog.Error("create org", "err", err)
		http.Error(w, "could not create organisation", http.StatusInternalServerError)
		return
	}

	// creator becomes admin
	member := &OrgMember{
		ID:        bson.NewObjectID().Hex(),
		OrgID:     org.ID,
		UserID:    a.UserID,
		Email:     a.Email,
		Role:      OrgRoleAdmin,
		CreatedAt: time.Now(),
	}
	if err := h.store.createMember(ctx, member); err != nil {
		slog.Error("create founding member", "err", err)
	}

	http.Redirect(w, r, "/orgs/"+slug, http.StatusSeeOther)
}

// ── Org home ──────────────────────────────────────────────────────────────────

func (h *Handler) OrgHome(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()

		years, _ := h.store.getFiscalYears(ctx, org.ID)
		teams, _ := h.store.getTeams(ctx, org.ID)
		members, _ := h.store.getMembers(ctx, org.ID)

		var active *FiscalYear
		for i := range years {
			if years[i].Status == FiscalYearActive {
				active = &years[i]
				break
			}
		}

		render(w, orgHomeTmpl, &OrgHomeData{
			UserID:      r.Header.Get("X-Auth-User-Id"),
			Email:       r.Header.Get("X-Auth-Email"),
			Title:       org.Name,
			Route:       "orgs",
			Org:         *org,
			MyRole:      me.Role,
			MyTeamIDs:   me.TeamIDs,
			FiscalYears: years,
			ActiveYear:  active,
			Teams:       teams,
			Members:     members,
		})
	})(w, r)
}

// ── Teams ─────────────────────────────────────────────────────────────────────

func (h *Handler) OrgTeams(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		teams, _ := h.store.getTeams(ctx, org.ID)
		members, _ := h.store.getMembers(ctx, org.ID)

		render(w, orgTeamsTmpl, &OrgTeamsData{
			UserID:  r.Header.Get("X-Auth-User-Id"),
			Email:   r.Header.Get("X-Auth-Email"),
			Title:   org.Name + " — Teams",
			Route:   "orgs",
			Org:     *org,
			MyRole:  me.Role,
			Teams:   teams,
			Members: members,
		})
	})(w, r)
}

func (h *Handler) OrgTeamCreate(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		name := strings.TrimSpace(r.FormValue("name"))
		teamType := TeamType(r.FormValue("type"))
		if teamType != TeamTypeGuest {
			teamType = TeamTypeInternal
		}
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		team := &OrgTeam{
			ID:        bson.NewObjectID().Hex(),
			OrgID:     org.ID,
			Name:      name,
			Type:      teamType,
			CreatedAt: time.Now(),
		}
		if err := h.store.createTeam(ctx, team); err != nil {
			slog.Error("create team", "err", err)
			http.Error(w, "could not create team", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/teams", http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgTeamDelete(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		teamID := r.PathValue("team_id")
		if err := h.store.deleteTeam(r.Context(), teamID, org.ID); err != nil {
			slog.Error("delete team", "err", err)
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/teams", http.StatusSeeOther)
	})(w, r)
}

// ── Members ───────────────────────────────────────────────────────────────────

func (h *Handler) OrgMembers(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		members, _ := h.store.getMembers(ctx, org.ID)
		teams, _ := h.store.getTeams(ctx, org.ID)
		invites, _ := h.store.getInvites(ctx, org.ID)

		render(w, orgMembersTmpl, &OrgMembersData{
			UserID:  r.Header.Get("X-Auth-User-Id"),
			Email:   r.Header.Get("X-Auth-Email"),
			Title:   org.Name + " — Members",
			Route:   "orgs",
			Org:     *org,
			MyRole:  me.Role,
			Members: members,
			Teams:   teams,
			Invites: invites,
		})
	})(w, r)
}

func (h *Handler) OrgMemberRoleUpdate(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		memberID := r.PathValue("member_id")
		role := OrgRole(r.FormValue("role"))
		switch role {
		case OrgRoleAdmin, OrgRoleFinance, OrgRoleMember, OrgRoleViewer:
		default:
			http.Error(w, "invalid role", http.StatusBadRequest)
			return
		}
		if err := h.store.updateMemberRole(ctx, memberID, org.ID, role); err != nil {
			slog.Error("update member role", "err", err)
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/members", http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgMemberRemove(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		memberID := r.PathValue("member_id")
		// prevent removing yourself
		a := getAuth(r)
		if me.UserID == a.UserID && memberID == me.ID {
			http.Error(w, "cannot remove yourself", http.StatusBadRequest)
			return
		}
		if err := h.store.removeMember(r.Context(), memberID, org.ID); err != nil {
			slog.Error("remove member", "err", err)
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/members", http.StatusSeeOther)
	})(w, r)
}

// ── Invites ───────────────────────────────────────────────────────────────────

func (h *Handler) OrgInviteNew(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		teams, _ := h.store.getTeams(ctx, org.ID)

		if r.Method == http.MethodGet {
			render(w, orgInviteTmpl, &OrgInviteData{
				UserID: r.Header.Get("X-Auth-User-Id"),
				Email:  r.Header.Get("X-Auth-Email"),
				Title:  "Invite to " + org.Name,
				Route:  "orgs",
				Org:    *org,
				MyRole: me.Role,
				Teams:  teams,
			})
			return
		}

		email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
		role := OrgRole(r.FormValue("role"))
		teamIDs := r.Form["team_ids"]

		errMsg := ""
		switch {
		case email == "":
			errMsg = "Email is required."
		case role != OrgRoleAdmin && role != OrgRoleFinance && role != OrgRoleMember && role != OrgRoleViewer:
			errMsg = "Invalid role."
		}

		if errMsg != "" {
			render(w, orgInviteTmpl, &OrgInviteData{
				UserID: r.Header.Get("X-Auth-User-Id"),
				Email:  r.Header.Get("X-Auth-Email"),
				Title:  "Invite to " + org.Name,
				Route:  "orgs",
				Org:    *org,
				MyRole: me.Role,
				Teams:  teams,
				Error:  errMsg,
			})
			return
		}

		token := randomHex(32)
		inv := &OrgInvite{
			ID:        bson.NewObjectID().Hex(),
			OrgID:     org.ID,
			Email:     email,
			Role:      role,
			TeamIDs:   teamIDs,
			Token:     token,
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			CreatedAt: time.Now(),
		}
		if err := h.store.createInvite(ctx, inv); err != nil {
			slog.Error("create invite", "err", err)
			http.Error(w, "could not create invite", http.StatusInternalServerError)
			return
		}

		// Show the link — email delivery is Phase 5
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		link := fmt.Sprintf("%s://%s/join/%s", scheme, r.Host, token)

		render(w, orgInviteTmpl, &OrgInviteData{
			UserID: r.Header.Get("X-Auth-User-Id"),
			Email:  r.Header.Get("X-Auth-Email"),
			Title:  "Invite to " + org.Name,
			Route:  "orgs",
			Org:    *org,
			MyRole: me.Role,
			Teams:  teams,
			Link:   link,
		})
	})(w, r)
}

func (h *Handler) OrgInviteRevoke(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		inviteID := r.PathValue("invite_id")
		if err := h.store.revokeInvite(r.Context(), inviteID, org.ID); err != nil {
			slog.Error("revoke invite", "err", err)
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/members", http.StatusSeeOther)
	})(w, r)
}

// OrgJoin handles the invite link: GET shows a confirmation page, POST accepts.
func (h *Handler) OrgJoin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := getAuth(r)
	token := r.PathValue("token")

	inv, err := h.store.getInviteByToken(ctx, token)
	if err != nil {
		http.Error(w, "invite not found or expired", http.StatusNotFound)
		return
	}
	org, err := h.store.getOrg(ctx, inv.OrgID)
	if err != nil {
		http.Error(w, "organisation not found", http.StatusNotFound)
		return
	}

	if r.Method == http.MethodGet {
		render(w, orgJoinTmpl, map[string]any{
			"Title":  "Join " + org.Name,
			"Route":  "orgs",
			"UserID": a.UserID,
			"Email":  a.Email,
			"Org":    org,
			"Invite": inv,
		})
		return
	}

	// check already a member
	if _, err := h.store.getMember(ctx, org.ID, a.UserID); err == nil {
		// already in — just consume the invite and redirect
		_ = h.store.consumeInvite(ctx, inv.ID)
		http.Redirect(w, r, "/orgs/"+org.Slug, http.StatusSeeOther)
		return
	}

	member := &OrgMember{
		ID:        bson.NewObjectID().Hex(),
		OrgID:     org.ID,
		UserID:    a.UserID,
		Email:     a.Email,
		Role:      inv.Role,
		TeamIDs:   inv.TeamIDs,
		CreatedAt: time.Now(),
	}
	if err := h.store.createMember(ctx, member); err != nil {
		slog.Error("create member from invite", "err", err)
		http.Error(w, "could not join organisation", http.StatusInternalServerError)
		return
	}
	if err := h.store.consumeInvite(ctx, inv.ID); err != nil {
		slog.Error("consume invite", "err", err)
	}
	http.Redirect(w, r, "/orgs/"+org.Slug, http.StatusSeeOther)
}

// ── Fiscal Years ──────────────────────────────────────────────────────────────

func (h *Handler) OrgFiscalYearCreate(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		label := strings.TrimSpace(r.FormValue("label"))
		startStr := r.FormValue("start_date")
		endStr := r.FormValue("end_date")

		start, errS := time.Parse("2006-01-02", startStr)
		end, errE := time.Parse("2006-01-02", endStr)
		if label == "" || errS != nil || errE != nil || !end.After(start) {
			http.Error(w, "invalid fiscal year data", http.StatusBadRequest)
			return
		}

		y := &FiscalYear{
			ID:        bson.NewObjectID().Hex(),
			OrgID:     org.ID,
			Label:     label,
			Status:    FiscalYearDraft,
			StartDate: start,
			EndDate:   end,
			CreatedAt: time.Now(),
		}
		if err := h.store.createFiscalYear(ctx, y); err != nil {
			slog.Error("create fiscal year", "err", err)
			http.Error(w, "could not create fiscal year", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug, http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgFiscalYearActivate(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")

		// verify no other year is already active
		if active, _ := h.store.getActiveFiscalYear(ctx, org.ID); active != nil {
			http.Error(w, "another fiscal year is already active — close it first", http.StatusConflict)
			return
		}

		// verify all events in this year are approved
		events, err := h.store.getEvents(ctx, org.ID, yearID)
		if err != nil {
			http.Error(w, "could not load events", http.StatusInternalServerError)
			return
		}
		for _, e := range events {
			if e.Status != EventApproved {
				http.Error(w, fmt.Sprintf("event %q is not yet approved", e.Name), http.StatusConflict)
				return
			}
		}

		if err := h.store.updateFiscalYearStatus(ctx, yearID, org.ID, FiscalYearActive, bson.M{
			"started_at": time.Now(),
		}); err != nil {
			slog.Error("activate fiscal year", "err", err)
			http.Error(w, "could not activate fiscal year", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug, http.StatusSeeOther)
	})(w, r)
}

// ── Events ────────────────────────────────────────────────────────────────────

func (h *Handler) OrgEventList(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")

		year, err := h.store.getFiscalYear(ctx, yearID, org.ID)
		if err != nil {
			http.Error(w, "fiscal year not found", http.StatusNotFound)
			return
		}
		events, _ := h.store.getEvents(ctx, org.ID, yearID)
		teams, _ := h.store.getTeams(ctx, org.ID)

		teamMap := make(map[string]OrgTeam, len(teams))
		for _, t := range teams {
			teamMap[t.ID] = t
		}

		summaries := make([]OrgEventSummary, 0, len(events))
		for _, ev := range events {
			lines, _ := h.store.getBudgetLines(ctx, ev.ID, org.ID)
			var inc, exp int64
			for _, l := range lines {
				if l.Type == BudgetIncome {
					inc += l.PlannedCents
				} else {
					exp += l.PlannedCents
				}
			}
			evTeams := make([]OrgTeam, 0)
			for _, tid := range ev.TeamIDs {
				if t, ok := teamMap[tid]; ok {
					evTeams = append(evTeams, t)
				}
			}
			summaries = append(summaries, OrgEventSummary{
				Event:        ev,
				TotalIncome:  inc,
				TotalExpense: exp,
				Teams:        evTeams,
			})
		}

		render(w, orgEventsTmpl, &OrgEventsData{
			UserID:     r.Header.Get("X-Auth-User-Id"),
			Email:      r.Header.Get("X-Auth-Email"),
			Title:      org.Name + " — Events",
			Route:      "orgs",
			Org:        *org,
			MyRole:     me.Role,
			FiscalYear: *year,
			Events:     summaries,
			Teams:      teams,
		})
	})(w, r)
}

func (h *Handler) OrgEventNew(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")

		year, err := h.store.getFiscalYear(ctx, yearID, org.ID)
		if err != nil {
			http.Error(w, "fiscal year not found", http.StatusNotFound)
			return
		}
		teams, _ := h.store.getTeams(ctx, org.ID)

		if r.Method == http.MethodGet {
			render(w, orgEventDetailTmpl, &OrgEventDetailData{
				UserID:     r.Header.Get("X-Auth-User-Id"),
				Email:      r.Header.Get("X-Auth-Email"),
				Title:      "New Event",
				Route:      "orgs",
				Org:        *org,
				MyRole:     me.Role,
				FiscalYear: *year,
				Teams:      teams,
			})
			return
		}

		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		startStr := r.FormValue("date_start")
		endStr := r.FormValue("date_end")
		start, errS := time.Parse("2006-01-02", startStr)
		end, errE := time.Parse("2006-01-02", endStr)
		if errS != nil || errE != nil || end.Before(start) {
			http.Error(w, "invalid dates", http.StatusBadRequest)
			return
		}
		teamIDs := r.Form["team_ids"]

		ev := &OrgEvent{
			ID:           bson.NewObjectID().Hex(),
			OrgID:        org.ID,
			FiscalYearID: yearID,
			TeamIDs:      teamIDs,
			Name:         name,
			Description:  r.FormValue("description"),
			Goals:        r.FormValue("goals"),
			DateStart:    start,
			DateEnd:      end,
			Status:       EventDraft,
			CreatedBy:    r.Header.Get("X-Auth-User-Id"),
			CreatedAt:    time.Now(),
		}
		if err := h.store.createEvent(ctx, ev); err != nil {
			slog.Error("create event", "err", err)
			http.Error(w, "could not create event", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/years/"+yearID+"/events/"+ev.ID, http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgEventDetail(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")
		eventID := r.PathValue("event_id")

		year, err := h.store.getFiscalYear(ctx, yearID, org.ID)
		if err != nil {
			http.Error(w, "fiscal year not found", http.StatusNotFound)
			return
		}
		ev, err := h.store.getEvent(ctx, eventID, org.ID)
		if err != nil {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}
		lines, _ := h.store.getBudgetLines(ctx, eventID, org.ID)
		comments, _ := h.store.getEventComments(ctx, eventID, org.ID)
		teams, _ := h.store.getTeams(ctx, org.ID)

		teamMap := make(map[string]OrgTeam, len(teams))
		for _, t := range teams {
			teamMap[t.ID] = t
		}
		evTeams := make([]OrgTeam, 0)
		for _, tid := range ev.TeamIDs {
			if t, ok := teamMap[tid]; ok {
				evTeams = append(evTeams, t)
			}
		}

		var inc, exp int64
		for _, l := range lines {
			if l.Type == BudgetIncome {
				inc += l.PlannedCents
			} else {
				exp += l.PlannedCents
			}
		}

		render(w, orgEventDetailTmpl, &OrgEventDetailData{
			UserID:       r.Header.Get("X-Auth-User-Id"),
			Email:        r.Header.Get("X-Auth-Email"),
			Title:        ev.Name,
			Route:        "orgs",
			Org:          *org,
			MyRole:       me.Role,
			FiscalYear:   *year,
			Event:        *ev,
			BudgetLines:  lines,
			Comments:     comments,
			Teams:        teams,
			EventTeams:   evTeams,
			TotalIncome:  inc,
			TotalExpense: exp,
		})
	})(w, r)
}

func (h *Handler) OrgEventEdit(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")
		eventID := r.PathValue("event_id")

		ev, err := h.store.getEvent(ctx, eventID, org.ID)
		if err != nil {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}
		if ev.Status != EventDraft && ev.Status != EventReview {
			http.Error(w, "cannot edit event in current status", http.StatusConflict)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		startStr := r.FormValue("date_start")
		endStr := r.FormValue("date_end")
		start, errS := time.Parse("2006-01-02", startStr)
		end, errE := time.Parse("2006-01-02", endStr)
		if errS != nil || errE != nil || end.Before(start) {
			http.Error(w, "invalid dates", http.StatusBadRequest)
			return
		}
		update := bson.M{
			"name":        name,
			"description": r.FormValue("description"),
			"goals":       r.FormValue("goals"),
			"date_start":  start,
			"date_end":    end,
			"team_ids":    r.Form["team_ids"],
		}
		if err := h.store.updateEvent(ctx, eventID, org.ID, update); err != nil {
			slog.Error("update event", "err", err)
			http.Error(w, "could not update event", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/years/"+yearID+"/events/"+eventID, http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgEventDelete(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")
		eventID := r.PathValue("event_id")

		ev, err := h.store.getEvent(ctx, eventID, org.ID)
		if err != nil {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}
		if ev.Status != EventDraft {
			http.Error(w, "only draft events can be deleted", http.StatusConflict)
			return
		}
		_ = h.store.deleteEvent(ctx, eventID, org.ID)
		http.Redirect(w, r, "/orgs/"+org.Slug+"/years/"+yearID+"/events", http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgEventSubmit(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")
		eventID := r.PathValue("event_id")

		ev, err := h.store.getEvent(ctx, eventID, org.ID)
		if err != nil {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}
		if ev.Status != EventDraft {
			http.Error(w, "only draft events can be submitted", http.StatusConflict)
			return
		}
		if err := h.store.updateEvent(ctx, eventID, org.ID, bson.M{"status": EventReview}); err != nil {
			slog.Error("submit event", "err", err)
			http.Error(w, "could not submit event", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/years/"+yearID+"/events/"+eventID, http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgEventReview(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin, OrgRoleFinance)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")
		eventID := r.PathValue("event_id")
		action := r.FormValue("action") // "approve", "reject", "comment"
		body := strings.TrimSpace(r.FormValue("comment"))

		ev, err := h.store.getEvent(ctx, eventID, org.ID)
		if err != nil {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}
		if ev.Status != EventReview {
			http.Error(w, "event is not under review", http.StatusConflict)
			return
		}

		if body != "" {
			c := &EventComment{
				ID:        bson.NewObjectID().Hex(),
				EventID:   eventID,
				OrgID:     org.ID,
				UserID:    me.ID,
				UserEmail: me.Email,
				Kind:      CommentReview,
				Body:      body,
				CreatedAt: time.Now(),
			}
			_ = h.store.createEventComment(ctx, c)
		}

		var newStatus EventStatus
		switch action {
		case "approve":
			newStatus = EventApproved
		case "reject":
			newStatus = EventRejected
		default:
			// comment only — stay in review
			http.Redirect(w, r, "/orgs/"+org.Slug+"/years/"+yearID+"/events/"+eventID, http.StatusSeeOther)
			return
		}
		if err := h.store.updateEvent(ctx, eventID, org.ID, bson.M{"status": newStatus}); err != nil {
			slog.Error("review event", "err", err)
			http.Error(w, "could not update event status", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/years/"+yearID+"/events/"+eventID, http.StatusSeeOther)
	})(w, r)
}

// OrgEventFeedback adds a post-mortem feedback comment (kind=feedback) after year closes.
func (h *Handler) OrgEventFeedback(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")
		eventID := r.PathValue("event_id")

		year, err := h.store.getFiscalYear(ctx, yearID, org.ID)
		if err != nil || year.Status != FiscalYearClosed {
			http.Error(w, "feedback only allowed on closed fiscal years", http.StatusConflict)
			return
		}
		body := strings.TrimSpace(r.FormValue("comment"))
		if body == "" {
			http.Error(w, "comment required", http.StatusBadRequest)
			return
		}
		c := &EventComment{
			ID:        bson.NewObjectID().Hex(),
			EventID:   eventID,
			OrgID:     org.ID,
			UserID:    me.ID,
			UserEmail: me.Email,
			Kind:      CommentFeedback,
			Body:      body,
			CreatedAt: time.Now(),
		}
		_ = h.store.createEventComment(ctx, c)
		http.Redirect(w, r, "/orgs/"+org.Slug+"/years/"+yearID+"/events/"+eventID, http.StatusSeeOther)
	})(w, r)
}

// ── Budget lines ──────────────────────────────────────────────────────────────

func (h *Handler) OrgBudgetLineCreate(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")
		eventID := r.PathValue("event_id")

		ev, err := h.store.getEvent(ctx, eventID, org.ID)
		if err != nil {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}
		if ev.Status == EventApproved {
			http.Error(w, "cannot modify approved event budget", http.StatusConflict)
			return
		}

		amtStr := r.FormValue("amount")
		amtFloat, err := parseEuroAmount(amtStr)
		if err != nil {
			http.Error(w, "invalid amount", http.StatusBadRequest)
			return
		}
		lineType := BudgetLineType(r.FormValue("type"))
		if lineType != BudgetIncome {
			lineType = BudgetExpense
		}
		category := strings.TrimSpace(r.FormValue("category"))
		if category == "" {
			category = "General"
		}

		line := &BudgetLine{
			ID:           bson.NewObjectID().Hex(),
			EventID:      eventID,
			OrgID:        org.ID,
			Category:     category,
			Type:         lineType,
			PlannedCents: amtFloat,
			Description:  strings.TrimSpace(r.FormValue("description")),
			CreatedAt:    time.Now(),
		}
		if err := h.store.createBudgetLine(ctx, line); err != nil {
			slog.Error("create budget line", "err", err)
			http.Error(w, "could not create budget line", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/years/"+yearID+"/events/"+eventID, http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgBudgetLineDelete(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")
		eventID := r.PathValue("event_id")
		lineID := r.PathValue("line_id")

		ev, err := h.store.getEvent(ctx, eventID, org.ID)
		if err != nil {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}
		if ev.Status == EventApproved {
			http.Error(w, "cannot modify approved event budget", http.StatusConflict)
			return
		}
		_ = h.store.deleteBudgetLine(ctx, lineID, org.ID)
		http.Redirect(w, r, "/orgs/"+org.Slug+"/years/"+yearID+"/events/"+eventID, http.StatusSeeOther)
	})(w, r)
}

// ── Transaction Requests ──────────────────────────────────────────────────────

func (h *Handler) OrgRequestList(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		statusFilter := r.URL.Query().Get("status")

		filter := bson.M{"org_id": org.ID}
		if statusFilter != "" {
			filter["status_log"] = bson.M{"$elemMatch": bson.M{"status": statusFilter}}
		}
		// Guest team members only see their team's requests
		if len(me.TeamIDs) > 0 {
			teams, _ := h.store.getTeams(ctx, org.ID)
			guestOnly := true
			for _, t := range teams {
				for _, tid := range me.TeamIDs {
					if t.ID == tid && t.Type == TeamTypeInternal {
						guestOnly = false
					}
				}
			}
			if guestOnly {
				filter["team_id"] = bson.M{"$in": me.TeamIDs}
			}
		}

		requests, _ := h.store.getTxRequests(ctx, org.ID, filter)
		events, _ := h.store.getEvents(ctx, org.ID, "")
		teams, _ := h.store.getTeams(ctx, org.ID)

		render(w, orgRequestsTmpl, &OrgRequestsData{
			UserID:       r.Header.Get("X-Auth-User-Id"),
			Email:        r.Header.Get("X-Auth-Email"),
			Title:        org.Name + " — Requests",
			Route:        "orgs",
			Org:          *org,
			MyRole:       me.Role,
			Requests:     requests,
			Events:       events,
			Teams:        teams,
			StatusFilter: statusFilter,
		})
	})(w, r)
}

func (h *Handler) OrgRequestNew(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()

		// only in active fiscal year
		activeYear, err := h.store.getActiveFiscalYear(ctx, org.ID)
		if err != nil || activeYear == nil {
			http.Error(w, "no active fiscal year — requests can only be submitted during an active year", http.StatusConflict)
			return
		}

		events, _ := h.store.getEvents(ctx, org.ID, activeYear.ID)
		teams, _ := h.store.getTeams(ctx, org.ID)

		if r.Method == http.MethodGet {
			render(w, orgRequestDetailTmpl, &OrgRequestDetailData{
				UserID:     r.Header.Get("X-Auth-User-Id"),
				Email:      r.Header.Get("X-Auth-Email"),
				Title:      "New Request",
				Route:      "orgs",
				Org:        *org,
				MyRole:     me.Role,
				FiscalYear: activeYear,
				// Use Request.ID="" to signal "new form" in template
			})
			_ = events
			_ = teams
			return
		}

		txType := TxRequestType(r.FormValue("type"))
		switch txType {
		case TxReimbursement, TxPurchaseOrder, TxCashAdvance, TxIncome, TxBudgetTransfer:
		default:
			http.Error(w, "invalid request type", http.StatusBadRequest)
			return
		}

		amtCents, err := parseEuroAmount(r.FormValue("amount"))
		if err != nil {
			http.Error(w, "invalid amount", http.StatusBadRequest)
			return
		}

		req := &TxRequest{
			ID:             bson.NewObjectID().Hex(),
			OrgID:          org.ID,
			FiscalYearID:   activeYear.ID,
			EventID:        r.FormValue("event_id"),
			BudgetLineID:   r.FormValue("budget_line_id"),
			TeamID:         r.FormValue("team_id"),
			SubmittedBy:    me.ID,
			SubmitterEmail: me.Email,
			Type:           txType,
			Description:    strings.TrimSpace(r.FormValue("description")),
			AmountCents:    amtCents,
			Vendor:         strings.TrimSpace(r.FormValue("vendor")),
			PayerName:      strings.TrimSpace(r.FormValue("payer_name")),
			PaymentMethod:  r.FormValue("payment_method"),
			AttachmentIDs:  []string{},
			StatusLog: []StatusLogEntry{{
				Status:    TxDraft,
				ChangedBy: me.ID,
				ChangedAt: time.Now(),
			}},
			CreatedAt: time.Now(),
		}
		if dueDateStr := r.FormValue("due_date"); dueDateStr != "" {
			if d, err := time.Parse("2006-01-02", dueDateStr); err == nil {
				req.DueDate = d
			}
		}
		if txType == TxBudgetTransfer {
			req.FromBudgetLineID = r.FormValue("from_budget_line_id")
			req.ToBudgetLineID = r.FormValue("to_budget_line_id")
		}

		if err := h.store.createTxRequest(ctx, req); err != nil {
			slog.Error("create tx request", "err", err)
			http.Error(w, "could not create request", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug+"/requests/"+req.ID, http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgRequestDetail(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		reqID := r.PathValue("req_id")

		req, err := h.store.getTxRequest(ctx, reqID, org.ID)
		if err != nil {
			http.Error(w, "request not found", http.StatusNotFound)
			return
		}

		var ev *OrgEvent
		var bl *BudgetLine
		var team *OrgTeam
		var fy *FiscalYear

		if req.EventID != "" {
			ev, _ = h.store.getEvent(ctx, req.EventID, org.ID)
		}
		if req.FiscalYearID != "" {
			fy, _ = h.store.getFiscalYear(ctx, req.FiscalYearID, org.ID)
		}
		if req.BudgetLineID != "" {
			lines, _ := h.store.getBudgetLines(ctx, req.EventID, org.ID)
			for i := range lines {
				if lines[i].ID == req.BudgetLineID {
					bl = &lines[i]
					break
				}
			}
		}
		if req.TeamID != "" {
			t, _ := h.store.getTeam(ctx, req.TeamID, org.ID)
			team = t
		}
		attachments, _ := h.store.getAttachments(ctx, reqID, org.ID)

		render(w, orgRequestDetailTmpl, &OrgRequestDetailData{
			UserID:      r.Header.Get("X-Auth-User-Id"),
			Email:       r.Header.Get("X-Auth-Email"),
			Title:       string(req.Type) + " Request",
			Route:       "orgs",
			Org:         *org,
			MyRole:      me.Role,
			Request:     *req,
			Event:       ev,
			BudgetLine:  bl,
			Team:        team,
			FiscalYear:  fy,
			Attachments: attachments,
		})
	})(w, r)
}

func (h *Handler) OrgRequestAction(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		reqID := r.PathValue("req_id")
		action := r.FormValue("action")
		comment := strings.TrimSpace(r.FormValue("comment"))

		req, err := h.store.getTxRequest(ctx, reqID, org.ID)
		if err != nil {
			http.Error(w, "request not found", http.StatusNotFound)
			return
		}
		current := req.CurrentStatus()

		canManage := me.Role == OrgRoleAdmin || me.Role == OrgRoleFinance

		var newStatus TxRequestStatus
		switch action {
		case "submit":
			if current != TxDraft {
				http.Error(w, "only draft requests can be submitted", http.StatusConflict)
				return
			}
			newStatus = TxSubmitted
		case "request_info":
			if !canManage {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			if comment == "" {
				http.Error(w, "comment required when requesting information", http.StatusBadRequest)
				return
			}
			newStatus = TxInfoRequested
		case "review":
			if !canManage {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			newStatus = TxUnderReview
		case "approve":
			if !canManage {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			newStatus = TxApproved
		case "reject":
			if !canManage {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			newStatus = TxRejected
		case "cancel":
			if current != TxDraft && current != TxSubmitted && current != TxInfoRequested {
				http.Error(w, "cannot cancel in current status", http.StatusConflict)
				return
			}
			newStatus = TxCancelled
		case "mark_paid":
			if !canManage || req.Type != TxReimbursement {
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
			newStatus = TxPaid
		case "mark_ordered":
			if !canManage || req.Type != TxPurchaseOrder {
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
			newStatus = TxOrdered
		case "mark_delivered":
			if req.Type != TxPurchaseOrder {
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
			newStatus = TxDelivered
		case "dispute":
			if !canManage || req.Type != TxPurchaseOrder {
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
			newStatus = TxDisputed
		case "disburse":
			if !canManage || req.Type != TxCashAdvance {
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
			newStatus = TxDisbursed
		case "settlement_due":
			if !canManage || req.Type != TxCashAdvance {
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
			newStatus = TxSettlementDue
		case "mark_pending_payment":
			if !canManage || req.Type != TxIncome {
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
			newStatus = TxPendingPayment
		case "mark_received":
			if !canManage || req.Type != TxIncome {
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
			newStatus = TxReceived
		case "reconcile":
			if !canManage {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			newStatus = TxReconciled
		case "done":
			if !canManage || req.Type != TxBudgetTransfer {
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
			newStatus = TxDone
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}

		entry := StatusLogEntry{
			Status:    newStatus,
			ChangedBy: me.ID,
			ChangedAt: time.Now(),
			Comment:   comment,
		}
		if err := h.store.appendStatusLog(ctx, reqID, org.ID, entry); err != nil {
			slog.Error("append status log", "err", err)
			http.Error(w, "could not update request status", http.StatusInternalServerError)
			return
		}

		// when approved, create a ledger entry
		if newStatus == TxApproved {
			ledger := &OrgLedgerEntry{
				ID:           bson.NewObjectID().Hex(),
				OrgID:        org.ID,
				FiscalYearID: req.FiscalYearID,
				EventID:      req.EventID,
				BudgetLineID: req.BudgetLineID,
				TeamID:       req.TeamID,
				RequestID:    req.ID,
				AmountCents:  req.AmountCents,
				Description:  req.Description,
				Date:         time.Now(),
				CreatedAt:    time.Now(),
			}
			if err := h.store.createLedgerEntry(ctx, ledger); err != nil {
				slog.Error("create ledger entry", "err", err)
			}
		}

		http.Redirect(w, r, "/orgs/"+org.Slug+"/requests/"+reqID, http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgRequestDelivery(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		reqID := r.PathValue("req_id")

		req, err := h.store.getTxRequest(ctx, reqID, org.ID)
		if err != nil || req.Type != TxPurchaseOrder {
			http.Error(w, "purchase order not found", http.StatusNotFound)
			return
		}
		if req.CurrentStatus() != TxOrdered {
			http.Error(w, "delivery can only be recorded on ordered requests", http.StatusConflict)
			return
		}
		amtCents, err := parseEuroAmount(r.FormValue("actual_amount"))
		if err != nil {
			http.Error(w, "invalid amount", http.StatusBadRequest)
			return
		}
		delivery := &PODelivery{
			ActualAmountCents: amtCents,
			ActualVendor:      strings.TrimSpace(r.FormValue("actual_vendor")),
			DeliveredAt:       time.Now(),
			StoreChanged:      r.FormValue("store_changed") == "true",
			ChangeNote:        strings.TrimSpace(r.FormValue("change_note")),
		}
		update := bson.M{"delivery": delivery}
		if err := h.store.updateTxRequest(ctx, reqID, org.ID, update); err != nil {
			slog.Error("record delivery", "err", err)
			http.Error(w, "could not record delivery", http.StatusInternalServerError)
			return
		}
		entry := StatusLogEntry{Status: TxDelivered, ChangedBy: me.ID, ChangedAt: time.Now()}
		_ = h.store.appendStatusLog(ctx, reqID, org.ID, entry)
		http.Redirect(w, r, "/orgs/"+org.Slug+"/requests/"+reqID, http.StatusSeeOther)
	})(w, r)
}

func (h *Handler) OrgRequestSettle(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		reqID := r.PathValue("req_id")

		req, err := h.store.getTxRequest(ctx, reqID, org.ID)
		if err != nil || req.Type != TxCashAdvance {
			http.Error(w, "cash advance not found", http.StatusNotFound)
			return
		}
		cs := req.CurrentStatus()
		if cs != TxDisbursed && cs != TxSettlementDue && cs != TxPartialSettlement {
			http.Error(w, "cannot settle in current status", http.StatusConflict)
			return
		}
		spentCents, err := parseEuroAmount(r.FormValue("amount_spent"))
		if err != nil {
			http.Error(w, "invalid spent amount", http.StatusBadRequest)
			return
		}
		returnedCents, err := parseEuroAmount(r.FormValue("amount_returned"))
		if err != nil {
			returnedCents = 0
		}
		settlement := &CashSettlement{
			AmountSpentCents:    spentCents,
			AmountReturnedCents: returnedCents,
			SettledAt:           time.Now(),
		}
		update := bson.M{"settlement": settlement}
		if err := h.store.updateTxRequest(ctx, reqID, org.ID, update); err != nil {
			slog.Error("record settlement", "err", err)
			http.Error(w, "could not record settlement", http.StatusInternalServerError)
			return
		}

		newStatus := TxSettled
		if returnedCents < (req.AmountCents - spentCents) {
			newStatus = TxPartialSettlement
		}
		entry := StatusLogEntry{Status: newStatus, ChangedBy: me.ID, ChangedAt: time.Now()}
		_ = h.store.appendStatusLog(ctx, reqID, org.ID, entry)
		http.Redirect(w, r, "/orgs/"+org.Slug+"/requests/"+reqID, http.StatusSeeOther)
	})(w, r)
}

// ── Ledger ────────────────────────────────────────────────────────────────────

func (h *Handler) OrgLedger(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin, OrgRoleFinance)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.URL.Query().Get("year_id")

		years, _ := h.store.getFiscalYears(ctx, org.ID)
		var activeYear *FiscalYear
		for i := range years {
			if years[i].Status == FiscalYearActive {
				activeYear = &years[i]
			}
		}
		if yearID == "" && activeYear != nil {
			yearID = activeYear.ID
		}

		var fy *FiscalYear
		for i := range years {
			if years[i].ID == yearID {
				fy = &years[i]
				break
			}
		}

		entries, _ := h.store.getLedgerEntries(ctx, org.ID, yearID, bson.M{})
		events, _ := h.store.getEvents(ctx, org.ID, yearID)
		teams, _ := h.store.getTeams(ctx, org.ID)

		evMap := make(map[string]OrgEvent, len(events))
		for _, e := range events {
			evMap[e.ID] = e
		}
		teamMap := make(map[string]OrgTeam, len(teams))
		for _, t := range teams {
			teamMap[t.ID] = t
		}

		var inc, exp int64
		for _, e := range entries {
			if e.AmountCents >= 0 {
				inc += e.AmountCents
			} else {
				exp += -e.AmountCents
			}
		}

		render(w, orgLedgerTmpl, &OrgLedgerData{
			UserID:       r.Header.Get("X-Auth-User-Id"),
			Email:        r.Header.Get("X-Auth-Email"),
			Title:        org.Name + " — Ledger",
			Route:        "orgs",
			Org:          *org,
			MyRole:       me.Role,
			FiscalYear:   fy,
			FiscalYears:  years,
			Entries:      entries,
			Events:       evMap,
			Teams:        teamMap,
			TotalIncome:  inc,
			TotalExpense: exp,
		})
	})(w, r)
}

func (h *Handler) OrgBankImport(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin, OrgRoleFinance)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()

		activeYear, _ := h.store.getActiveFiscalYear(ctx, org.ID)

		if r.Method == http.MethodGet {
			render(w, orgBankImportTmpl, &OrgBankImportData{
				UserID:     r.Header.Get("X-Auth-User-Id"),
				Email:      r.Header.Get("X-Auth-Email"),
				Title:      org.Name + " — Bank Import",
				Route:      "orgs",
				Org:        *org,
				MyRole:     me.Role,
				FiscalYear: activeYear,
			})
			return
		}

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "could not parse form", http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("csv")
		if err != nil {
			http.Error(w, "csv file required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		rows, err := parseBankCSV(file)
		if err != nil {
			render(w, orgBankImportTmpl, &OrgBankImportData{
				UserID: r.Header.Get("X-Auth-User-Id"), Email: r.Header.Get("X-Auth-Email"),
				Title: org.Name + " — Bank Import", Route: "orgs",
				Org: *org, MyRole: me.Role, FiscalYear: activeYear,
				Error: "could not parse CSV: " + err.Error(),
			})
			return
		}

		if r.FormValue("confirm") != "1" {
			// preview mode
			render(w, orgBankImportTmpl, &OrgBankImportData{
				UserID: r.Header.Get("X-Auth-User-Id"), Email: r.Header.Get("X-Auth-Email"),
				Title: org.Name + " — Bank Import", Route: "orgs",
				Org: *org, MyRole: me.Role, FiscalYear: activeYear,
				Rows: rows,
			})
			return
		}

		// import confirmed
		fy := activeYear
		if fy == nil {
			http.Error(w, "no active fiscal year", http.StatusConflict)
			return
		}
		imported := 0
		for _, row := range rows {
			d, err := time.Parse("2006-01-02", row.Date)
			if err != nil {
				d = time.Now()
			}
			entry := &OrgLedgerEntry{
				ID:           bson.NewObjectID().Hex(),
				OrgID:        org.ID,
				FiscalYearID: fy.ID,
				AmountCents:  row.AmountCents,
				Description:  row.Description,
				BankRef:      row.Reference,
				Date:         d,
				CreatedAt:    time.Now(),
			}
			if err := h.store.createLedgerEntry(ctx, entry); err == nil {
				imported++
			}
		}
		render(w, orgBankImportTmpl, &OrgBankImportData{
			UserID: r.Header.Get("X-Auth-User-Id"), Email: r.Header.Get("X-Auth-Email"),
			Title: org.Name + " — Bank Import", Route: "orgs",
			Org: *org, MyRole: me.Role, FiscalYear: fy,
			Imported: imported,
		})
	})(w, r)
}

// ── Plan vs actual analysis ───────────────────────────────────────────────────

func (h *Handler) OrgAnalysis(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")

		year, err := h.store.getFiscalYear(ctx, yearID, org.ID)
		if err != nil {
			http.Error(w, "fiscal year not found", http.StatusNotFound)
			return
		}
		years, _ := h.store.getFiscalYears(ctx, org.ID)
		events, _ := h.store.getEvents(ctx, org.ID, yearID)
		teams, _ := h.store.getTeams(ctx, org.ID)
		entries, _ := h.store.getLedgerEntries(ctx, org.ID, yearID, bson.M{})

		// build actual maps
		actualByEvent := make(map[string]int64)
		actualByTeam := make(map[string]int64)
		for _, e := range entries {
			actualByEvent[e.EventID] += e.AmountCents
			actualByTeam[e.TeamID] += e.AmountCents
		}

		teamMap := make(map[string]OrgTeam, len(teams))
		for _, t := range teams {
			teamMap[t.ID] = t
		}

		var totalPI, totalAI, totalPE, totalAE int64

		eventRows := make([]AnalysisEventRow, 0, len(events))
		for _, ev := range events {
			lines, _ := h.store.getBudgetLines(ctx, ev.ID, org.ID)
			var pi, pe int64
			for _, l := range lines {
				if l.Type == BudgetIncome {
					pi += l.PlannedCents
				} else {
					pe += l.PlannedCents
				}
			}
			actual := actualByEvent[ev.ID]
			var ai, ae int64
			if actual >= 0 {
				ai = actual
			} else {
				ae = -actual
			}
			totalPI += pi
			totalPE += pe
			totalAI += ai
			totalAE += ae
			eventRows = append(eventRows, AnalysisEventRow{
				Event: ev, PlannedIncome: pi, ActualIncome: ai,
				PlannedExpense: pe, ActualExpense: ae,
			})
		}

		// team rows — aggregate from events by team membership
		teamActual := make(map[string]struct{ pi, pe, ai, ae int64 })
		for _, ev := range events {
			lines, _ := h.store.getBudgetLines(ctx, ev.ID, org.ID)
			var pi, pe int64
			for _, l := range lines {
				if l.Type == BudgetIncome {
					pi += l.PlannedCents
				} else {
					pe += l.PlannedCents
				}
			}
			actual := actualByTeam[ev.ID]
			var ai, ae int64
			if actual >= 0 {
				ai = actual
			} else {
				ae = -actual
			}
			for _, tid := range ev.TeamIDs {
				a := teamActual[tid]
				a.pi += pi / int64(max(len(ev.TeamIDs), 1))
				a.pe += pe / int64(max(len(ev.TeamIDs), 1))
				a.ai += ai / int64(max(len(ev.TeamIDs), 1))
				a.ae += ae / int64(max(len(ev.TeamIDs), 1))
				teamActual[tid] = a
			}
		}
		teamRows := make([]AnalysisTeamRow, 0, len(teams))
		for _, t := range teams {
			a := teamActual[t.ID]
			teamRows = append(teamRows, AnalysisTeamRow{
				Team: t, PlannedIncome: a.pi, ActualIncome: a.ai,
				PlannedExpense: a.pe, ActualExpense: a.ae,
			})
		}

		render(w, orgAnalysisTmpl, &OrgAnalysisData{
			UserID: r.Header.Get("X-Auth-User-Id"), Email: r.Header.Get("X-Auth-Email"),
			Title: org.Name + " — Analysis", Route: "orgs",
			Org: *org, MyRole: me.Role, FiscalYear: *year, FiscalYears: years,
			EventRows: eventRows, TeamRows: teamRows,
			TotalPlannedIncome: totalPI, TotalActualIncome: totalAI,
			TotalPlannedExpense: totalPE, TotalActualExpense: totalAE,
		})
	})(w, r)
}

// ── Year-end report ───────────────────────────────────────────────────────────

func (h *Handler) OrgReport(w http.ResponseWriter, r *http.Request) {
	h.requireOrgMember(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")

		year, err := h.store.getFiscalYear(ctx, yearID, org.ID)
		if err != nil {
			http.Error(w, "fiscal year not found", http.StatusNotFound)
			return
		}
		years, _ := h.store.getFiscalYears(ctx, org.ID)
		events, _ := h.store.getEvents(ctx, org.ID, yearID)
		teams, _ := h.store.getTeams(ctx, org.ID)
		entries, _ := h.store.getLedgerEntries(ctx, org.ID, yearID, bson.M{})

		teamMap := make(map[string]OrgTeam, len(teams))
		for _, t := range teams {
			teamMap[t.ID] = t
		}
		actualByEvent := make(map[string]int64)
		for _, e := range entries {
			actualByEvent[e.EventID] += e.AmountCents
		}

		var totalPI, totalAI, totalPE, totalAE int64

		eventReports := make([]EventReport, 0, len(events))
		for _, ev := range events {
			lines, _ := h.store.getBudgetLines(ctx, ev.ID, org.ID)
			comments, _ := h.store.getEventComments(ctx, ev.ID, org.ID)

			var pi, pe int64
			for _, l := range lines {
				if l.Type == BudgetIncome {
					pi += l.PlannedCents
				} else {
					pe += l.PlannedCents
				}
			}
			actual := actualByEvent[ev.ID]
			var ai, ae int64
			if actual >= 0 {
				ai = actual
			} else {
				ae = -actual
			}
			totalPI += pi
			totalPE += pe
			totalAI += ai
			totalAE += ae

			feedbackComments := make([]EventComment, 0)
			for _, c := range comments {
				if c.Kind == CommentFeedback {
					feedbackComments = append(feedbackComments, c)
				}
			}
			evTeams := make([]OrgTeam, 0)
			for _, tid := range ev.TeamIDs {
				if t, ok := teamMap[tid]; ok {
					evTeams = append(evTeams, t)
				}
			}
			eventReports = append(eventReports, EventReport{
				Event: ev, BudgetLines: lines, Comments: feedbackComments,
				PlannedIncome: pi, ActualIncome: ai,
				PlannedExpense: pe, ActualExpense: ae,
				Teams: evTeams,
			})
		}

		render(w, orgReportTmpl, &OrgReportData{
			UserID: r.Header.Get("X-Auth-User-Id"), Email: r.Header.Get("X-Auth-Email"),
			Title: org.Name + " — " + year.Label + " Report", Route: "orgs",
			Org: *org, MyRole: me.Role, FiscalYear: *year, FiscalYears: years,
			EventReports:        eventReports,
			TotalPlannedIncome:  totalPI, TotalActualIncome: totalAI,
			TotalPlannedExpense: totalPE, TotalActualExpense: totalAE,
		})
	})(w, r)
}

// ── Fiscal year close ─────────────────────────────────────────────────────────

func (h *Handler) OrgFiscalYearClose(w http.ResponseWriter, r *http.Request) {
	h.requireOrgRole(OrgRoleAdmin)(func(w http.ResponseWriter, r *http.Request, org *Org, me *OrgMember) {
		ctx := r.Context()
		yearID := r.PathValue("year_id")
		if err := h.store.updateFiscalYearStatus(ctx, yearID, org.ID, FiscalYearClosed, bson.M{
			"closed_at": time.Now(),
		}); err != nil {
			slog.Error("close fiscal year", "err", err)
			http.Error(w, "could not close fiscal year", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/orgs/"+org.Slug, http.StatusSeeOther)
	})(w, r)
}

// ── Route registration ────────────────────────────────────────────────────────

func (h *Handler) RegisterOrgRoutes(mux *http.ServeMux) {
	// Exact/literal patterns first (must precede wildcard {slug} routes)
	mux.HandleFunc("GET /orgs",                                  h.authMW(h.OrgList))
	mux.HandleFunc("GET /orgs/new",                              h.authMW(h.OrgCreate))
	mux.HandleFunc("POST /orgs/new",                             h.authMW(h.OrgCreate))
	mux.HandleFunc("GET /join/{token}",                          h.authMW(h.OrgJoin))
	mux.HandleFunc("POST /join/{token}",                         h.authMW(h.OrgJoin))

	// {slug} wildcard routes
	mux.HandleFunc("GET /orgs/{slug}",                           h.OrgHome)

	// Teams
	mux.HandleFunc("GET /orgs/{slug}/teams",                     h.OrgTeams)
	mux.HandleFunc("POST /orgs/{slug}/teams",                    h.OrgTeamCreate)
	mux.HandleFunc("POST /orgs/{slug}/teams/{team_id}/delete",   h.OrgTeamDelete)

	// Members
	mux.HandleFunc("GET /orgs/{slug}/members",                   h.OrgMembers)
	mux.HandleFunc("POST /orgs/{slug}/members/{member_id}/role", h.OrgMemberRoleUpdate)
	mux.HandleFunc("POST /orgs/{slug}/members/{member_id}/remove", h.OrgMemberRemove)

	// Invites
	mux.HandleFunc("GET /orgs/{slug}/invite",                    h.OrgInviteNew)
	mux.HandleFunc("POST /orgs/{slug}/invite",                   h.OrgInviteNew)
	mux.HandleFunc("POST /orgs/{slug}/invites/{invite_id}/revoke", h.OrgInviteRevoke)

	// Fiscal years
	mux.HandleFunc("POST /orgs/{slug}/years",                    h.OrgFiscalYearCreate)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/activate", h.OrgFiscalYearActivate)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/close",    h.OrgFiscalYearClose)

	// Events (literal "new" before {event_id} wildcard)
	mux.HandleFunc("GET /orgs/{slug}/years/{year_id}/events",                                     h.OrgEventList)
	mux.HandleFunc("GET /orgs/{slug}/years/{year_id}/events/new",                                 h.OrgEventNew)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/events/new",                                h.OrgEventNew)
	mux.HandleFunc("GET /orgs/{slug}/years/{year_id}/events/{event_id}",                          h.OrgEventDetail)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/events/{event_id}/edit",                    h.OrgEventEdit)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/events/{event_id}/delete",                  h.OrgEventDelete)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/events/{event_id}/submit",                  h.OrgEventSubmit)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/events/{event_id}/review",                  h.OrgEventReview)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/events/{event_id}/feedback",                h.OrgEventFeedback)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/events/{event_id}/budget",                  h.OrgBudgetLineCreate)
	mux.HandleFunc("POST /orgs/{slug}/years/{year_id}/events/{event_id}/budget/{line_id}/delete", h.OrgBudgetLineDelete)

	// Analysis & report
	mux.HandleFunc("GET /orgs/{slug}/years/{year_id}/analysis", h.OrgAnalysis)
	mux.HandleFunc("GET /orgs/{slug}/years/{year_id}/report",   h.OrgReport)

	// Transaction requests (literal "new" before {req_id} wildcard)
	mux.HandleFunc("GET /orgs/{slug}/requests",                        h.OrgRequestList)
	mux.HandleFunc("GET /orgs/{slug}/requests/new",                    h.OrgRequestNew)
	mux.HandleFunc("POST /orgs/{slug}/requests/new",                   h.OrgRequestNew)
	mux.HandleFunc("GET /orgs/{slug}/requests/{req_id}",               h.OrgRequestDetail)
	mux.HandleFunc("POST /orgs/{slug}/requests/{req_id}/action",       h.OrgRequestAction)
	mux.HandleFunc("POST /orgs/{slug}/requests/{req_id}/delivery",     h.OrgRequestDelivery)
	mux.HandleFunc("POST /orgs/{slug}/requests/{req_id}/settle",       h.OrgRequestSettle)

	// Ledger (literal "import" before potential wildcards)
	mux.HandleFunc("GET /orgs/{slug}/ledger",         h.OrgLedger)
	mux.HandleFunc("GET /orgs/{slug}/ledger/import",  h.OrgBankImport)
	mux.HandleFunc("POST /orgs/{slug}/ledger/import", h.OrgBankImport)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// parseEuroAmount converts a user-entered decimal string (e.g. "12.50") to cents.
func parseEuroAmount(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", ".")
	var euros, cents int64
	parts := strings.SplitN(s, ".", 2)
	var err error
	if euros, err = parseInt64(parts[0]); err != nil {
		return 0, fmt.Errorf("invalid amount")
	}
	if len(parts) == 2 {
		centStr := parts[1]
		if len(centStr) == 1 {
			centStr += "0"
		} else if len(centStr) > 2 {
			centStr = centStr[:2]
		}
		if cents, err = parseInt64(centStr); err != nil {
			return 0, fmt.Errorf("invalid amount")
		}
	}
	return euros*100 + cents, nil
}

func parseInt64(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	var n int64
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func csvReader(r io.Reader) ([][]string, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	return cr.ReadAll()
}

// parseBankCSV reads a bank statement CSV. It expects at minimum columns:
// date, description, amount (and optionally reference).
// Negative amounts are expenses, positive are income.
func parseBankCSV(r io.Reader) ([]BankImportRow, error) {
	records, err := csvReader(r)
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("CSV is empty")
	}
	header := records[0]
	idx := func(name string) int {
		for i, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return i
			}
		}
		return -1
	}
	dateIdx := idx("date")
	descIdx := idx("description")
	amtIdx := idx("amount")
	refIdx := idx("reference")
	if dateIdx < 0 || descIdx < 0 || amtIdx < 0 {
		return nil, fmt.Errorf("CSV must have columns: date, description, amount")
	}

	rows := make([]BankImportRow, 0, len(records)-1)
	for _, rec := range records[1:] {
		if len(rec) <= amtIdx {
			continue
		}
		amt, err := parseEuroAmount(rec[amtIdx])
		if err != nil {
			continue
		}
		ref := ""
		if refIdx >= 0 && refIdx < len(rec) {
			ref = strings.TrimSpace(rec[refIdx])
		}
		rows = append(rows, BankImportRow{
			Date:        strings.TrimSpace(rec[dateIdx]),
			Description: strings.TrimSpace(rec[descIdx]),
			AmountCents: amt,
			Reference:   ref,
		})
	}
	return rows, nil
}

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

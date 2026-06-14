package main

import (
	"context"
	"fmt"
	"time"

	"homelab/pkg/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	mgmongo "go.mongodb.org/mongo-driver/v2/mongo"
)

// ── Collection helpers ────────────────────────────────────────────────────────

func (s *Store) orgs() *mgmongo.Collection {
	return s.db.Collection("org_organizations")
}

func (s *Store) orgTeams() *mgmongo.Collection {
	return s.db.Collection("org_teams")
}

func (s *Store) orgMembers() *mgmongo.Collection {
	return s.db.Collection("org_members")
}

func (s *Store) orgInvites() *mgmongo.Collection {
	return s.db.Collection("org_invites")
}

func (s *Store) fiscalYears() *mgmongo.Collection {
	return s.db.Collection("org_fiscal_years")
}

func (s *Store) orgEvents() *mgmongo.Collection {
	return s.db.Collection("org_events")
}

func (s *Store) budgetLines() *mgmongo.Collection {
	return s.db.Collection("org_budget_lines")
}

func (s *Store) eventComments() *mgmongo.Collection {
	return s.db.Collection("org_event_comments")
}

func (s *Store) txRequests() *mgmongo.Collection {
	return s.db.Collection("org_tx_requests")
}

func (s *Store) orgLedger() *mgmongo.Collection {
	return s.db.Collection("org_ledger")
}

func (s *Store) orgAttachments() *mgmongo.Collection {
	return s.db.Collection("org_attachments")
}

// ── Organizations ─────────────────────────────────────────────────────────────

func (s *Store) getOrgsForUser(ctx context.Context, userID string) ([]OrgWithRole, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getOrgsForUser")
	defer span.End()

	cur, err := s.orgMembers().Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("find memberships: %w", err)
	}
	defer cur.Close(ctx)

	var members []OrgMember
	if err := cur.All(ctx, &members); err != nil {
		return nil, fmt.Errorf("decode memberships: %w", err)
	}

	if len(members) == 0 {
		return nil, nil
	}

	orgIDs := make([]string, len(members))
	roleByOrg := make(map[string]OrgRole, len(members))
	for i, m := range members {
		orgIDs[i] = m.OrgID
		roleByOrg[m.OrgID] = m.Role
	}

	cur2, err := s.orgs().Find(ctx, bson.M{"_id": bson.M{"$in": orgIDs}})
	if err != nil {
		return nil, fmt.Errorf("find orgs: %w", err)
	}
	defer cur2.Close(ctx)

	var orgs []Org
	if err := cur2.All(ctx, &orgs); err != nil {
		return nil, fmt.Errorf("decode orgs: %w", err)
	}

	result := make([]OrgWithRole, len(orgs))
	for i, o := range orgs {
		result[i] = OrgWithRole{Org: o, Role: roleByOrg[o.ID]}
	}
	return result, nil
}

func (s *Store) getOrg(ctx context.Context, orgID string) (*Org, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getOrg")
	defer span.End()
	var o Org
	if err := s.orgs().FindOne(ctx, bson.M{"_id": orgID}).Decode(&o); err != nil {
		return nil, fmt.Errorf("find org: %w", err)
	}
	return &o, nil
}

func (s *Store) getOrgBySlug(ctx context.Context, slug string) (*Org, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getOrgBySlug")
	defer span.End()
	var o Org
	if err := s.orgs().FindOne(ctx, bson.M{"slug": slug}).Decode(&o); err != nil {
		return nil, fmt.Errorf("find org by slug: %w", err)
	}
	return &o, nil
}

func (s *Store) createOrg(ctx context.Context, o *Org) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createOrg")
	defer span.End()
	_, err := s.orgs().InsertOne(ctx, o)
	return err
}

func (s *Store) slugExists(ctx context.Context, slug string) (bool, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.slugExists")
	defer span.End()
	n, err := s.orgs().CountDocuments(ctx, bson.M{"slug": slug})
	return n > 0, err
}

// ── Teams ─────────────────────────────────────────────────────────────────────

func (s *Store) getTeams(ctx context.Context, orgID string) ([]OrgTeam, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getTeams")
	defer span.End()
	opts := options.Find().SetSort(bson.M{"name": 1})
	cur, err := s.orgTeams().Find(ctx, bson.M{"org_id": orgID}, opts)
	if err != nil {
		return nil, fmt.Errorf("find teams: %w", err)
	}
	defer cur.Close(ctx)
	var teams []OrgTeam
	if err := cur.All(ctx, &teams); err != nil {
		return nil, fmt.Errorf("decode teams: %w", err)
	}
	return teams, nil
}

func (s *Store) getTeam(ctx context.Context, teamID, orgID string) (*OrgTeam, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getTeam")
	defer span.End()
	var t OrgTeam
	if err := s.orgTeams().FindOne(ctx, bson.M{"_id": teamID, "org_id": orgID}).Decode(&t); err != nil {
		return nil, fmt.Errorf("find team: %w", err)
	}
	return &t, nil
}

func (s *Store) createTeam(ctx context.Context, t *OrgTeam) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createTeam")
	defer span.End()
	_, err := s.orgTeams().InsertOne(ctx, t)
	return err
}

func (s *Store) deleteTeam(ctx context.Context, teamID, orgID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteTeam")
	defer span.End()
	_, err := s.orgTeams().DeleteOne(ctx, bson.M{"_id": teamID, "org_id": orgID})
	return err
}

// ── Members ───────────────────────────────────────────────────────────────────

func (s *Store) getMembers(ctx context.Context, orgID string) ([]OrgMember, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getMembers")
	defer span.End()
	opts := options.Find().SetSort(bson.M{"email": 1})
	cur, err := s.orgMembers().Find(ctx, bson.M{"org_id": orgID}, opts)
	if err != nil {
		return nil, fmt.Errorf("find members: %w", err)
	}
	defer cur.Close(ctx)
	var members []OrgMember
	if err := cur.All(ctx, &members); err != nil {
		return nil, fmt.Errorf("decode members: %w", err)
	}
	return members, nil
}

func (s *Store) getMember(ctx context.Context, orgID, userID string) (*OrgMember, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getMember")
	defer span.End()
	var m OrgMember
	if err := s.orgMembers().FindOne(ctx, bson.M{"org_id": orgID, "user_id": userID}).Decode(&m); err != nil {
		return nil, fmt.Errorf("find member: %w", err)
	}
	return &m, nil
}

func (s *Store) createMember(ctx context.Context, m *OrgMember) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createMember")
	defer span.End()
	_, err := s.orgMembers().InsertOne(ctx, m)
	return err
}

func (s *Store) updateMemberRole(ctx context.Context, memberID, orgID string, role OrgRole) error {
	ctx, span := mongo.StartSpan(ctx, "Store.updateMemberRole")
	defer span.End()
	_, err := s.orgMembers().UpdateOne(ctx,
		bson.M{"_id": memberID, "org_id": orgID},
		bson.M{"$set": bson.M{"role": role}},
	)
	return err
}

func (s *Store) removeMember(ctx context.Context, memberID, orgID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.removeMember")
	defer span.End()
	_, err := s.orgMembers().DeleteOne(ctx, bson.M{"_id": memberID, "org_id": orgID})
	return err
}

// ── Invites ───────────────────────────────────────────────────────────────────

func (s *Store) getInvites(ctx context.Context, orgID string) ([]OrgInvite, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getInvites")
	defer span.End()
	// only pending (not yet used, not expired)
	cur, err := s.orgInvites().Find(ctx, bson.M{
		"org_id":     orgID,
		"expires_at": bson.M{"$gt": time.Now()},
		"used_at":    bson.M{"$exists": false},
	})
	if err != nil {
		return nil, fmt.Errorf("find invites: %w", err)
	}
	defer cur.Close(ctx)
	var invites []OrgInvite
	if err := cur.All(ctx, &invites); err != nil {
		return nil, fmt.Errorf("decode invites: %w", err)
	}
	return invites, nil
}

func (s *Store) getInviteByToken(ctx context.Context, token string) (*OrgInvite, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getInviteByToken")
	defer span.End()
	var inv OrgInvite
	err := s.orgInvites().FindOne(ctx, bson.M{
		"token":      token,
		"expires_at": bson.M{"$gt": time.Now()},
		"used_at":    bson.M{"$exists": false},
	}).Decode(&inv)
	if err != nil {
		return nil, fmt.Errorf("find invite: %w", err)
	}
	return &inv, nil
}

func (s *Store) createInvite(ctx context.Context, inv *OrgInvite) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createInvite")
	defer span.End()
	_, err := s.orgInvites().InsertOne(ctx, inv)
	return err
}

func (s *Store) consumeInvite(ctx context.Context, inviteID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.consumeInvite")
	defer span.End()
	_, err := s.orgInvites().UpdateOne(ctx,
		bson.M{"_id": inviteID},
		bson.M{"$set": bson.M{"used_at": time.Now()}},
	)
	return err
}

func (s *Store) revokeInvite(ctx context.Context, inviteID, orgID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.revokeInvite")
	defer span.End()
	// set expiry to past
	_, err := s.orgInvites().UpdateOne(ctx,
		bson.M{"_id": inviteID, "org_id": orgID},
		bson.M{"$set": bson.M{"expires_at": time.Now().Add(-time.Second)}},
	)
	return err
}

// ── Fiscal Years ──────────────────────────────────────────────────────────────

func (s *Store) getFiscalYears(ctx context.Context, orgID string) ([]FiscalYear, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getFiscalYears")
	defer span.End()
	opts := options.Find().SetSort(bson.M{"start_date": -1})
	cur, err := s.fiscalYears().Find(ctx, bson.M{"org_id": orgID}, opts)
	if err != nil {
		return nil, fmt.Errorf("find fiscal years: %w", err)
	}
	defer cur.Close(ctx)
	var years []FiscalYear
	if err := cur.All(ctx, &years); err != nil {
		return nil, fmt.Errorf("decode fiscal years: %w", err)
	}
	return years, nil
}

func (s *Store) getFiscalYear(ctx context.Context, yearID, orgID string) (*FiscalYear, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getFiscalYear")
	defer span.End()
	var y FiscalYear
	if err := s.fiscalYears().FindOne(ctx, bson.M{"_id": yearID, "org_id": orgID}).Decode(&y); err != nil {
		return nil, fmt.Errorf("find fiscal year: %w", err)
	}
	return &y, nil
}

func (s *Store) getActiveFiscalYear(ctx context.Context, orgID string) (*FiscalYear, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getActiveFiscalYear")
	defer span.End()
	var y FiscalYear
	err := s.fiscalYears().FindOne(ctx, bson.M{"org_id": orgID, "status": FiscalYearActive}).Decode(&y)
	if err != nil {
		return nil, err
	}
	return &y, nil
}

func (s *Store) createFiscalYear(ctx context.Context, y *FiscalYear) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createFiscalYear")
	defer span.End()
	_, err := s.fiscalYears().InsertOne(ctx, y)
	return err
}

func (s *Store) updateFiscalYearStatus(ctx context.Context, yearID, orgID string, status FiscalYearStatus, extraSet bson.M) error {
	ctx, span := mongo.StartSpan(ctx, "Store.updateFiscalYearStatus")
	defer span.End()
	set := bson.M{"status": status}
	for k, v := range extraSet {
		set[k] = v
	}
	_, err := s.fiscalYears().UpdateOne(ctx,
		bson.M{"_id": yearID, "org_id": orgID},
		bson.M{"$set": set},
	)
	return err
}

// ── Events ────────────────────────────────────────────────────────────────────

func (s *Store) getEvents(ctx context.Context, orgID, fiscalYearID string) ([]OrgEvent, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getEvents")
	defer span.End()
	q := bson.M{"org_id": orgID}
	if fiscalYearID != "" {
		q["fiscal_year_id"] = fiscalYearID
	}
	opts := options.Find().SetSort(bson.M{"date_start": 1})
	cur, err := s.orgEvents().Find(ctx, q, opts)
	if err != nil {
		return nil, fmt.Errorf("find events: %w", err)
	}
	defer cur.Close(ctx)
	var events []OrgEvent
	if err := cur.All(ctx, &events); err != nil {
		return nil, fmt.Errorf("decode events: %w", err)
	}
	return events, nil
}

func (s *Store) getEvent(ctx context.Context, eventID, orgID string) (*OrgEvent, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getEvent")
	defer span.End()
	var e OrgEvent
	if err := s.orgEvents().FindOne(ctx, bson.M{"_id": eventID, "org_id": orgID}).Decode(&e); err != nil {
		return nil, fmt.Errorf("find event: %w", err)
	}
	return &e, nil
}

func (s *Store) createEvent(ctx context.Context, e *OrgEvent) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createEvent")
	defer span.End()
	_, err := s.orgEvents().InsertOne(ctx, e)
	return err
}

func (s *Store) updateEvent(ctx context.Context, eventID, orgID string, update bson.M) error {
	ctx, span := mongo.StartSpan(ctx, "Store.updateEvent")
	defer span.End()
	_, err := s.orgEvents().UpdateOne(ctx,
		bson.M{"_id": eventID, "org_id": orgID},
		bson.M{"$set": update},
	)
	return err
}

func (s *Store) deleteEvent(ctx context.Context, eventID, orgID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteEvent")
	defer span.End()
	_, err := s.orgEvents().DeleteOne(ctx, bson.M{"_id": eventID, "org_id": orgID})
	return err
}

// ── Budget Lines ──────────────────────────────────────────────────────────────

func (s *Store) getBudgetLines(ctx context.Context, eventID, orgID string) ([]BudgetLine, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getBudgetLines")
	defer span.End()
	cur, err := s.budgetLines().Find(ctx, bson.M{"event_id": eventID, "org_id": orgID})
	if err != nil {
		return nil, fmt.Errorf("find budget lines: %w", err)
	}
	defer cur.Close(ctx)
	var lines []BudgetLine
	if err := cur.All(ctx, &lines); err != nil {
		return nil, fmt.Errorf("decode budget lines: %w", err)
	}
	return lines, nil
}

func (s *Store) createBudgetLine(ctx context.Context, l *BudgetLine) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createBudgetLine")
	defer span.End()
	_, err := s.budgetLines().InsertOne(ctx, l)
	return err
}

func (s *Store) deleteBudgetLine(ctx context.Context, lineID, orgID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteBudgetLine")
	defer span.End()
	_, err := s.budgetLines().DeleteOne(ctx, bson.M{"_id": lineID, "org_id": orgID})
	return err
}

// ── Event Comments ────────────────────────────────────────────────────────────

func (s *Store) getEventComments(ctx context.Context, eventID, orgID string) ([]EventComment, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getEventComments")
	defer span.End()
	opts := options.Find().SetSort(bson.M{"created_at": 1})
	cur, err := s.eventComments().Find(ctx, bson.M{"event_id": eventID, "org_id": orgID}, opts)
	if err != nil {
		return nil, fmt.Errorf("find event comments: %w", err)
	}
	defer cur.Close(ctx)
	var comments []EventComment
	if err := cur.All(ctx, &comments); err != nil {
		return nil, fmt.Errorf("decode event comments: %w", err)
	}
	return comments, nil
}

func (s *Store) createEventComment(ctx context.Context, c *EventComment) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createEventComment")
	defer span.End()
	_, err := s.eventComments().InsertOne(ctx, c)
	return err
}

// ── Transaction Requests ──────────────────────────────────────────────────────

func (s *Store) getTxRequests(ctx context.Context, orgID string, filter bson.M) ([]TxRequest, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getTxRequests")
	defer span.End()
	q := bson.M{"org_id": orgID}
	for k, v := range filter {
		q[k] = v
	}
	opts := options.Find().SetSort(bson.M{"created_at": -1})
	cur, err := s.txRequests().Find(ctx, q, opts)
	if err != nil {
		return nil, fmt.Errorf("find tx requests: %w", err)
	}
	defer cur.Close(ctx)
	var reqs []TxRequest
	if err := cur.All(ctx, &reqs); err != nil {
		return nil, fmt.Errorf("decode tx requests: %w", err)
	}
	return reqs, nil
}

func (s *Store) getTxRequest(ctx context.Context, reqID, orgID string) (*TxRequest, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getTxRequest")
	defer span.End()
	var r TxRequest
	if err := s.txRequests().FindOne(ctx, bson.M{"_id": reqID, "org_id": orgID}).Decode(&r); err != nil {
		return nil, fmt.Errorf("find tx request: %w", err)
	}
	return &r, nil
}

func (s *Store) createTxRequest(ctx context.Context, r *TxRequest) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createTxRequest")
	defer span.End()
	_, err := s.txRequests().InsertOne(ctx, r)
	return err
}

func (s *Store) appendStatusLog(ctx context.Context, reqID, orgID string, entry StatusLogEntry) error {
	ctx, span := mongo.StartSpan(ctx, "Store.appendStatusLog")
	defer span.End()
	_, err := s.txRequests().UpdateOne(ctx,
		bson.M{"_id": reqID, "org_id": orgID},
		bson.M{"$push": bson.M{"status_log": entry}},
	)
	return err
}

func (s *Store) updateTxRequest(ctx context.Context, reqID, orgID string, update bson.M) error {
	ctx, span := mongo.StartSpan(ctx, "Store.updateTxRequest")
	defer span.End()
	_, err := s.txRequests().UpdateOne(ctx,
		bson.M{"_id": reqID, "org_id": orgID},
		bson.M{"$set": update},
	)
	return err
}

// ── Ledger ────────────────────────────────────────────────────────────────────

func (s *Store) getLedgerEntries(ctx context.Context, orgID, fiscalYearID string, extra bson.M) ([]OrgLedgerEntry, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getLedgerEntries")
	defer span.End()

	filter := bson.M{"org_id": orgID}
	if fiscalYearID != "" {
		filter["fiscal_year_id"] = fiscalYearID
	}
	for k, v := range extra {
		filter[k] = v
	}
	cur, err := s.orgLedger().Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "date", Value: -1}}))
	if err != nil {
		return nil, err
	}
	var entries []OrgLedgerEntry
	if err := cur.All(ctx, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) createLedgerEntry(ctx context.Context, e *OrgLedgerEntry) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createLedgerEntry")
	defer span.End()
	_, err := s.orgLedger().InsertOne(ctx, e)
	return err
}

func (s *Store) updateLedgerEntry(ctx context.Context, id, orgID string, update bson.M) error {
	ctx, span := mongo.StartSpan(ctx, "Store.updateLedgerEntry")
	defer span.End()
	_, err := s.orgLedger().UpdateOne(ctx, bson.M{"_id": id, "org_id": orgID}, bson.M{"$set": update})
	return err
}

// ── Attachments ───────────────────────────────────────────────────────────────

func (s *Store) getAttachments(ctx context.Context, requestID, orgID string) ([]OrgAttachment, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getAttachments")
	defer span.End()
	cur, err := s.orgAttachments().Find(ctx, bson.M{"request_id": requestID, "org_id": orgID})
	if err != nil {
		return nil, err
	}
	var attachments []OrgAttachment
	if err := cur.All(ctx, &attachments); err != nil {
		return nil, err
	}
	return attachments, nil
}

func (s *Store) createAttachment(ctx context.Context, a *OrgAttachment) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createAttachment")
	defer span.End()
	_, err := s.orgAttachments().InsertOne(ctx, a)
	return err
}

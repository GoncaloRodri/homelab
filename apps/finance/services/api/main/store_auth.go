package main

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *Store) createAuthUser(ctx context.Context, u *AuthUser) error {
	u.ID = bson.NewObjectID()
	u.CreatedAt = time.Now()
	_, err := s.db.Collection("finance_users").InsertOne(ctx, u)
	return err
}

func (s *Store) findAuthUserByEmail(ctx context.Context, email string) (*AuthUser, error) {
	var u AuthUser
	err := s.db.Collection("finance_users").FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &u, err
}

func (s *Store) findAuthUserByID(ctx context.Context, userID string) (*AuthUser, error) {
	oid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, nil
	}
	var u AuthUser
	err = s.db.Collection("finance_users").FindOne(ctx, bson.M{"_id": oid}).Decode(&u)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &u, err
}

func (s *Store) findAuthUserByProvider(ctx context.Context, provider, providerID string) (*AuthUser, error) {
	var u AuthUser
	err := s.db.Collection("finance_users").FindOne(ctx, bson.M{
		"provider":    provider,
		"provider_id": providerID,
	}).Decode(&u)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &u, err
}

func (s *Store) createAuthSession(ctx context.Context, sess *AuthSession) error {
	sess.ID = bson.NewObjectID()
	sess.CreatedAt = time.Now()
	_, err := s.db.Collection("finance_sessions").InsertOne(ctx, sess)
	return err
}

func (s *Store) getAuthSession(ctx context.Context, id string) (*AuthSession, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, nil
	}
	var sess AuthSession
	err = s.db.Collection("finance_sessions").FindOne(ctx, bson.M{"_id": oid}).Decode(&sess)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &sess, err
}

func (s *Store) deleteAuthSession(ctx context.Context, id string) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil
	}
	_, err = s.db.Collection("finance_sessions").DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

func (s *Store) getSessionsByUserID(ctx context.Context, userID string) ([]AuthSession, error) {
	oid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, nil
	}
	cur, err := s.db.Collection("finance_sessions").Find(ctx,
		bson.M{"user_id": oid},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	var sessions []AuthSession
	if err := cur.All(ctx, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *Store) deleteSessionForUser(ctx context.Context, sessionID, userID string) error {
	sid, err := bson.ObjectIDFromHex(sessionID)
	if err != nil {
		return nil
	}
	uid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil
	}
	_, err = s.db.Collection("finance_sessions").DeleteOne(ctx, bson.M{"_id": sid, "user_id": uid})
	return err
}

// deleteAllUserData purges every record belonging to userID across all collections,
// then deletes the user account itself. Irreversible.
func (s *Store) deleteAllUserData(ctx context.Context, userID string) error {
	uid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return err
	}
	filter := bson.M{"user_id": uid}
	orFilter := bson.M{"$or": bson.A{bson.M{"owner_id": uid}, bson.M{"partner_id": uid}}}
	orFilterPerms := bson.M{"$or": bson.A{bson.M{"owner_id": uid}, bson.M{"viewer_id": uid}}}

	collections := []struct {
		name   string
		filter interface{}
	}{
		{"finance_accounts", filter},
		{"finance_categories", filter},
		{"finance_transactions", filter},
		{"finance_trades", filter},
		{"finance_ticker_mappings", filter},
		{"finance_goals", filter},
		{"finance_import_schedules", filter},
		{"finance_properties", filter},
		{"finance_loans", filter},
		{"finance_permissions", orFilterPerms},
		{"finance_households", orFilter},
		{"finance_sessions", bson.M{"user_id": uid}},
	}
	for _, c := range collections {
		if _, err := s.db.Collection(c.name).DeleteMany(ctx, c.filter); err != nil {
			return err
		}
	}
	_, err = s.db.Collection("finance_users").DeleteOne(ctx, bson.M{"_id": uid})
	return err
}

func (s *Store) ensureAuthIndexes(ctx context.Context) {
	s.db.Collection("finance_users").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true).SetSparse(true),
	})
	s.db.Collection("finance_sessions").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	})
}

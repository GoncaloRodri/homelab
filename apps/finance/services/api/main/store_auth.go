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

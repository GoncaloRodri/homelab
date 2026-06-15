package main

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *Store) getProperties(ctx context.Context, userID string) ([]Property, error) {
	cur, err := s.db.Collection("finance_properties").Find(ctx,
		bson.M{"user_id": userID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	var out []Property
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) getProperty(ctx context.Context, id, userID string) (*Property, error) {
	var p Property
	err := s.db.Collection("finance_properties").FindOne(ctx,
		bson.M{"_id": id, "user_id": userID},
	).Decode(&p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) createProperty(ctx context.Context, p *Property) error {
	p.CreatedAt = time.Now()
	_, err := s.db.Collection("finance_properties").InsertOne(ctx, p)
	return err
}

func (s *Store) updateProperty(ctx context.Context, id, userID string, update bson.M) error {
	_, err := s.db.Collection("finance_properties").UpdateOne(ctx,
		bson.M{"_id": id, "user_id": userID},
		bson.M{"$set": update},
	)
	return err
}

func (s *Store) deleteProperty(ctx context.Context, id, userID string) error {
	_, err := s.db.Collection("finance_properties").DeleteOne(ctx,
		bson.M{"_id": id, "user_id": userID},
	)
	return err
}

func (s *Store) getLoans(ctx context.Context, userID string) ([]Loan, error) {
	cur, err := s.db.Collection("finance_loans").Find(ctx,
		bson.M{"user_id": userID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	var out []Loan
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) getLoan(ctx context.Context, id, userID string) (*Loan, error) {
	var l Loan
	err := s.db.Collection("finance_loans").FindOne(ctx,
		bson.M{"_id": id, "user_id": userID},
	).Decode(&l)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Store) createLoan(ctx context.Context, l *Loan) error {
	l.CreatedAt = time.Now()
	_, err := s.db.Collection("finance_loans").InsertOne(ctx, l)
	return err
}

func (s *Store) updateLoan(ctx context.Context, id, userID string, update bson.M) error {
	_, err := s.db.Collection("finance_loans").UpdateOne(ctx,
		bson.M{"_id": id, "user_id": userID},
		bson.M{"$set": update},
	)
	return err
}

func (s *Store) deleteLoan(ctx context.Context, id, userID string) error {
	_, err := s.db.Collection("finance_loans").DeleteOne(ctx,
		bson.M{"_id": id, "user_id": userID},
	)
	return err
}

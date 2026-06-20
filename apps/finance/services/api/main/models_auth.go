package main

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type AuthUser struct {
	ID           bson.ObjectID `bson:"_id,omitempty"`
	Email        string        `bson:"email"`
	Name         string        `bson:"name,omitempty"`
	PasswordHash string        `bson:"password_hash,omitempty"`
	Provider     string        `bson:"provider,omitempty"`    // "google" or ""
	ProviderID   string        `bson:"provider_id,omitempty"` // provider's user sub/id
	CreatedAt    time.Time     `bson:"created_at"`
}

type AuthSession struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	UserID    bson.ObjectID `bson:"user_id"`
	Email     string        `bson:"email"`
	ExpiresAt time.Time     `bson:"expires_at"`
	CreatedAt time.Time     `bson:"created_at"`
	IPAddress string        `bson:"ip,omitempty"`
	Device    string        `bson:"device,omitempty"` // "Chrome on macOS" etc.
}

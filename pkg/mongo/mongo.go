package mongo

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var tracer = otel.Tracer("mongo")

type DB struct {
	client *mongo.Client
	db     *mongo.Database
}

func Connect(ctx context.Context) (*DB, error) {
	ctx, span := tracer.Start(ctx, "mongo.connect")
	defer span.End()

	uri := env("MONGO_URI", "mongodb://localhost:27017")
	dbName := env("MONGO_DB", "homelab")
	span.SetAttributes(attribute.String("db.name", dbName))

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	slog.Info("connected to mongo", "db", dbName)
	return &DB{client: client, db: client.Database(dbName)}, nil
}

func StartSpan(ctx context.Context, op string) (context.Context, trace.Span) {
	return tracer.Start(ctx, op)
}

func (d *DB) Close(ctx context.Context) error {
	return d.client.Disconnect(ctx)
}

func (d *DB) Collection(name string) *mongo.Collection {
	return d.db.Collection(name)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

package main

import (
	"context"
	"log/slog"
	"time"

	"homelab/pkg/mongo"

	"go.opentelemetry.io/otel/attribute"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func startWorker(db *mongo.DB, interval time.Duration) {
	col := db.Collection("example_pings")

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

			func() {
				ctx, span := mongo.StartSpan(ctx, "worker.insert")
				defer span.End()

				doc := bson.M{"ts": time.Now().UTC(), "msg": "ping"}
				_, err := col.InsertOne(ctx, doc)
				if err != nil {
					span.RecordError(err)
					slog.Error("worker insert", "err", err)
				}
			}()

			func() {
				ctx, span := mongo.StartSpan(ctx, "worker.find")
				defer span.End()

				cur, err := col.Find(ctx, bson.M{}, options.Find().SetLimit(5).SetSort(bson.M{"ts": -1}))
				if err != nil {
					span.RecordError(err)
					slog.Error("worker find", "err", err)
					cancel()
					return
				}

				var results []bson.M
				if err := cur.All(ctx, &results); err != nil {
					span.RecordError(err)
					slog.Error("worker decode", "err", err)
				}
				cur.Close(ctx)
				span.SetAttributes(attribute.Int("result_count", len(results)))
			}()

			cancel()
		}
	}()

	slog.Info("worker started", "interval", interval)
}

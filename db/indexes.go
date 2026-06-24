package db

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// EnsureIndexes создаёт уникальные индексы на коллекции users.
func (d *DB) EnsureIndexes() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := d.Collection("users").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "phone", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_phone")},
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_email")},
		{Keys: bson.D{{Key: "nickname", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_nickname")},
	})
	if err != nil {
		return err
	}

	// TTL-индекс: токены сброса пароля удаляются автоматически по истечении.
	_, err = d.Collection("reset_tokens").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_expires"),
	})
	return err
}

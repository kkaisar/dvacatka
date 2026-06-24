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
	if _, err = d.Collection("reset_tokens").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_expires"),
	}); err != nil {
		return err
	}

	// Статистика матчей: одна запись на (лобби, игрок); поиск по игроку для профиля.
	_, err = d.Collection("match_stats").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "lobby_id", Value: 1}, {Key: "user_id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_lobby_user")},
		{Keys: bson.D{{Key: "user_id", Value: 1}}, Options: options.Index().SetName("by_user")},
	})
	return err
}

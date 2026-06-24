// Одноразовая утилита: очищает коллекции users и lobbies от тестовых данных.
// Запуск: go run ./scripts/cleanup
package main

import (
	"context"
	"log"
	"time"

	"dvacatka/config"
	"dvacatka/db"

	"go.mongodb.org/mongo-driver/bson"
)

func main() {
	cfg := config.Load()
	database, err := db.Connect(cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatalf("cleanup: подключение: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	u, err := database.Collection("users").DeleteMany(ctx, bson.M{})
	if err != nil {
		log.Fatalf("cleanup: users: %v", err)
	}
	l, err := database.Collection("lobbies").DeleteMany(ctx, bson.M{})
	if err != nil {
		log.Fatalf("cleanup: lobbies: %v", err)
	}
	log.Printf("cleanup: удалено users=%d, lobbies=%d", u.DeletedCount, l.DeletedCount)
}

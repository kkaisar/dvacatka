// Разовый скрипт: выставляет ручную пару полуфиналов в сетке демо-лобби.
// Запуск: go run ./scripts/setbracket   (использует MONGODB_DB из .env = прод)
package main

import (
	"context"
	"log"
	"time"

	"dvacatka/config"
	"dvacatka/db"
	"dvacatka/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func ip(v int) *int { return &v }

func main() {
	const lobbyHex = "6a3c57cbdb2cd98d927a337d"
	// Желаемые полуфиналы (id команд: AbiX=1, areokk=2, TND=3, Aidakhr=4):
	//   матч0: TND(3) vs AbiX(1)
	//   матч1: areokk(2) vs Aidakhr(4)

	cfg := config.Load()
	database, err := db.Connect(cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	lid, _ := primitive.ObjectIDFromHex(lobbyHex)
	var l models.Lobby
	if err := database.Collection("lobbies").FindOne(ctx, bson.M{"_id": lid}).Decode(&l); err != nil {
		log.Fatalf("find lobby: %v", err)
	}
	if len(l.Bracket.Rounds) < 2 || len(l.Bracket.Rounds[0].Matches) < 2 {
		log.Fatalf("неожиданная структура сетки")
	}

	// Полуфиналы (раунд 0).
	m0 := &l.Bracket.Rounds[0].Matches[0]
	m0.Team1, m0.Team2, m0.Winner, m0.Score1, m0.Score2 = ip(3), ip(1), nil, 0, 0
	m1 := &l.Bracket.Rounds[0].Matches[1]
	m1.Team1, m1.Team2, m1.Winner, m1.Score1, m1.Score2 = ip(2), ip(4), nil, 0, 0

	// Финал (раунд 1) — очистить.
	f := &l.Bracket.Rounds[1].Matches[0]
	f.Team1, f.Team2, f.Winner, f.Score1, f.Score2 = nil, nil, nil, 0, 0

	if _, err := database.Collection("lobbies").UpdateByID(ctx, lid, bson.M{"$set": bson.M{"bracket": l.Bracket}}); err != nil {
		log.Fatalf("update: %v", err)
	}
	log.Println("сетка обновлена: матч0 TND vs AbiX, матч1 areokk vs Aidakhr")
}

package db

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// DB оборачивает клиент MongoDB и активную базу данных.
type DB struct {
	Client   *mongo.Client
	Database *mongo.Database
}

// Connect устанавливает соединение с MongoDB и проверяет его через Ping.
func Connect(uri, dbName string) (*DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	log.Printf("db: подключено к MongoDB, база %q", dbName)
	return &DB{Client: client, Database: client.Database(dbName)}, nil
}

// Collection — удобный доступ к коллекции активной базы.
func (d *DB) Collection(name string) *mongo.Collection {
	return d.Database.Collection(name)
}

// Disconnect корректно закрывает соединение с MongoDB.
func (d *DB) Disconnect(ctx context.Context) error {
	return d.Client.Disconnect(ctx)
}

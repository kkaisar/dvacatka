package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Category — игровая категория пользователя.
type Category string

const (
	CategoryA       Category = "A"
	CategoryB       Category = "B"
	CategoryC       Category = "C"
	CategoryD       Category = "Д"
	CategoryCaptain Category = "Captain"
)

// CategoryRank задаёт порядок сортировки категорий: A, B, C, Д, Captain.
func CategoryRank(c Category) int {
	switch c {
	case CategoryA:
		return 0
	case CategoryB:
		return 1
	case CategoryC:
		return 2
	case CategoryD:
		return 3
	case CategoryCaptain:
		return 4
	default:
		return 5
	}
}

// User — зарегистрированный пользователь платформы.
type User struct {
	ID           primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	Phone        string               `bson:"phone" json:"phone"`
	Email        string               `bson:"email" json:"email"`
	Nickname     string               `bson:"nickname" json:"nickname"`
	RealName     string               `bson:"real_name" json:"real_name"`
	PasswordHash string               `bson:"password_hash" json:"-"`
	Category     Category             `bson:"category" json:"category"`
	IsAdmin      bool                 `bson:"is_admin" json:"is_admin"`
	IsBlocked    bool                 `bson:"is_blocked" json:"is_blocked"`
	CreatedAt    time.Time            `bson:"created_at" json:"created_at"`
	GameHistory  []primitive.ObjectID `bson:"game_history" json:"game_history"`
}

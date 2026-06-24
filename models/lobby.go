package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LobbyType — формат турнира.
type LobbyType string

const (
	TypeDvacatka LobbyType = "dvacatka" // 20 игроков, 4 команды
	TypeTricatka LobbyType = "tricatka" // 30 игроков, 6 команд
	TypeSorokovka LobbyType = "sorokovka" // 40 игроков, 8 команд
)

// LobbyStatus — этап жизненного цикла лобби.
type LobbyStatus string

const (
	StatusOpen     LobbyStatus = "open"     // сбор игроков
	StatusDraft    LobbyStatus = "draft"    // пики капитанов
	StatusActive   LobbyStatus = "active"   // турнирная сетка
	StatusFinished LobbyStatus = "finished" // завершено
)

// PaymentDetails — реквизиты для оплаты участия.
type PaymentDetails struct {
	Phone string `bson:"phone" json:"phone"`
	Card  string `bson:"card" json:"card"`
}

// LobbyPlayer — игрок внутри лобби.
type LobbyPlayer struct {
	UserID primitive.ObjectID  `bson:"user_id" json:"user_id"`
	Paid   bool                `bson:"paid" json:"paid"`
	TeamID *int                `bson:"team_id,omitempty" json:"team_id,omitempty"`
}

// TeamSlot — занятый слот в составе команды.
type TeamSlot struct {
	UserID   primitive.ObjectID `bson:"user_id" json:"user_id"`
	Category Category           `bson:"category" json:"category"`
}

// Team — команда внутри лобби.
type Team struct {
	ID        int                `bson:"id" json:"id"`
	Name      string             `bson:"name" json:"name"`
	CaptainID primitive.ObjectID `bson:"captain_id,omitempty" json:"captain_id"`
	Slots     []TeamSlot         `bson:"slots" json:"slots"`
}

// Match — один матч турнирной сетки.
type Match struct {
	ID     int   `bson:"id" json:"id"`
	Team1  *int  `bson:"team1,omitempty" json:"team1,omitempty"`
	Team2  *int  `bson:"team2,omitempty" json:"team2,omitempty"`
	Score1 int   `bson:"score1" json:"score1"`
	Score2 int   `bson:"score2" json:"score2"`
	Winner *int  `bson:"winner,omitempty" json:"winner,omitempty"`
}

// Round — раунд сетки (набор матчей).
type Round struct {
	Matches []Match `bson:"matches" json:"matches"`
}

// Bracket — турнирная сетка (single elimination).
type Bracket struct {
	Rounds []Round `bson:"rounds" json:"rounds"`
}

// PickRecord — одна запись истории пиков (для отмены ошибочного выбора).
type PickRecord struct {
	TeamID int                `bson:"team_id" json:"team_id"`
	UserID primitive.ObjectID `bson:"user_id" json:"user_id"`
}

// Draft — состояние этапа пиков.
type Draft struct {
	Order   []int        `bson:"order" json:"order"`     // team_id в порядке очереди пиков
	Turn    int          `bson:"turn" json:"turn"`       // индекс в Order — чья сейчас очередь
	Picking bool         `bson:"picking" json:"picking"` // true, когда все капитаны заняли слоты и пошли пики
	History []PickRecord `bson:"history" json:"history"` // история пиков для отмены
}

// Lobby — турнирное лобби.
type Lobby struct {
	ID             primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	Name           string              `bson:"name" json:"name"`
	Type           LobbyType           `bson:"type" json:"type"`
	MaxPlayers     int                 `bson:"max_players" json:"max_players"`
	TeamCount      int                 `bson:"team_count" json:"team_count"`
	Password       string              `bson:"password,omitempty" json:"-"`
	Status         LobbyStatus         `bson:"status" json:"status"`
	CreatorID      primitive.ObjectID  `bson:"creator_id" json:"creator_id"`
	PaymentDetails PaymentDetails      `bson:"payment_details" json:"payment_details"`
	Players        []LobbyPlayer       `bson:"players" json:"players"`
	Teams          []Team              `bson:"teams" json:"teams"`
	Draft          Draft               `bson:"draft" json:"draft"`
	Bracket        Bracket             `bson:"bracket" json:"bracket"`
	CreatedAt      time.Time           `bson:"created_at" json:"created_at"`
	WinnerTeamID   *int                `bson:"winner_team_id,omitempty" json:"winner_team_id,omitempty"`
}

// TypeConfig возвращает (max_players, team_count) для типа лобби.
func TypeConfig(t LobbyType) (maxPlayers, teamCount int) {
	switch t {
	case TypeDvacatka:
		return 20, 4
	case TypeTricatka:
		return 30, 6
	case TypeSorokovka:
		return 40, 8
	default:
		return 0, 0
	}
}

package handlers

import (
	"context"
	"errors"
	"strings"
	"time"

	"dvacatka/config"
	"dvacatka/db"
	"dvacatka/middleware"
	"dvacatka/models"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// UserHandler обслуживает профиль и настройки пользователя.
type UserHandler struct {
	DB  *db.DB
	Cfg *config.Config
}

func NewUserHandler(database *db.DB, cfg *config.Config) *UserHandler {
	return &UserHandler{DB: database, Cfg: cfg}
}

func (h *UserHandler) users() *mongo.Collection {
	return h.DB.Collection("users")
}

// findUser достаёт пользователя по ID.
func (h *UserHandler) findUser(ctx context.Context, id primitive.ObjectID) (models.User, error) {
	var u models.User
	err := h.users().FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	return u, err
}

// Profile — GET /profile. Полные данные текущего пользователя.
func (h *UserHandler) Profile(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	u, err := h.findUser(ctx, middleware.UserID(c))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "пользователь не найден")
	}
	return c.JSON(fiber.Map{
		"id":           u.ID.Hex(),
		"phone":        u.Phone,
		"email":        u.Email,
		"nickname":     u.Nickname,
		"real_name":    u.RealName,
		"category":     u.Category,
		"created_at":   u.CreatedAt,
		"game_history": u.GameHistory,
	})
}

type settingsReq struct {
	Nickname string `json:"nickname"`
	RealName string `json:"real_name"`
	Category string `json:"category"`
}

// Settings — PUT /profile/settings. Изменить никнейм, имя, категорию.
func (h *UserHandler) Settings(c *fiber.Ctx) error {
	var req settingsReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	req.Nickname = strings.TrimSpace(req.Nickname)
	req.RealName = strings.TrimSpace(req.RealName)

	if req.Nickname == "" {
		return fiber.NewError(fiber.StatusBadRequest, "никнейм обязателен")
	}
	if !validCategory(models.Category(req.Category)) {
		return fiber.NewError(fiber.StatusBadRequest, "недопустимая категория")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	uid := middleware.UserID(c)

	// Никнейм должен оставаться уникальным (исключая самого себя).
	taken, err := h.users().CountDocuments(ctx, bson.M{
		"nickname": req.Nickname,
		"_id":      bson.M{"$ne": uid},
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка базы данных")
	}
	if taken > 0 {
		return fiber.NewError(fiber.StatusConflict, "никнейм уже занят")
	}

	_, err = h.users().UpdateByID(ctx, uid, bson.M{"$set": bson.M{
		"nickname":  req.Nickname,
		"real_name": req.RealName,
		"category":  req.Category,
	}})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось сохранить настройки")
	}
	return c.JSON(fiber.Map{"ok": true})
}

// PublicProfile — GET /player/:id. Публичный профиль игрока.
func (h *UserHandler) PublicProfile(c *fiber.Ctx) error {
	id, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id игрока")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	u, err := h.findUser(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fiber.NewError(fiber.StatusNotFound, "игрок не найден")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка базы данных")
	}

	history := h.gameHistory(ctx, u.GameHistory)

	return c.JSON(fiber.Map{
		"id":           u.ID.Hex(),
		"nickname":     u.Nickname,
		"real_name":    u.RealName,
		"category":     u.Category,
		"phone":        u.Phone,
		"game_history": history,
	})
}

// gameHistory подтягивает краткую инфу по сыгранным лобби.
func (h *UserHandler) gameHistory(ctx context.Context, ids []primitive.ObjectID) []fiber.Map {
	out := []fiber.Map{}
	if len(ids) == 0 {
		return out
	}
	cur, err := h.DB.Collection("lobbies").Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return out
	}
	defer cur.Close(ctx)

	var lobbies []models.Lobby
	if err := cur.All(ctx, &lobbies); err != nil {
		return out
	}
	for _, l := range lobbies {
		out = append(out, fiber.Map{
			"lobby_id":       l.ID.Hex(),
			"name":           l.Name,
			"type":           l.Type,
			"status":         l.Status,
			"winner_team_id": l.WinnerTeamID,
			"created_at":     l.CreatedAt,
		})
	}
	return out
}

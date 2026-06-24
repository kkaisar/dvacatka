package handlers

import (
	"context"
	"crypto/subtle"
	"strings"
	"time"

	"dvacatka/config"
	"dvacatka/db"
	"dvacatka/middleware"
	"dvacatka/models"
	"dvacatka/ws"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

// AdminHandler обслуживает админ-панель (единый аккаунт по ADMIN_PASSWORD).
type AdminHandler struct {
	DB  *db.DB
	Cfg *config.Config
	Hub *ws.Hub
}

func NewAdminHandler(database *db.DB, cfg *config.Config, hub *ws.Hub) *AdminHandler {
	return &AdminHandler{DB: database, Cfg: cfg, Hub: hub}
}

func (h *AdminHandler) users() *mongo.Collection { return h.DB.Collection("users") }

type adminLoginReq struct {
	Password string `json:"password"`
}

// Login — POST /admin/login. Вход по ADMIN_PASSWORD.
func (h *AdminHandler) Login(c *fiber.Ctx) error {
	var req adminLoginReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	// Сравнение в постоянное время, чтобы не утекал пароль по таймингу.
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(h.Cfg.AdminPassword)) != 1 {
		return fiber.NewError(fiber.StatusUnauthorized, "неверный пароль администратора")
	}
	token, err := middleware.GenerateAdminToken(h.Cfg.JWTSecret)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось создать сессию")
	}
	middleware.SetAdminCookie(c, token)
	return c.JSON(fiber.Map{"ok": true})
}

// Logout — POST /admin/logout.
func (h *AdminHandler) Logout(c *fiber.Ctx) error {
	middleware.ClearAdminCookie(c)
	return c.JSON(fiber.Map{"ok": true})
}

// Me — GET /admin/me. Возвращает 200 если админ-сессия активна (для фронта).
func (h *AdminHandler) Me(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"admin": true})
}

// ListLobbies — GET /admin/lobbies. Все лобби (для мониторинга/модерации).
func (h *AdminHandler) ListLobbies(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	cur, err := h.DB.Collection("lobbies").Find(ctx, bson.M{})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка базы данных")
	}
	defer cur.Close(ctx)
	var list []models.Lobby
	if err := cur.All(ctx, &list); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка базы данных")
	}
	out := make([]fiber.Map, 0, len(list))
	for _, l := range list {
		out = append(out, fiber.Map{
			"id": l.ID.Hex(), "name": l.Name, "type": l.Type, "status": l.Status,
			"players_count": len(l.Players), "max_players": l.MaxPlayers,
			"creator_id": l.CreatorID.Hex(), "created_at": l.CreatedAt,
		})
	}
	return c.JSON(fiber.Map{"lobbies": out})
}

// DeleteLobby — DELETE /admin/lobbies/:id. Админ удаляет любое лобби (включая активное).
func (h *AdminHandler) DeleteLobby(c *fiber.Ctx) error {
	id, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id лобби")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	r, err := h.DB.Collection("lobbies").DeleteOne(ctx, bson.M{"_id": id})
	if err != nil || r.DeletedCount == 0 {
		return fiber.NewError(fiber.StatusNotFound, "лобби не найдено")
	}
	h.Hub.Broadcast(id.Hex(), fiber.Map{"type": "deleted"})
	return c.JSON(fiber.Map{"ok": true})
}

// ListUsers — GET /admin/users. Все пользователи.
func (h *AdminHandler) ListUsers(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	cur, err := h.users().Find(ctx, bson.M{})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка базы данных")
	}
	defer cur.Close(ctx)
	var list []models.User
	if err := cur.All(ctx, &list); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка базы данных")
	}
	out := make([]fiber.Map, 0, len(list))
	for _, u := range list {
		out = append(out, fiber.Map{
			"id": u.ID.Hex(), "phone": u.Phone, "email": u.Email,
			"nickname": u.Nickname, "real_name": u.RealName,
			"category": u.Category, "is_blocked": u.IsBlocked,
			"created_at": u.CreatedAt,
		})
	}
	return c.JSON(fiber.Map{"users": out})
}

// CreateUser — POST /admin/create-user. Создать пользователя вручную.
func (h *AdminHandler) CreateUser(c *fiber.Ctx) error {
	var req registerReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	req.Phone = strings.TrimSpace(req.Phone)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Nickname = strings.TrimSpace(req.Nickname)
	req.RealName = strings.TrimSpace(req.RealName)

	if req.Phone == "" || req.Email == "" || req.Nickname == "" {
		return fiber.NewError(fiber.StatusBadRequest, "телефон, email и никнейм обязательны")
	}
	if len(req.Password) < 6 {
		return fiber.NewError(fiber.StatusBadRequest, "пароль минимум 6 символов")
	}
	if !validCategory(models.Category(req.Category)) {
		return fiber.NewError(fiber.StatusBadRequest, "недопустимая категория")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	exists, _ := h.users().CountDocuments(ctx, bson.M{"$or": bson.A{
		bson.M{"phone": req.Phone}, bson.M{"email": req.Email}, bson.M{"nickname": req.Nickname},
	}})
	if exists > 0 {
		return fiber.NewError(fiber.StatusConflict, "телефон, email или никнейм уже заняты")
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	user := models.User{
		Phone: req.Phone, Email: req.Email, Nickname: req.Nickname,
		RealName: req.RealName, PasswordHash: string(hash),
		Category: models.Category(req.Category), CreatedAt: time.Now(),
		GameHistory: []primitive.ObjectID{},
	}
	res, err := h.users().InsertOne(ctx, user)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось создать пользователя")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": res.InsertedID.(primitive.ObjectID).Hex()})
}

type resetPwReq struct {
	Password string `json:"password"`
}

// ResetPassword — POST /admin/users/:id/reset-password.
func (h *AdminHandler) ResetPassword(c *fiber.Ctx) error {
	id, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id")
	}
	var req resetPwReq
	if err := c.BodyParser(&req); err != nil || len(req.Password) < 6 {
		return fiber.NewError(fiber.StatusBadRequest, "пароль минимум 6 символов")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	r, err := h.users().UpdateByID(ctx, id, bson.M{"$set": bson.M{"password_hash": string(hash)}})
	if err != nil || r.MatchedCount == 0 {
		return fiber.NewError(fiber.StatusNotFound, "пользователь не найден")
	}
	return c.JSON(fiber.Map{"ok": true})
}

type blockReq struct {
	Blocked bool `json:"blocked"`
}

// SetBlocked — POST /admin/users/:id/block. Заблокировать/разблокировать.
func (h *AdminHandler) SetBlocked(c *fiber.Ctx) error {
	id, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id")
	}
	var req blockReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	r, err := h.users().UpdateByID(ctx, id, bson.M{"$set": bson.M{"is_blocked": req.Blocked}})
	if err != nil || r.MatchedCount == 0 {
		return fiber.NewError(fiber.StatusNotFound, "пользователь не найден")
	}
	return c.JSON(fiber.Map{"ok": true, "blocked": req.Blocked})
}

// DeleteUser — DELETE /admin/users/:id.
func (h *AdminHandler) DeleteUser(c *fiber.Ctx) error {
	id, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	r, err := h.users().DeleteOne(ctx, bson.M{"_id": id})
	if err != nil || r.DeletedCount == 0 {
		return fiber.NewError(fiber.StatusNotFound, "пользователь не найден")
	}
	return c.JSON(fiber.Map{"ok": true})
}

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler обслуживает регистрацию, вход и выход.
type AuthHandler struct {
	DB  *db.DB
	Cfg *config.Config
}

func NewAuthHandler(database *db.DB, cfg *config.Config) *AuthHandler {
	return &AuthHandler{DB: database, Cfg: cfg}
}

func (h *AuthHandler) users() *mongo.Collection {
	return h.DB.Collection("users")
}

// validCategory проверяет, что категория из допустимого набора.
func validCategory(c models.Category) bool {
	switch c {
	case models.CategoryA, models.CategoryB, models.CategoryC, models.CategoryCaptain:
		return true
	}
	return false
}

type registerReq struct {
	Phone           string `json:"phone"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
	Nickname        string `json:"nickname"`
	RealName        string `json:"real_name"`
	Category        string `json:"category"`
}

// Register — POST /auth/register. Создаёт обычного пользователя.
func (h *AuthHandler) Register(c *fiber.Ctx) error {
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
	if req.Password != req.PasswordConfirm {
		return fiber.NewError(fiber.StatusBadRequest, "пароли не совпадают")
	}
	if !validCategory(models.Category(req.Category)) {
		return fiber.NewError(fiber.StatusBadRequest, "недопустимая категория")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	// Проверка уникальности телефона / email / никнейма.
	exists, err := h.users().CountDocuments(ctx, bson.M{
		"$or": bson.A{
			bson.M{"phone": req.Phone},
			bson.M{"email": req.Email},
			bson.M{"nickname": req.Nickname},
		},
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка базы данных")
	}
	if exists > 0 {
		return fiber.NewError(fiber.StatusConflict, "телефон, email или никнейм уже заняты")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка хеширования пароля")
	}

	user := models.User{
		Phone:        req.Phone,
		Email:        req.Email,
		Nickname:     req.Nickname,
		RealName:     req.RealName,
		PasswordHash: string(hash),
		Category:     models.Category(req.Category),
		CreatedAt:    time.Now(),
		GameHistory:  []primitive.ObjectID{},
	}

	res, err := h.users().InsertOne(ctx, user)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось создать пользователя")
	}
	user.ID = res.InsertedID.(primitive.ObjectID)

	return h.issueToken(c, user)
}

type loginReq struct {
	Login    string `json:"login"` // телефон ИЛИ email
	Password string `json:"password"`
}

// Login — POST /auth/login. Вход по телефону или email + пароль.
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req loginReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	login := strings.TrimSpace(req.Login)
	if login == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "введите логин и пароль")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	var user models.User
	err := h.users().FindOne(ctx, bson.M{
		"$or": bson.A{
			bson.M{"phone": login},
			bson.M{"email": strings.ToLower(login)},
		},
	}).Decode(&user)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "неверный логин или пароль")
	}
	if user.IsBlocked {
		return fiber.NewError(fiber.StatusForbidden, "пользователь заблокирован")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "неверный логин или пароль")
	}

	return h.issueToken(c, user)
}

// Logout — POST /auth/logout.
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	middleware.ClearAuthCookie(c)
	return c.JSON(fiber.Map{"ok": true})
}

func (h *AuthHandler) resetTokens() *mongo.Collection { return h.DB.Collection("reset_tokens") }

type forgotReq struct {
	Email string `json:"email"`
}

// ForgotPassword — POST /auth/forgot-password. Высылает ссылку сброса на email.
// Ответ всегда одинаковый, чтобы не раскрывать, есть ли такой email.
func (h *AuthHandler) ForgotPassword(c *fiber.Ctx) error {
	var req forgotReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	var user models.User
	err := h.users().FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err == nil {
		// Генерируем токен и сохраняем с истечением через 1 час.
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err == nil {
			token := hex.EncodeToString(buf)
			_, _ = h.resetTokens().InsertOne(ctx, bson.M{
				"token":      token,
				"user_id":    user.ID,
				"expires_at": time.Now().Add(time.Hour),
			})
			link := c.BaseURL() + "/reset.html?token=" + token
			_ = sendResetEmail(h.Cfg, user.Email, link)
		}
	}
	return c.JSON(fiber.Map{"ok": true, "message": "если email зарегистрирован, на него отправлена ссылка сброса"})
}

type resetReq struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// ResetPassword — POST /auth/reset-password. Меняет пароль по токену из письма.
func (h *AuthHandler) ResetPassword(c *fiber.Ctx) error {
	var req resetReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	if len(req.Password) < 6 {
		return fiber.NewError(fiber.StatusBadRequest, "пароль минимум 6 символов")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	var doc struct {
		UserID    primitive.ObjectID `bson:"user_id"`
		ExpiresAt time.Time          `bson:"expires_at"`
	}
	err := h.resetTokens().FindOne(ctx, bson.M{"token": req.Token}).Decode(&doc)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ссылка недействительна")
	}
	if time.Now().After(doc.ExpiresAt) {
		_, _ = h.resetTokens().DeleteOne(ctx, bson.M{"token": req.Token})
		return fiber.NewError(fiber.StatusBadRequest, "ссылка истекла, запросите новую")
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	_, err = h.users().UpdateByID(ctx, doc.UserID, bson.M{"$set": bson.M{"password_hash": string(hash)}})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось обновить пароль")
	}
	_, _ = h.resetTokens().DeleteOne(ctx, bson.M{"token": req.Token})
	return c.JSON(fiber.Map{"ok": true})
}

// issueToken генерирует JWT, ставит cookie и возвращает пользователя.
func (h *AuthHandler) issueToken(c *fiber.Ctx, user models.User) error {
	token, err := middleware.GenerateToken(h.Cfg.JWTSecret, user.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось создать токен")
	}
	middleware.SetAuthCookie(c, token)
	return c.JSON(fiber.Map{
		"id":       user.ID.Hex(),
		"nickname": user.Nickname,
		"category": user.Category,
	})
}

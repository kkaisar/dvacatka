package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CookieName — имя cookie, в которой хранится пользовательский JWT.
const CookieName = "token"

// AdminCookieName — имя cookie админ-сессии (отдельной от пользовательской).
const AdminCookieName = "admin_token"

// Claims — полезная нагрузка JWT.
type Claims struct {
	UserID string `json:"uid"`
	Admin  bool   `json:"adm,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken создаёт подписанный JWT для пользователя сроком на 30 дней.
func GenerateToken(secret string, userID primitive.ObjectID) (string, error) {
	claims := Claims{
		UserID: userID.Hex(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// SetAuthCookie кладёт JWT в httpOnly-cookie.
func SetAuthCookie(c *fiber.Ctx, token string) {
	c.Cookie(&fiber.Cookie{
		Name:     CookieName,
		Value:    token,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		HTTPOnly: true,
		SameSite: "Lax",
		Path:     "/",
	})
}

// ClearAuthCookie удаляет cookie авторизации (logout).
func ClearAuthCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     CookieName,
		Value:    "",
		Expires:  time.Now().Add(-time.Hour),
		HTTPOnly: true,
		SameSite: "Lax",
		Path:     "/",
	})
}

// parseUserID извлекает и валидирует userID из токена.
func parseUserID(secret, tokenStr string) (primitive.ObjectID, bool) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return primitive.NilObjectID, false
	}
	id, err := primitive.ObjectIDFromHex(claims.UserID)
	if err != nil {
		return primitive.NilObjectID, false
	}
	return id, true
}

// RequireAuth — middleware: пропускает только с валидным JWT, кладёт userID в locals.
func RequireAuth(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, ok := parseUserID(secret, c.Cookies(CookieName))
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "не авторизован"})
		}
		c.Locals("userID", id)
		return c.Next()
	}
}

// UserID достаёт ObjectID текущего пользователя из контекста (после RequireAuth).
func UserID(c *fiber.Ctx) primitive.ObjectID {
	if id, ok := c.Locals("userID").(primitive.ObjectID); ok {
		return id
	}
	return primitive.NilObjectID
}

// GenerateAdminToken создаёт админский JWT (срок 12 часов).
func GenerateAdminToken(secret string) (string, error) {
	claims := Claims{
		Admin: true,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(12 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// SetAdminCookie кладёт админский JWT в httpOnly-cookie.
func SetAdminCookie(c *fiber.Ctx, token string) {
	c.Cookie(&fiber.Cookie{
		Name: AdminCookieName, Value: token,
		Expires: time.Now().Add(12 * time.Hour),
		HTTPOnly: true, SameSite: "Lax", Path: "/",
	})
}

// ClearAdminCookie удаляет админскую cookie.
func ClearAdminCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name: AdminCookieName, Value: "",
		Expires: time.Now().Add(-time.Hour),
		HTTPOnly: true, SameSite: "Lax", Path: "/",
	})
}

// RequireAdmin — middleware: пропускает только с валидной админ-сессией.
func RequireAdmin(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(c.Cookies(AdminCookieName), claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid || !claims.Admin {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "требуется вход администратора"})
		}
		return c.Next()
	}
}

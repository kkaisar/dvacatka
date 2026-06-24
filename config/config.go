package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config содержит все настройки приложения, загружаемые из ENV.
type Config struct {
	MongoURI      string
	MongoDB       string
	JWTSecret     string
	AdminPassword string
	Port          string

	SMTPHost string
	SMTPPort string
	SMTPUser string
	SMTPPass string
}

// Load читает .env (если есть) и собирает конфиг из переменных окружения.
func Load() *Config {
	// .env опционален: в проде переменные приходят из окружения платформы.
	if err := godotenv.Load(); err != nil {
		log.Println("config: .env не найден, читаю переменные окружения")
	}

	cfg := &Config{
		MongoURI:      get("MONGODB_URI", ""),
		MongoDB:       get("MONGODB_DB", "dvacatka"),
		JWTSecret:     get("JWT_SECRET", "dev-secret-change-me"),
		AdminPassword: get("ADMIN_PASSWORD", "admin123"),
		Port:          get("PORT", "8080"),

		SMTPHost: get("SMTP_HOST", ""),
		SMTPPort: get("SMTP_PORT", "587"),
		SMTPUser: get("SMTP_USER", ""),
		SMTPPass: get("SMTP_PASS", ""),
	}

	if cfg.MongoURI == "" {
		log.Println("config: MONGODB_URI не задан — подключение к MongoDB не будет работать")
	}
	return cfg
}

func get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

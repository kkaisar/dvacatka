package main

import (
	"context"
	"log"

	"dvacatka/config"
	"dvacatka/db"
	"dvacatka/handlers"
	"dvacatka/middleware"
	"dvacatka/ws"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

func main() {
	cfg := config.Load()

	if cfg.MongoURI == "" {
		log.Fatal("main: MONGODB_URI обязателен — заполни .env")
	}

	database, err := db.Connect(cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatalf("main: не удалось подключиться к MongoDB: %v", err)
	}
	defer database.Disconnect(context.Background())

	if err := database.EnsureIndexes(); err != nil {
		log.Printf("main: предупреждение, не удалось создать индексы: %v", err)
	}

	app := fiber.New()

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// --- Авторизация ---
	auth := handlers.NewAuthHandler(database, cfg)
	authGroup := app.Group("/auth")
	authGroup.Post("/register", auth.Register)
	authGroup.Post("/login", auth.Login)
	authGroup.Post("/logout", auth.Logout)
	authGroup.Post("/forgot-password", auth.ForgotPassword)
	authGroup.Post("/reset-password", auth.ResetPassword)

	// --- Профиль ---
	user := handlers.NewUserHandler(database, cfg)
	authReq := middleware.RequireAuth(cfg.JWTSecret)
	app.Get("/profile", authReq, user.Profile)
	app.Put("/profile/settings", authReq, user.Settings)
	app.Get("/player/:id", authReq, user.PublicProfile)

	// --- Лобби (CRUD + этап сбора игроков) ---
	hub := ws.NewHub()
	lobby := handlers.NewLobbyHandler(database, cfg, hub)
	app.Get("/api/lobbies", authReq, lobby.List)
	app.Post("/lobby/create", authReq, lobby.Create)
	app.Get("/lobby/:id", authReq, lobby.Get)
	app.Delete("/lobby/:id", authReq, lobby.Delete)
	app.Post("/lobby/:id/join", authReq, lobby.Join)
	app.Post("/lobby/:id/leave", authReq, lobby.Leave)
	app.Post("/lobby/:id/toggle-paid", authReq, lobby.TogglePaid)
	app.Post("/lobby/:id/kick/:user_id", authReq, lobby.Kick)

	// --- Драфт (этап пиков) ---
	draft := handlers.NewDraftHandler(database, cfg, hub)
	app.Post("/lobby/:id/start-draft", authReq, draft.StartDraft)
	app.Post("/lobby/:id/claim-captain/:team_id", authReq, draft.ClaimCaptain)
	app.Post("/lobby/:id/pick/:user_id", authReq, draft.Pick)
	app.Post("/lobby/:id/undo-pick", authReq, draft.UndoPick)
	app.Get("/lobby/:id/draft-state", authReq, draft.DraftState)

	// --- Турнирная сетка ---
	bracket := handlers.NewBracketHandler(database, cfg, hub)
	app.Post("/lobby/:id/generate-bracket", authReq, bracket.GenerateBracket)
	app.Post("/lobby/:id/match/:match_id/result", authReq, bracket.MatchResult)
	app.Post("/lobby/:id/finish", authReq, bracket.Finish)

	// --- Админ-панель (отдельный вход по ADMIN_PASSWORD) ---
	admin := handlers.NewAdminHandler(database, cfg)
	adminReq := middleware.RequireAdmin(cfg.JWTSecret)
	app.Post("/admin/login", admin.Login)
	app.Post("/admin/logout", admin.Logout)
	app.Get("/admin/users", adminReq, admin.ListUsers)
	app.Post("/admin/create-user", adminReq, admin.CreateUser)
	app.Post("/admin/users/:id/reset-password", adminReq, admin.ResetPassword)
	app.Post("/admin/users/:id/block", adminReq, admin.SetBlocked)
	app.Delete("/admin/users/:id", adminReq, admin.DeleteUser)

	// --- WebSocket: real-time обновления лобби ---
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws/lobby/:id", websocket.New(func(c *websocket.Conn) {
		hub.Handle(c, c.Params("id"))
	}))

	// --- Фронтенд (статические HTML/JS из ./web) ---
	app.Static("/", "./web")

	log.Printf("main: сервер слушает на :%s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("main: сервер остановлен: %v", err)
	}
}

package handlers

import (
	"context"
	"errors"
	"sort"
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
	"go.mongodb.org/mongo-driver/mongo/options"
)

// LobbyHandler обслуживает CRUD лобби.
type LobbyHandler struct {
	DB  *db.DB
	Cfg *config.Config
	Hub *ws.Hub
}

func NewLobbyHandler(database *db.DB, cfg *config.Config, hub *ws.Hub) *LobbyHandler {
	return &LobbyHandler{DB: database, Cfg: cfg, Hub: hub}
}

// broadcast рассылает актуальное состояние лобби всем подписчикам комнаты.
func (h *LobbyHandler) broadcast(ctx context.Context, idHex string) {
	l, err := h.findLobby(ctx, idHex)
	if err != nil {
		return
	}
	h.Hub.Broadcast(idHex, fiber.Map{"type": "lobby", "lobby": h.view(ctx, l)})
}

// playerInfo возвращает карту user_id(hex) -> {nickname, category} по игрокам лобби.
func (h *LobbyHandler) playerInfo(ctx context.Context, l models.Lobby) map[string]fiber.Map {
	info := map[string]fiber.Map{}
	ids := make([]primitive.ObjectID, 0, len(l.Players))
	for _, p := range l.Players {
		ids = append(ids, p.UserID)
	}
	if len(ids) == 0 {
		return info
	}
	cur, err := h.DB.Collection("users").Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return info
	}
	defer cur.Close(ctx)
	var users []models.User
	if cur.All(ctx, &users) != nil {
		return info
	}
	// Ник — из профиля; категория — лобби-локальная (тир в рамках этого лобби).
	localCat := map[primitive.ObjectID]models.Category{}
	for _, p := range l.Players {
		localCat[p.UserID] = p.Category
	}
	for _, u := range users {
		cat := localCat[u.ID]
		if cat == "" {
			cat = u.Category // запасной вариант для старых лобби без снимка
		}
		info[u.ID.Hex()] = fiber.Map{"nickname": u.Nickname, "category": cat}
	}
	return info
}

func (h *LobbyHandler) lobbies() *mongo.Collection {
	return h.DB.Collection("lobbies")
}

// userCategory возвращает глобальную категорию пользователя (для снимка при входе в лобби).
func (h *LobbyHandler) userCategory(ctx context.Context, uid primitive.ObjectID) models.Category {
	var u models.User
	if err := h.DB.Collection("users").FindOne(ctx, bson.M{"_id": uid}).Decode(&u); err != nil {
		return ""
	}
	return u.Category
}

// findLobby достаёт лобби по hex-id из параметра :id.
func (h *LobbyHandler) findLobby(ctx context.Context, idHex string) (models.Lobby, error) {
	var l models.Lobby
	id, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return l, errBadID
	}
	err = h.lobbies().FindOne(ctx, bson.M{"_id": id}).Decode(&l)
	return l, err
}

var errBadID = errors.New("bad id")

type createLobbyReq struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Password       string `json:"password"`
	PaymentPhone   string `json:"payment_phone"`
	PaymentCard    string `json:"payment_card"`
}

// Create — POST /lobby/create. Любой авторизованный создаёт лобби и сам входит в него.
func (h *LobbyHandler) Create(c *fiber.Ctx) error {
	var req createLobbyReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "название обязательно")
	}

	lobbyType := models.LobbyType(req.Type)
	maxPlayers, teamCount := models.TypeConfig(lobbyType)
	if maxPlayers == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "недопустимый тип лобби")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	uid := middleware.UserID(c)
	lobby := models.Lobby{
		Name:       req.Name,
		Type:       lobbyType,
		MaxPlayers: maxPlayers,
		TeamCount:  teamCount,
		Password:   req.Password,
		Status:     models.StatusOpen,
		CreatorID:  uid,
		PaymentDetails: models.PaymentDetails{
			Phone: strings.TrimSpace(req.PaymentPhone),
			Card:  strings.TrimSpace(req.PaymentCard),
		},
		// Создатель сразу попадает в список игроков (с тиром из своего профиля).
		Players:   []models.LobbyPlayer{{UserID: uid, Paid: false, Category: h.userCategory(ctx, uid)}},
		Teams:     []models.Team{},
		Bracket:   models.Bracket{Rounds: []models.Round{}},
		CreatedAt: time.Now(),
	}

	res, err := h.lobbies().InsertOne(ctx, lobby)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось создать лобби")
	}
	lobby.ID = res.InsertedID.(primitive.ObjectID)
	return c.Status(fiber.StatusCreated).JSON(h.view(ctx, lobby))
}

// List — GET /. Активные лобби + история завершённых.
func (h *LobbyHandler) List(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	newest := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	active, err := h.queryLobbies(ctx, bson.M{"status": bson.M{"$ne": models.StatusFinished}}, newest)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка базы данных")
	}
	history, err := h.queryLobbies(ctx, bson.M{"status": models.StatusFinished}, newest)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "ошибка базы данных")
	}

	return c.JSON(fiber.Map{"active": active, "history": history})
}

func (h *LobbyHandler) queryLobbies(ctx context.Context, filter bson.M, opts *options.FindOptions) ([]fiber.Map, error) {
	cur, err := h.lobbies().Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var list []models.Lobby
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	out := make([]fiber.Map, 0, len(list))
	for _, l := range list {
		out = append(out, h.summary(l))
	}
	return out, nil
}

// Get — GET /lobby/:id. Полные данные лобби.
func (h *LobbyHandler) Get(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.findLobby(ctx, c.Params("id"))
	if err != nil {
		if errors.Is(err, errBadID) {
			return fiber.NewError(fiber.StatusBadRequest, "неверный id лобби")
		}
		return fiber.NewError(fiber.StatusNotFound, "лобби не найдено")
	}
	return c.JSON(h.view(ctx, l))
}

// Delete — DELETE /lobby/:id. Только создатель и только пока лобби открыто.
func (h *LobbyHandler) Delete(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.findLobby(ctx, c.Params("id"))
	if err != nil {
		if errors.Is(err, errBadID) {
			return fiber.NewError(fiber.StatusBadRequest, "неверный id лобби")
		}
		return fiber.NewError(fiber.StatusNotFound, "лобби не найдено")
	}
	if l.CreatorID != middleware.UserID(c) {
		return fiber.NewError(fiber.StatusForbidden, "удалить может только создатель")
	}
	if l.Status != models.StatusOpen {
		return fiber.NewError(fiber.StatusConflict, "лобби уже стартовало, удалить нельзя")
	}

	if _, err := h.lobbies().DeleteOne(ctx, bson.M{"_id": l.ID}); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось удалить лобби")
	}
	h.Hub.Broadcast(l.ID.Hex(), fiber.Map{"type": "deleted"})
	return c.JSON(fiber.Map{"ok": true})
}

// hasPlayer проверяет, есть ли пользователь среди игроков лобби.
func hasPlayer(l models.Lobby, uid primitive.ObjectID) bool {
	for _, p := range l.Players {
		if p.UserID == uid {
			return true
		}
	}
	return false
}

type joinReq struct {
	Password string `json:"password"`
}

// Join — POST /lobby/:id/join. Войти в лобби (этап сбора игроков).
func (h *LobbyHandler) Join(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.loadOpen(c, ctx)
	if err != nil {
		return err
	}
	uid := middleware.UserID(c)

	if hasPlayer(l, uid) {
		return fiber.NewError(fiber.StatusConflict, "вы уже в лобби")
	}
	if len(l.Players) >= l.MaxPlayers {
		return fiber.NewError(fiber.StatusConflict, "лобби заполнено")
	}
	if l.Password != "" {
		var req joinReq
		_ = c.BodyParser(&req)
		if req.Password != l.Password {
			return fiber.NewError(fiber.StatusForbidden, "неверный пароль лобби")
		}
	}

	_, err = h.lobbies().UpdateByID(ctx, l.ID, bson.M{
		"$push": bson.M{"players": models.LobbyPlayer{UserID: uid, Paid: false, Category: h.userCategory(ctx, uid)}},
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось войти в лобби")
	}
	h.broadcast(ctx, l.ID.Hex())
	return c.JSON(fiber.Map{"ok": true})
}

// Leave — POST /lobby/:id/leave. Выйти из лобби.
func (h *LobbyHandler) Leave(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.loadOpen(c, ctx)
	if err != nil {
		return err
	}
	uid := middleware.UserID(c)
	if l.CreatorID == uid {
		return fiber.NewError(fiber.StatusForbidden, "создатель не может выйти; удалите лобби")
	}

	_, err = h.lobbies().UpdateByID(ctx, l.ID, bson.M{
		"$pull": bson.M{"players": bson.M{"user_id": uid}},
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось выйти")
	}
	h.broadcast(ctx, l.ID.Hex())
	return c.JSON(fiber.Map{"ok": true})
}

// TogglePaid — POST /lobby/:id/toggle-paid. Переключить свою галочку оплаты.
func (h *LobbyHandler) TogglePaid(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.loadOpen(c, ctx)
	if err != nil {
		return err
	}
	uid := middleware.UserID(c)
	if !hasPlayer(l, uid) {
		return fiber.NewError(fiber.StatusForbidden, "вы не в лобби")
	}

	// Инвертируем текущее значение paid для своего игрока.
	var newPaid bool
	for _, p := range l.Players {
		if p.UserID == uid {
			newPaid = !p.Paid
			break
		}
	}
	_, err = h.lobbies().UpdateOne(ctx,
		bson.M{"_id": l.ID, "players.user_id": uid},
		bson.M{"$set": bson.M{"players.$.paid": newPaid}},
	)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось обновить оплату")
	}
	h.broadcast(ctx, l.ID.Hex())
	return c.JSON(fiber.Map{"ok": true, "paid": newPaid})
}

// Kick — POST /lobby/:id/kick/:user_id. Создатель выгоняет игрока.
func (h *LobbyHandler) Kick(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.loadOpen(c, ctx)
	if err != nil {
		return err
	}
	if l.CreatorID != middleware.UserID(c) {
		return fiber.NewError(fiber.StatusForbidden, "выгонять может только создатель")
	}
	target, err := primitive.ObjectIDFromHex(c.Params("user_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id игрока")
	}
	if target == l.CreatorID {
		return fiber.NewError(fiber.StatusBadRequest, "нельзя выгнать создателя")
	}

	_, err = h.lobbies().UpdateByID(ctx, l.ID, bson.M{
		"$pull": bson.M{"players": bson.M{"user_id": target}},
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось выгнать игрока")
	}
	h.broadcast(ctx, l.ID.Hex())
	return c.JSON(fiber.Map{"ok": true})
}

type setTierReq struct {
	Category string `json:"category"`
}

// SetTier — POST /lobby/:id/set-tier/:user_id. Создатель меняет тир игрока
// ТОЛЬКО в рамках этого лобби (глобальный профиль игрока не меняется).
func (h *LobbyHandler) SetTier(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.loadOpen(c, ctx)
	if err != nil {
		return err
	}
	if l.CreatorID != middleware.UserID(c) {
		return fiber.NewError(fiber.StatusForbidden, "менять тир может только создатель")
	}
	target, err := primitive.ObjectIDFromHex(c.Params("user_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id игрока")
	}
	var req setTierReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	if !validCategory(models.Category(req.Category)) {
		return fiber.NewError(fiber.StatusBadRequest, "недопустимая категория")
	}
	if !hasPlayer(l, target) {
		return fiber.NewError(fiber.StatusBadRequest, "игрок не в лобби")
	}

	res, err := h.lobbies().UpdateOne(ctx,
		bson.M{"_id": l.ID, "players.user_id": target},
		bson.M{"$set": bson.M{"players.$.category": req.Category}},
	)
	if err != nil || res.MatchedCount == 0 {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось изменить тир")
	}
	h.broadcast(ctx, l.ID.Hex())
	return c.JSON(fiber.Map{"ok": true, "category": req.Category})
}

// loadOpen загружает лобби и требует статус "open" (этап сбора игроков).
func (h *LobbyHandler) loadOpen(c *fiber.Ctx, ctx context.Context) (models.Lobby, error) {
	l, err := h.findLobby(ctx, c.Params("id"))
	if err != nil {
		if errors.Is(err, errBadID) {
			return l, fiber.NewError(fiber.StatusBadRequest, "неверный id лобби")
		}
		return l, fiber.NewError(fiber.StatusNotFound, "лобби не найдено")
	}
	if l.Status != models.StatusOpen {
		return l, fiber.NewError(fiber.StatusConflict, "лобби уже не на этапе сбора игроков")
	}
	return l, nil
}

// summary — краткая карточка лобби для списков.
func (h *LobbyHandler) summary(l models.Lobby) fiber.Map {
	return fiber.Map{
		"id":             l.ID.Hex(),
		"name":           l.Name,
		"type":           l.Type,
		"status":         l.Status,
		"max_players":    l.MaxPlayers,
		"team_count":     l.TeamCount,
		"players_count":  len(l.Players),
		"has_password":   l.Password != "",
		"creator_id":     l.CreatorID.Hex(),
		"winner_team_id": l.WinnerTeamID,
		"created_at":     l.CreatedAt,
	}
}

// view — полное представление лобби (без пароля), с никами игроков.
func (h *LobbyHandler) view(ctx context.Context, l models.Lobby) fiber.Map {
	info := h.playerInfo(ctx, l)
	m := h.summary(l)
	m["payment_details"] = l.PaymentDetails
	m["players"] = sortPlayersByCategory(l.Players, info)
	m["player_info"] = info
	m["teams"] = l.Teams
	m["draft"] = l.Draft
	m["bracket"] = l.Bracket
	return m
}

// sortPlayersByCategory сортирует игроков лобби по категории A→B→C→Captain, затем по нику.
func sortPlayersByCategory(players []models.LobbyPlayer, info map[string]fiber.Map) []models.LobbyPlayer {
	sorted := make([]models.LobbyPlayer, len(players))
	copy(sorted, players)
	rank := func(p models.LobbyPlayer) (int, string) {
		i := info[p.UserID.Hex()]
		if i == nil {
			return 4, ""
		}
		cat, _ := i["category"].(models.Category)
		nick, _ := i["nickname"].(string)
		return models.CategoryRank(cat), nick
	}
	sort.SliceStable(sorted, func(a, b int) bool {
		ra, na := rank(sorted[a])
		rb, nb := rank(sorted[b])
		if ra != rb {
			return ra < rb
		}
		return na < nb
	})
	return sorted
}

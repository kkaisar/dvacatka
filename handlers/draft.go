package handlers

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
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
)

// DraftHandler обслуживает этап пиков (жеребьёвка + выбор игроков).
type DraftHandler struct {
	DB  *db.DB
	Cfg *config.Config
	Hub *ws.Hub
}

func NewDraftHandler(database *db.DB, cfg *config.Config, hub *ws.Hub) *DraftHandler {
	return &DraftHandler{DB: database, Cfg: cfg, Hub: hub}
}

func (h *DraftHandler) lobbies() *mongo.Collection { return h.DB.Collection("lobbies") }

// isManager — создатель ИЛИ админ.
func (h *DraftHandler) isManager(c *fiber.Ctx, l models.Lobby) bool {
	return l.CreatorID == middleware.UserID(c) || middleware.IsAdmin(c, h.Cfg.JWTSecret)
}

func (h *DraftHandler) load(c *fiber.Ctx, ctx context.Context) (models.Lobby, error) {
	var l models.Lobby
	id, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return l, fiber.NewError(fiber.StatusBadRequest, "неверный id лобби")
	}
	if err := h.lobbies().FindOne(ctx, bson.M{"_id": id}).Decode(&l); err != nil {
		return l, fiber.NewError(fiber.StatusNotFound, "лобби не найдено")
	}
	return l, nil
}

func (h *DraftHandler) save(ctx context.Context, l models.Lobby) error {
	_, err := h.lobbies().UpdateByID(ctx, l.ID, bson.M{"$set": bson.M{
		"status": l.Status,
		"teams":  l.Teams,
		"draft":  l.Draft,
	}})
	return err
}

func (h *DraftHandler) broadcast(ctx context.Context, l models.Lobby) {
	h.Hub.Broadcast(l.ID.Hex(), fiber.Map{"type": "draft", "draft_state": h.state(ctx, l)})
}

func (h *DraftHandler) slotsPerTeam(l models.Lobby) int {
	if l.TeamCount == 0 {
		return 0
	}
	return l.MaxPlayers / l.TeamCount
}

// StartDraft — POST /lobby/:id/start-draft. Создатель запускает драфт при полном лобби.
func (h *DraftHandler) StartDraft(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.load(c, ctx)
	if err != nil {
		return err
	}
	if !h.isManager(c, l) {
		return fiber.NewError(fiber.StatusForbidden, "начать драфт может только создатель или админ")
	}
	if l.Status != models.StatusOpen {
		return fiber.NewError(fiber.StatusConflict, "драфт уже начат или лобби завершено")
	}
	if len(l.Players) != l.MaxPlayers {
		return fiber.NewError(fiber.StatusConflict, fmt.Sprintf("нужно %d игроков, сейчас %d", l.MaxPlayers, len(l.Players)))
	}

	// Создаём пустые команды.
	teams := make([]models.Team, l.TeamCount)
	for i := 0; i < l.TeamCount; i++ {
		teams[i] = models.Team{
			ID:    i + 1,
			Name:  fmt.Sprintf("Команда %d", i+1),
			Slots: []models.TeamSlot{},
		}
	}
	l.Teams = teams
	l.Draft = models.Draft{Order: []int{}, Turn: 0, Picking: false}
	l.Status = models.StatusDraft

	if err := h.save(ctx, l); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось начать драфт")
	}
	h.broadcast(ctx, l)
	return c.JSON(h.state(ctx, l))
}

// ClaimCaptain — POST /lobby/:id/claim-captain/:team_id.
// Игрок категории Captain занимает капитанский слот команды.
func (h *DraftHandler) ClaimCaptain(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.load(c, ctx)
	if err != nil {
		return err
	}
	if l.Status != models.StatusDraft {
		return fiber.NewError(fiber.StatusConflict, "лобби не на этапе драфта")
	}
	if l.Draft.Picking {
		return fiber.NewError(fiber.StatusConflict, "капитаны уже определены")
	}

	uid := middleware.UserID(c)
	if !hasPlayerLobby(l, uid) {
		return fiber.NewError(fiber.StatusForbidden, "вы не в этом лобби")
	}

	// Проверяем тир в рамках лобби (с запасом на глобальную категорию).
	cat := lobbyCat(l, uid)
	if cat == "" {
		var u models.User
		if h.DB.Collection("users").FindOne(ctx, bson.M{"_id": uid}).Decode(&u) == nil {
			cat = u.Category
		}
	}
	if cat != models.CategoryCaptain {
		return fiber.NewError(fiber.StatusForbidden, "только игрок категории Captain может стать капитаном")
	}
	// Один капитан — одна команда.
	if teamOf(l, uid) != 0 {
		return fiber.NewError(fiber.StatusConflict, "вы уже капитан команды")
	}

	teamID, err := c.ParamsInt("team_id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id команды")
	}
	idx := teamIndex(l, teamID)
	if idx < 0 {
		return fiber.NewError(fiber.StatusBadRequest, "команда не найдена")
	}
	if !l.Teams[idx].CaptainID.IsZero() {
		return fiber.NewError(fiber.StatusConflict, "у команды уже есть капитан")
	}

	l.Teams[idx].CaptainID = uid
	l.Teams[idx].Slots = append(l.Teams[idx].Slots, models.TeamSlot{UserID: uid, Category: cat})

	// Если все команды получили капитанов — жеребьёвка порядка пиков.
	if allCaptained(l) {
		order := make([]int, len(l.Teams))
		for i := range l.Teams {
			order[i] = l.Teams[i].ID
		}
		rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		l.Draft.Order = order
		l.Draft.Turn = 0
		l.Draft.Picking = true
	}

	if err := h.save(ctx, l); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось занять слот капитана")
	}
	h.broadcast(ctx, l)
	return c.JSON(h.state(ctx, l))
}

// Pick — POST /lobby/:id/pick/:user_id. Текущий капитан пикает игрока в свою команду.
func (h *DraftHandler) Pick(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.load(c, ctx)
	if err != nil {
		return err
	}
	if l.Status != models.StatusDraft || !l.Draft.Picking {
		return fiber.NewError(fiber.StatusConflict, "сейчас не этап пиков")
	}

	uid := middleware.UserID(c)
	currentTeamID := l.Draft.Order[l.Draft.Turn]
	curIdx := teamIndex(l, currentTeamID)
	if l.Teams[curIdx].CaptainID != uid {
		return fiber.NewError(fiber.StatusForbidden, "сейчас не ваша очередь пикать")
	}

	target, err := primitive.ObjectIDFromHex(c.Params("user_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id игрока")
	}
	if !hasPlayerLobby(l, target) {
		return fiber.NewError(fiber.StatusBadRequest, "игрок не в лобби")
	}
	if teamOf(l, target) != 0 {
		return fiber.NewError(fiber.StatusConflict, "игрок уже в команде")
	}

	// Категория пикнутого игрока — лобби-локальный тир (с запасом на глобальную).
	cat := lobbyCat(l, target)
	if cat == "" {
		var u models.User
		if h.DB.Collection("users").FindOne(ctx, bson.M{"_id": target}).Decode(&u) == nil {
			cat = u.Category
		}
	}
	l.Teams[curIdx].Slots = append(l.Teams[curIdx].Slots, models.TeamSlot{UserID: target, Category: cat})
	// Запоминаем пик в истории (для возможной отмены).
	l.Draft.History = append(l.Draft.History, models.PickRecord{TeamID: currentTeamID, UserID: target})

	// Передаём ход следующей не заполненной команде.
	advanceTurn(&l, h.slotsPerTeam(l))

	if err := h.save(ctx, l); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось запикать игрока")
	}
	h.broadcast(ctx, l)
	return c.JSON(h.state(ctx, l))
}

// UndoPick — POST /lobby/:id/undo-pick. Создатель отменяет последний пик.
// Игрок возвращается в пул, ход возвращается тому капитану.
func (h *DraftHandler) UndoPick(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.load(c, ctx)
	if err != nil {
		return err
	}
	if !h.isManager(c, l) {
		return fiber.NewError(fiber.StatusForbidden, "отменять пик может только создатель или админ")
	}
	if l.Status != models.StatusDraft || !l.Draft.Picking {
		return fiber.NewError(fiber.StatusConflict, "сейчас не этап пиков")
	}
	if len(l.Draft.History) == 0 {
		return fiber.NewError(fiber.StatusConflict, "нет пиков для отмены")
	}

	// Снимаем последнюю запись истории.
	last := l.Draft.History[len(l.Draft.History)-1]
	l.Draft.History = l.Draft.History[:len(l.Draft.History)-1]

	// Убираем игрока из слотов его команды.
	if idx := teamIndex(l, last.TeamID); idx >= 0 {
		slots := l.Teams[idx].Slots
		for i, s := range slots {
			if s.UserID == last.UserID {
				l.Teams[idx].Slots = append(slots[:i], slots[i+1:]...)
				break
			}
		}
	}

	// Возвращаем ход капитану, чей пик отменили.
	for i, tid := range l.Draft.Order {
		if tid == last.TeamID {
			l.Draft.Turn = i
			break
		}
	}

	if err := h.save(ctx, l); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось отменить пик")
	}
	h.broadcast(ctx, l)
	return c.JSON(h.state(ctx, l))
}

// DraftState — GET /lobby/:id/draft-state.
func (h *DraftHandler) DraftState(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.load(c, ctx)
	if err != nil {
		return err
	}
	return c.JSON(h.state(ctx, l))
}

// state собирает полное состояние драфта для фронта/брокаста.
func (h *DraftHandler) state(ctx context.Context, l models.Lobby) fiber.Map {
	slots := h.slotsPerTeam(l)
	complete := l.Draft.Picking && allTeamsFull(l, slots)

	// Текущий капитан, если идут пики и не завершено.
	var currentCaptain string
	var currentTeam int
	if l.Draft.Picking && !complete && len(l.Draft.Order) > 0 {
		currentTeam = l.Draft.Order[l.Draft.Turn]
		if idx := teamIndex(l, currentTeam); idx >= 0 {
			currentCaptain = l.Teams[idx].CaptainID.Hex()
		}
	}

	return fiber.Map{
		"lobby_id":         l.ID.Hex(),
		"status":           l.Status,
		"slots_per_team":   slots,
		"teams":            h.teamsWithInfo(ctx, l),
		"available":        h.availablePlayers(ctx, l),
		"picking":          l.Draft.Picking,
		"order":            l.Draft.Order,
		"current_team":     currentTeam,
		"current_captain":  currentCaptain,
		"all_captained":    allCaptained(l),
		"complete":         complete,
		"can_undo":         l.Draft.Picking && len(l.Draft.History) > 0,
	}
}

// teamsWithInfo обогащает слоты ником игрока.
func (h *DraftHandler) teamsWithInfo(ctx context.Context, l models.Lobby) []fiber.Map {
	names := h.nicknames(ctx, l)
	out := make([]fiber.Map, 0, len(l.Teams))
	for _, t := range l.Teams {
		sl := make([]fiber.Map, 0, len(t.Slots))
		for _, s := range t.Slots {
			sl = append(sl, fiber.Map{
				"user_id":  s.UserID.Hex(),
				"nickname": names[s.UserID],
				"category": s.Category,
			})
		}
		captain := ""
		if !t.CaptainID.IsZero() {
			captain = t.CaptainID.Hex()
		}
		out = append(out, fiber.Map{
			"id":         t.ID,
			"name":       t.Name,
			"captain_id": captain,
			"slots":      sl,
		})
	}
	return out
}

// availablePlayers — игроки лобби, ещё не попавшие ни в одну команду,
// отсортированные по категории A→B→C→Captain, затем по нику.
func (h *DraftHandler) availablePlayers(ctx context.Context, l models.Lobby) []fiber.Map {
	names := h.nicknames(ctx, l)
	cats := h.categories(ctx, l)
	type row struct {
		uid, nick string
		cat       models.Category
	}
	rows := []row{}
	for _, p := range l.Players {
		if teamOf(l, p.UserID) == 0 {
			rows = append(rows, row{p.UserID.Hex(), names[p.UserID], cats[p.UserID]})
		}
	}
	sort.SliceStable(rows, func(a, b int) bool {
		ra, rb := models.CategoryRank(rows[a].cat), models.CategoryRank(rows[b].cat)
		if ra != rb {
			return ra < rb
		}
		return rows[a].nick < rows[b].nick
	})
	out := make([]fiber.Map, 0, len(rows))
	for _, r := range rows {
		out = append(out, fiber.Map{"user_id": r.uid, "nickname": r.nick, "category": r.cat})
	}
	return out
}

// usersInLobby загружает пользователей лобби одним запросом.
func (h *DraftHandler) usersInLobby(ctx context.Context, l models.Lobby) []models.User {
	ids := make([]primitive.ObjectID, 0, len(l.Players))
	for _, p := range l.Players {
		ids = append(ids, p.UserID)
	}
	cur, err := h.DB.Collection("users").Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil
	}
	defer cur.Close(ctx)
	var users []models.User
	_ = cur.All(ctx, &users)
	return users
}

func (h *DraftHandler) nicknames(ctx context.Context, l models.Lobby) map[primitive.ObjectID]string {
	m := map[primitive.ObjectID]string{}
	for _, u := range h.usersInLobby(ctx, l) {
		m[u.ID] = u.Nickname
	}
	return m
}

func (h *DraftHandler) categories(ctx context.Context, l models.Lobby) map[primitive.ObjectID]models.Category {
	m := map[primitive.ObjectID]models.Category{}
	for _, u := range h.usersInLobby(ctx, l) {
		m[u.ID] = u.Category // глобальная как запасной вариант
	}
	for _, p := range l.Players {
		if p.Category != "" {
			m[p.UserID] = p.Category // лобби-локальный тир в приоритете
		}
	}
	return m
}

// lobbyCat возвращает тир игрока в рамках лобби (снимок/переопределение создателем).
func lobbyCat(l models.Lobby, uid primitive.ObjectID) models.Category {
	for _, p := range l.Players {
		if p.UserID == uid {
			return p.Category
		}
	}
	return ""
}

// --- вспомогательные чистые функции ---

func hasPlayerLobby(l models.Lobby, uid primitive.ObjectID) bool {
	for _, p := range l.Players {
		if p.UserID == uid {
			return true
		}
	}
	return false
}

// teamOf возвращает team_id, в котором состоит игрок, или 0 если ни в одном.
func teamOf(l models.Lobby, uid primitive.ObjectID) int {
	for _, t := range l.Teams {
		for _, s := range t.Slots {
			if s.UserID == uid {
				return t.ID
			}
		}
	}
	return 0
}

func teamIndex(l models.Lobby, teamID int) int {
	for i, t := range l.Teams {
		if t.ID == teamID {
			return i
		}
	}
	return -1
}

func allCaptained(l models.Lobby) bool {
	if len(l.Teams) == 0 {
		return false
	}
	for _, t := range l.Teams {
		if t.CaptainID.IsZero() {
			return false
		}
	}
	return true
}

func allTeamsFull(l models.Lobby, slots int) bool {
	for _, t := range l.Teams {
		if len(t.Slots) < slots {
			return false
		}
	}
	return true
}

// advanceTurn передаёт ход следующей команде, у которой ещё есть свободные слоты.
func advanceTurn(l *models.Lobby, slots int) {
	n := len(l.Draft.Order)
	if n == 0 {
		return
	}
	for i := 1; i <= n; i++ {
		next := (l.Draft.Turn + i) % n
		idx := teamIndex(*l, l.Draft.Order[next])
		if idx >= 0 && len(l.Teams[idx].Slots) < slots {
			l.Draft.Turn = next
			return
		}
	}
	// все заполнены — оставляем как есть (драфт завершён)
}

package handlers

import (
	"context"
	"math/rand"
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

// BracketHandler обслуживает турнирную сетку и завершение турнира.
type BracketHandler struct {
	DB  *db.DB
	Cfg *config.Config
	Hub *ws.Hub
}

func NewBracketHandler(database *db.DB, cfg *config.Config, hub *ws.Hub) *BracketHandler {
	return &BracketHandler{DB: database, Cfg: cfg, Hub: hub}
}

func (h *BracketHandler) lobbies() *mongo.Collection { return h.DB.Collection("lobbies") }

// isManager — создатель ИЛИ админ.
func (h *BracketHandler) isManager(c *fiber.Ctx, l models.Lobby) bool {
	return l.CreatorID == middleware.UserID(c) || middleware.IsAdmin(c, h.Cfg.JWTSecret)
}

func (h *BracketHandler) load(c *fiber.Ctx, ctx context.Context) (models.Lobby, error) {
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

func intp(v int) *int { return &v }

// GenerateBracket — POST /lobby/:id/generate-bracket.
// Создатель формирует сетку single elimination из укомплектованных команд.
func (h *BracketHandler) GenerateBracket(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.load(c, ctx)
	if err != nil {
		return err
	}
	if !h.isManager(c, l) {
		return fiber.NewError(fiber.StatusForbidden, "сформировать сетку может только создатель или админ")
	}
	if l.Status != models.StatusDraft {
		return fiber.NewError(fiber.StatusConflict, "сетку можно сформировать только после драфта")
	}
	if !l.Draft.Picking {
		return fiber.NewError(fiber.StatusConflict, "драфт ещё не завершён")
	}

	bracket := buildBracket(l.Teams)
	l.Bracket = bracket
	l.Status = models.StatusActive

	_, err = h.lobbies().UpdateByID(ctx, l.ID, bson.M{"$set": bson.M{
		"status":  l.Status,
		"bracket": l.Bracket,
	}})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось сформировать сетку")
	}
	h.broadcast(l)
	return c.JSON(h.view(l))
}

// buildBracket строит single-elimination сетку с bye для не-степеней двойки.
func buildBracket(teams []models.Team) models.Bracket {
	n := len(teams)
	order := make([]int, n)
	for i, t := range teams {
		order[i] = t.ID
	}
	rand.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })

	size := nextPow2(n)
	matchCount := size / 2

	// Раунд 1: распределяем команды так, чтобы bye достались части матчей.
	first := models.Round{Matches: make([]models.Match, matchCount)}
	for i := 0; i < matchCount; i++ {
		m := models.Match{ID: i}
		if i < n {
			m.Team1 = intp(order[i]) // первые команды — в team1 каждого матча
		}
		first.Matches[i] = m
	}
	// Оставшиеся команды — в team2 первых матчей.
	for i := 0; i < n-matchCount; i++ {
		first.Matches[i].Team2 = intp(order[matchCount+i])
	}
	// Матчи с одной командой — bye: победитель сразу определён.
	for i := range first.Matches {
		m := &first.Matches[i]
		if m.Team1 != nil && m.Team2 == nil {
			m.Winner = intp(*m.Team1)
		}
	}

	rounds := []models.Round{first}
	// Пустые последующие раунды.
	for cnt := matchCount / 2; cnt >= 1; cnt /= 2 {
		r := models.Round{Matches: make([]models.Match, cnt)}
		idBase := totalMatches(rounds)
		for i := range r.Matches {
			r.Matches[i] = models.Match{ID: idBase + i}
		}
		rounds = append(rounds, r)
	}

	bracket := models.Bracket{Rounds: rounds}
	// Проводим bye-победителей в следующий раунд.
	for i := range bracket.Rounds[0].Matches {
		if w := bracket.Rounds[0].Matches[i].Winner; w != nil {
			propagate(&bracket, 0, i, *w)
		}
	}
	return bracket
}

func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

func totalMatches(rounds []models.Round) int {
	t := 0
	for _, r := range rounds {
		t += len(r.Matches)
	}
	return t
}

// propagate проводит победителя матча (round, idx) в соответствующий слот следующего раунда.
func propagate(b *models.Bracket, round, idx, winner int) {
	if round+1 >= len(b.Rounds) {
		return
	}
	next := idx / 2
	if next >= len(b.Rounds[round+1].Matches) {
		return
	}
	target := &b.Rounds[round+1].Matches[next]
	if idx%2 == 0 {
		target.Team1 = intp(winner)
	} else {
		target.Team2 = intp(winner)
	}
}

type resultReq struct {
	Score1 int `json:"score1"`
	Score2 int `json:"score2"`
}

// MatchResult — POST /lobby/:id/match/:match_id/result. Создатель вводит счёт.
func (h *BracketHandler) MatchResult(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.load(c, ctx)
	if err != nil {
		return err
	}
	if !h.isManager(c, l) {
		return fiber.NewError(fiber.StatusForbidden, "вводить результаты может только создатель или админ")
	}
	if l.Status != models.StatusActive {
		return fiber.NewError(fiber.StatusConflict, "турнир не активен")
	}

	matchID, err := c.ParamsInt("match_id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный id матча")
	}
	var req resultReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "неверный формат запроса")
	}
	if req.Score1 == req.Score2 {
		return fiber.NewError(fiber.StatusBadRequest, "в плей-офф не может быть ничьи")
	}

	round, idx := findMatch(l.Bracket, matchID)
	if round < 0 {
		return fiber.NewError(fiber.StatusNotFound, "матч не найден")
	}
	m := &l.Bracket.Rounds[round].Matches[idx]
	if m.Team1 == nil || m.Team2 == nil {
		return fiber.NewError(fiber.StatusConflict, "обе команды матча ещё не определены")
	}

	m.Score1 = req.Score1
	m.Score2 = req.Score2
	if req.Score1 > req.Score2 {
		m.Winner = intp(*m.Team1)
	} else {
		m.Winner = intp(*m.Team2)
	}
	propagate(&l.Bracket, round, idx, *m.Winner)

	_, err = h.lobbies().UpdateByID(ctx, l.ID, bson.M{"$set": bson.M{"bracket": l.Bracket}})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось сохранить результат")
	}
	h.broadcast(l)
	return c.JSON(h.view(l))
}

// Finish — POST /lobby/:id/finish. Создатель объявляет победителя турнира.
func (h *BracketHandler) Finish(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	l, err := h.load(c, ctx)
	if err != nil {
		return err
	}
	if !h.isManager(c, l) {
		return fiber.NewError(fiber.StatusForbidden, "завершить может только создатель или админ")
	}
	if l.Status != models.StatusActive {
		return fiber.NewError(fiber.StatusConflict, "турнир не активен")
	}

	finalWinner := bracketWinner(l.Bracket)
	if finalWinner == nil {
		return fiber.NewError(fiber.StatusConflict, "финал ещё не сыгран")
	}

	l.WinnerTeamID = finalWinner
	l.Status = models.StatusFinished

	_, err = h.lobbies().UpdateByID(ctx, l.ID, bson.M{"$set": bson.M{
		"status":         l.Status,
		"winner_team_id": l.WinnerTeamID,
	}})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "не удалось завершить турнир")
	}

	// Добавляем лобби в историю игр всех участников.
	h.addToHistory(ctx, l)

	h.broadcast(l)
	return c.JSON(h.view(l))
}

// bracketWinner возвращает победителя финального матча, если он сыгран.
func bracketWinner(b models.Bracket) *int {
	if len(b.Rounds) == 0 {
		return nil
	}
	final := b.Rounds[len(b.Rounds)-1]
	if len(final.Matches) != 1 {
		return nil
	}
	return final.Matches[0].Winner
}

// findMatch находит (round, index) матча по его глобальному id.
func findMatch(b models.Bracket, id int) (int, int) {
	for r := range b.Rounds {
		for i := range b.Rounds[r].Matches {
			if b.Rounds[r].Matches[i].ID == id {
				return r, i
			}
		}
	}
	return -1, -1
}

// addToHistory добавляет id лобби в game_history каждого игрока.
func (h *BracketHandler) addToHistory(ctx context.Context, l models.Lobby) {
	ids := make([]primitive.ObjectID, 0, len(l.Players))
	for _, p := range l.Players {
		ids = append(ids, p.UserID)
	}
	_, _ = h.DB.Collection("users").UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		bson.M{"$addToSet": bson.M{"game_history": l.ID}},
	)
}

func (h *BracketHandler) view(l models.Lobby) fiber.Map {
	return fiber.Map{
		"lobby_id":       l.ID.Hex(),
		"status":         l.Status,
		"teams":          l.Teams,
		"bracket":        l.Bracket,
		"winner_team_id": l.WinnerTeamID,
	}
}

func (h *BracketHandler) broadcast(l models.Lobby) {
	h.Hub.Broadcast(l.ID.Hex(), fiber.Map{"type": "bracket", "bracket_state": h.view(l)})
}

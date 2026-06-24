package handlers

import (
	"testing"

	"dvacatka/models"
)

func makeTeams(n int) []models.Team {
	t := make([]models.Team, n)
	for i := 0; i < n; i++ {
		t[i] = models.Team{ID: i + 1, Name: "T"}
	}
	return t
}

// simulateBracket доигрывает сетку: каждый готовый матч выигрывает team1.
// Возвращает id чемпиона.
func simulateBracket(b *models.Bracket) int {
	for {
		final := bracketWinner(*b)
		if final != nil {
			return *final
		}
		progressed := false
		for r := range b.Rounds {
			for i := range b.Rounds[r].Matches {
				m := &b.Rounds[r].Matches[i]
				if m.Winner == nil && m.Team1 != nil && m.Team2 != nil {
					m.Score1, m.Score2 = 16, 10
					w := *m.Team1
					m.Winner = &w
					propagate(b, r, i, w)
					progressed = true
				}
			}
		}
		if !progressed {
			panic("сетка застряла: нет готовых матчей и нет чемпиона")
		}
	}
}

func TestBuildBracket(t *testing.T) {
	for _, n := range []int{4, 6, 8} {
		b := buildBracket(makeTeams(n))

		// Первый раунд: size/2 матчей.
		size := nextPow2(n)
		if got := len(b.Rounds[0].Matches); got != size/2 {
			t.Fatalf("n=%d: первый раунд %d матчей, ожидалось %d", n, got, size/2)
		}

		// Число bye = size - n; столько матчей первого раунда должны иметь готового победителя.
		byes := 0
		for _, m := range b.Rounds[0].Matches {
			if m.Team1 != nil && m.Team2 == nil && m.Winner != nil {
				byes++
			}
		}
		if byes != size-n {
			t.Fatalf("n=%d: bye=%d, ожидалось %d", n, byes, size-n)
		}

		// Последний раунд — ровно один матч (финал).
		if got := len(b.Rounds[len(b.Rounds)-1].Matches); got != 1 {
			t.Fatalf("n=%d: финальный раунд %d матчей, ожидался 1", n, got)
		}

		// Доигрываем — должен получиться ровно один чемпион из числа команд.
		champ := simulateBracket(&b)
		if champ < 1 || champ > n {
			t.Fatalf("n=%d: некорректный чемпион %d", n, champ)
		}
		t.Logf("n=%d: раундов=%d, bye=%d, чемпион=%d — ok", n, len(b.Rounds), byes, champ)
	}
}

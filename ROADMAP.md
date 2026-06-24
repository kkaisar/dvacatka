# Dvacatka — Roadmap & ТЗ

Платформа для организации мини-турниров по CS среди друзей (~100 пользователей).

## Стек
- **Backend:** Go — Fiber или Gin, gorilla/websocket, MongoDB driver (`go.mongodb.org/mongo-driver`)
- **Database:** MongoDB Atlas (free tier M0, 512MB)
- **Frontend:** HTML + Tailwind CSS + Alpine.js / Vanilla JS, серверный рендеринг + HTMX
- **Деплой:** Railway.app или Render.com (free tier) + MongoDB Atlas

---

## Порядок работ (двигаемся сверху вниз)

- [x] **ФРОНТЕНД** (web/ статика, Tailwind CDN + ванильный JS + WS): login.html (вход/регистрация), index.html (меню + создание + список + история), lobby.html (все 4 этапа live), settings.html, player.html, static/app.js. Список лобби переехал на `GET /api/lobbies`; статика раздаётся `app.Static("/", "./web")`. Раздача и API протестированы ✅

- [x] **1. Подключение MongoDB + базовые модели** (`db/mongodb.go`, `models/user.go`, `models/lobby.go`) — каркас на Fiber, `config`, `/health`, `go.mod` собран ✅
- [x] **2. Авторизация** (без SMS) — `register`, `login`, `logout`, JWT в httpOnly-cookie, `RequireAuth`, bcrypt, уникальные индексы. Протестировано вживую ✅ (forgot-password перенесён в п.9)
- [x] **3. Профиль пользователя** — `GET /profile`, `PUT /profile/settings` (уникальность никнейма), публичный `GET /player/:id` с историей игр. Протестировано ✅
- [x] **4. Создание и просмотр лобби** — `POST /lobby/create` (любой авторизованный, создатель авто-входит), `GET /` (active+history), `GET /lobby/:id`, `DELETE /lobby/:id` (создатель, пока open). Протестировано ✅
- [x] **5. Вход/выход + галочки оплаты + WebSocket** — join/leave/toggle-paid/kick, ws-хаб с комнатами по лобби, broadcast при изменениях. Протестировано вживую (real-time) ✅
- [x] **6. Драфт** — start-draft, claim-captain/:team_id (Captain занимает слот), жеребьёвка порядка, pick/:user_id по очереди, draft-state + WS. Протестировано на 20 игроках ✅
- [x] **7. Турнирная сетка** — generate-bracket (single elim + bye для 6 команд), match/:id/result с авто-проходом победителей, draft/bracket в WS. Юнит-тест 4/6/8 команд + e2e на 4 ✅
- [x] **8. Результаты и история** (бэкенд) — finish объявляет чемпиона, пишет лобби в game_history всех игроков, переводит в историю; `GET /` отдаёт history, профиль — game_history ✅ (визуал 🏆 — на этапе фронта)
- [x] **9. Email восстановление пароля (SMTP)** — forgot-password (одинаковый ответ, не палит email), reset-password (одноразовый токен, TTL-индекс 1ч), страница reset.html, ссылка «Забыли пароль?» на входе. Без SMTP-кредов ссылка пишется в лог. Протестировано ✅ (Twilio SMS отпал — авторизация по email)
- [x] **10. Деплой на Railway + MongoDB Atlas** — Dockerfile (multi-stage, статич. бинарь + web/), .dockerignore, DEPLOY.md (GitHub и CLI варианты). Образ собран и контейнер проверен локально (подключился к Atlas, раздаёт сайт) ✅ — осталось залить на Railway по инструкции

---

## Модели данных (MongoDB)

### users
```js
{
  _id, phone, email, nickname, real_name,
  password_hash,
  category: "A"|"B"|"C"|"Captain",
  is_admin: bool, is_blocked: bool,
  created_at, game_history: [lobby_id]
}
```

### lobbies
```js
{
  _id, name, type: "dvacatka"|"tricatka"|"sorokovka",
  max_players: 20|30|40, team_count: 4|6|8,
  password?: string, status: "open"|"draft"|"active"|"finished",
  creator_id, payment_details: { phone, card },
  players: [{ user_id, paid: bool, team_id? }],
  teams: [{ id, name, captain_id, slots: [{ user_id, category }] }],
  bracket: { rounds: [{ matches: [{ team1, team2, score1, score2, winner }] }] },
  created_at, winner_team_id?
}
```

---

## Модули и функционал

### Авторизация (без SMS на старте)
- **/register:** телефон (уникальный), email (уникальный), пароль + подтверждение, никнейм, имя, категория (A/B/C/Captain).
  ⚠️ Только обычные пользователи. Выбора роли / админ-режима при регистрации НЕТ.
- **/login:** телефон ИЛИ email + пароль
- **/forgot-password:** email → ссылка сброса (SMTP) ИЛИ admin сбрасывает вручную

### Админ (отдельный, не часть регистрации) ✅ ГОТОВО
- Единый админ-аккаунт. Креды через ENV `ADMIN_PASSWORD`, отдельная cookie `admin_token` (JWT, 12ч), middleware `RequireAdmin`.
- Страница `/admin.html`: вход по паролю → список юзеров, создать, сбросить пароль, блок/разблок, удалить.
- Эндпоинты: POST /admin/login, /admin/logout, GET /admin/users, POST /admin/create-user, /admin/users/:id/reset-password, /admin/users/:id/block, DELETE /admin/users/:id. Протестировано ✅

### Профиль
- Первый вход → настройка профиля (никнейм уникальный, имя, категория)
- `/settings` → изменить в любой момент
- `/player/:id` (публичный) → имя, никнейм, категория, телефон, история игр

### Главное меню
- `[Создать Двацатку]` → 20 игроков, 4 команды по 5
- `[Создать Трицатку]` → 30 игроков, 6 команд по 5
- `[Создать Сороковку]` → 40 игроков, 8 команд по 5
- При создании: название, пароль (опц.), реквизиты оплаты (телефон/карта), кнопка "Отменить"
- Лобби может создать **любой авторизованный пользователь** (он становится creator_id)
- Список активных лобби (всем авторизованным) + раздел "История игр"

### Лобби — жизненный цикл
1. **СБОР (open):** список игроков + галочки оплаты (каждый сам ✅), создатель "Выгнать", любой "Выйти", real-time WS
2. **ДРАФТ (draft):** триггер "Начать драфт" (места заполнены) → жеребьёвка порядка → колонки команд → слот капитана только для "Captain" → пики по очереди Cap1→Cap2→... → кнопка "Добавить" только у текущего пикающего → фильтр по категории (опц.)
3. **СЕТКА (active):** триггер "Сформировать сетку" → single elimination → создатель вводит счёт → победитель проходит дальше → клик на игрока → профиль
4. **ЗАВЕРШЕНИЕ (finished):** "Объявить победителя" → 🏆 на сетке → лобби в историю

### Турнирная сетка
- 4 команды (Двацатка): полуфинал → финал → чемпион
- 6 команд (Трицатка): double round-robin или single elim с bye
- 8 команд (Сороковка): классический single elimination

---

## Файловая структура
```
dvacatka/
├── main.go
├── config/config.go          # ENV
├── db/mongodb.go             # подключение
├── models/{user,lobby}.go
├── handlers/{auth,user,lobby,draft,bracket,admin}.go
├── middleware/auth.go        # JWT
├── ws/hub.go                 # WebSocket
├── templates/{base,login,profile,home,lobby,draft,bracket}.html
├── static/{css,js}/
├── go.mod, go.sum, .env
```

---

## API endpoints

```
AUTH
  POST   /auth/register
  POST   /auth/login
  POST   /auth/logout
  POST   /auth/forgot-password

PROFILE
  GET    /profile
  PUT    /profile/settings
  GET    /player/:id

LOBBY
  GET    /                    # список лобби + история
  POST   /lobby/create
  GET    /lobby/:id
  POST   /lobby/:id/join
  POST   /lobby/:id/leave
  POST   /lobby/:id/toggle-paid
  DELETE /lobby/:id           # только создатель

DRAFT
  POST   /lobby/:id/start-draft
  POST   /lobby/:id/pick/:user_id
  GET    /lobby/:id/draft-state

BRACKET
  POST   /lobby/:id/generate-bracket
  POST   /lobby/:id/match/:match_id/result
  POST   /lobby/:id/finish

ADMIN
  GET    /admin
  POST   /admin/create-user
  GET    /admin/users
  POST   /admin/reset-password

WEBSOCKET
  WS     /ws/lobby/:id        # real-time обновления
```

---

## ENV (.env)
```
MONGODB_URI=mongodb+srv://...
JWT_SECRET=supersecretkey
ADMIN_PASSWORD=admin123
PORT=8080

# Email (один вариант)
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your@gmail.com
SMTP_PASS=xxxx xxxx xxxx xxxx
# или RESEND_API_KEY / Brevo (300 писем/день)
```

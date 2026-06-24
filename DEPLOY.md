# Деплой Dvacatka на Railway

Приложение собирается в Docker-образ (есть `Dockerfile`) и читает все настройки из переменных окружения. Локальный `.env` в образ НЕ попадает — переменные задаются в панели Railway.

## Шаг 0. Подготовка MongoDB Atlas
В Atlas → **Network Access** добавь `0.0.0.0/0` (Allow access from anywhere).
Railway использует динамические исходящие IP, поэтому без этого приложение не подключится к базе.

## Переменные окружения (задать в Railway → Variables)
| Переменная | Значение |
|---|---|
| `MONGODB_URI` | строка подключения Atlas (как в `.env`) |
| `MONGODB_DB` | `dvacatka` |
| `JWT_SECRET` | длинная случайная строка (смени дефолт!) |
| `ADMIN_PASSWORD` | твой админ-пароль (смени `admin123`!) |
| `SMTP_HOST` `SMTP_PORT` `SMTP_USER` `SMTP_PASS` | (опц.) для писем восстановления пароля |

`PORT` задавать НЕ нужно — Railway проставляет его сам, приложение его читает.

---

## Вариант A. Деплой через GitHub (рекомендуется)
1. Создай репозиторий на GitHub и запушь проект:
   ```bash
   git init
   git add .
   git commit -m "Dvacatka MVP"
   git branch -M main
   git remote add origin https://github.com/<ты>/dvacatka.git
   git push -u origin main
   ```
   (`.env` не запушится — он в `.gitignore`.)
2. На https://railway.app → **New Project → Deploy from GitHub repo** → выбери репозиторий.
3. Railway увидит `Dockerfile` и соберёт образ.
4. Вкладка **Variables** → добавь переменные из таблицы выше.
5. Вкладка **Settings → Networking → Generate Domain** — получишь публичный URL.

## Вариант B. Деплой через Railway CLI (без GitHub)
```bash
npm i -g @railway/cli
railway login
railway init
railway up            # зальёт текущую папку, соберёт Dockerfile
railway variables set MONGODB_URI="..." JWT_SECRET="..." ADMIN_PASSWORD="..."
railway domain        # сгенерирует публичный домен
```

---

## После деплоя
- Открой выданный домен — попадёшь на `/login.html`.
- Админка — `<домен>/admin.html`, вход по `ADMIN_PASSWORD`.
- Проверь health: `<домен>/health` → `{"status":"ok"}`.

## Локальный запуск для разработки
```bash
go run .          # http://localhost:8080 (нужен .env с MONGODB_URI)
```

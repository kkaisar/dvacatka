# --- Сборка ---
FROM golang:1.26-alpine AS build
WORKDIR /src

# Кэшируем зависимости.
COPY go.mod go.sum ./
RUN go mod download

# Собираем статический бинарь.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/dvacatka .

# --- Рантайм ---
FROM alpine:3.20
WORKDIR /app

# Сертификаты для TLS-соединения с MongoDB Atlas и SMTP.
RUN apk add --no-cache ca-certificates

COPY --from=build /app/dvacatka .
COPY web ./web

EXPOSE 8080
CMD ["./dvacatka"]

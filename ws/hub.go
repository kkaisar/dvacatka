package ws

import (
	"encoding/json"
	"sync"

	"github.com/gofiber/contrib/websocket"
)

// client — одно websocket-соединение с буфером исходящих сообщений.
type client struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub держит комнаты по lobbyID и рассылает события всем подписчикам комнаты.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[*client]struct{}
}

// NewHub создаёт пустой хаб.
func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*client]struct{})}
}

// Broadcast рассылает событие всем клиентам комнаты lobbyID.
func (h *Hub) Broadcast(lobbyID string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for cl := range h.rooms[lobbyID] {
		select {
		case cl.send <- data:
		default:
			// клиент не успевает читать — пропускаем, чтобы не блокировать
		}
	}
}

func (h *Hub) add(lobbyID string, cl *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[lobbyID] == nil {
		h.rooms[lobbyID] = make(map[*client]struct{})
	}
	h.rooms[lobbyID][cl] = struct{}{}
}

func (h *Hub) remove(lobbyID string, cl *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if room := h.rooms[lobbyID]; room != nil {
		delete(room, cl)
		if len(room) == 0 {
			delete(h.rooms, lobbyID)
		}
	}
}

// Handle обслуживает одно websocket-соединение для комнаты lobbyID.
// Используется как тело fiber websocket.New(...) хендлера.
func (h *Hub) Handle(c *websocket.Conn, lobbyID string) {
	cl := &client{conn: c, send: make(chan []byte, 16)}
	h.add(lobbyID, cl)

	// writer: пишем исходящие сообщения из канала
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range cl.send {
			if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	// reader: блокируемся на чтении, чтобы держать соединение и ловить закрытие
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}

	h.remove(lobbyID, cl)
	close(cl.send)
	<-done
}

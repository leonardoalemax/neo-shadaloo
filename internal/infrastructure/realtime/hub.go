package realtime

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	domain "neo-shadaloo/internal/domain/battlelog"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Hub manages WebSocket connections grouped by userID and implements
// domain.EventPublisher so the application layer can broadcast updates
// without knowing about WebSocket internals.
type Hub struct {
	mu      sync.Mutex
	clients map[string]map[*websocket.Conn]struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]map[*websocket.Conn]struct{}),
	}
}

// Publish implements domain.EventPublisher.
func (h *Hub) Publish(event domain.BattlelogSyncedEvent) {
	h.broadcast(event.UserID, map[string]any{
		"type":     "update",
		"cachedAt": event.CachedAt,
	})
}

// ServeWS upgrades an HTTP request to a WebSocket connection for userID.
func (h *Hub) ServeWS(userID string, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}
	defer conn.Close()

	h.register(userID, conn)
	defer h.unregister(userID, conn)

	conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		return nil
	})

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) broadcast(userID string, msg any) {
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.clients[userID]))
	for conn := range h.clients[userID] {
		conns = append(conns, conn)
	}
	h.mu.Unlock()

	for _, conn := range conns {
		if err := conn.WriteJSON(msg); err != nil {
			log.Printf("[ws] broadcast error for %s: %v", userID, err)
		}
	}
}

func (h *Hub) register(userID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[*websocket.Conn]struct{})
	}
	h.clients[userID][conn] = struct{}{}
}

func (h *Hub) unregister(userID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients[userID], conn)
	if len(h.clients[userID]) == 0 {
		delete(h.clients, userID)
	}
}

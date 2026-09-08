package wsserver

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub upgrades HTTP connections to WebSocket, dispatches incoming client
// messages to a DeviceController, and can broadcast server-initiated
// messages (device connection status) to every connected client.
type Hub struct {
	ctrl     DeviceController
	upgrader websocket.Upgrader

	mu      sync.Mutex
	clients map[*wsClient]struct{}
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

func NewHub(ctrl DeviceController) *Hub {
	return &Hub{
		ctrl:    ctrl,
		clients: make(map[*wsClient]struct{}),
		upgrader: websocket.Upgrader{
			// LAN-only, no auth by design (see design doc) — any origin
			// on the private network is allowed to connect.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("wsserver: upgrade failed: %v", err)
		return
	}

	c := &wsClient{conn: conn, send: make(chan []byte, 16)}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go h.writePump(c)
	h.sendInitialState(c)
	h.readPump(c)

	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	close(c.send)
}

func (h *Hub) sendInitialState(c *wsClient) {
	appsB, _ := json.Marshal(map[string]any{"type": "apps", "items": h.ctrl.Apps()})
	statusB, _ := json.Marshal(map[string]any{"type": "status", "connected": h.ctrl.Connected()})
	for _, b := range [][]byte{appsB, statusB} {
		select {
		case c.send <- b:
		default:
		}
	}
}

func (h *Hub) readPump(c *wsClient) {
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if err := Dispatch(context.Background(), h.ctrl, raw); err != nil {
			log.Printf("wsserver: dispatch error: %v", err)
		}
	}
}

func (h *Hub) writePump(c *wsClient) {
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			_ = c.conn.Close()
			return
		}
	}
	_ = c.conn.Close()
}

// BroadcastStatus notifies all connected clients of a device connection
// state change. Safe to call from adbctl's status callback.
func (h *Hub) BroadcastStatus(connected bool) {
	b, _ := json.Marshal(map[string]any{"type": "status", "connected": connected})
	h.broadcast(b)
}

func (h *Hub) broadcast(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- b:
		default:
		}
	}
}

package wsserver

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"projectorremote/internal/deviceio"
	"projectorremote/internal/playback"
)

// Hub upgrades HTTP connections to WebSocket, dispatches incoming client
// messages to a DeviceController, and can broadcast server-initiated
// messages (device connection status) to every connected client.
type Hub struct {
	ctrl     DeviceController
	pb       Playback
	upgrader websocket.Upgrader

	mu               sync.Mutex
	clients          map[*wsClient]struct{}
	settingsRevision atomic.Int64
	settingsMu       sync.RWMutex
	settingsCache    *deviceio.ProjectorSettings
	settingsErr      error
	settingsReady    chan struct{}
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
	done chan struct{}
}

func NewHub(ctrl DeviceController, pb Playback) *Hub {
	h := &Hub{
		ctrl:          ctrl,
		pb:            pb,
		clients:       make(map[*wsClient]struct{}),
		settingsReady: make(chan struct{}),
		upgrader: websocket.Upgrader{
			// LAN-only, no auth by design (see design doc) — any origin
			// on the private network is allowed to connect.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	// The vendor settings provider takes several seconds to read all fields.
	// Warm one snapshot when the server starts so opening or refreshing the PWA
	// never pays that cost.
	go h.warmSettingsCache()
	return h
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("wsserver: upgrade failed: %v", err)
		return
	}

	c := &wsClient{conn: conn, send: make(chan []byte, 16), done: make(chan struct{})}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go h.writePump(c)
	h.sendInitialState(c)
	go h.sendInitialSettings(c)
	h.readPump(c)

	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	close(c.done)
	_ = c.conn.Close()
}

func (h *Hub) sendInitialState(c *wsClient) {
	appsB, _ := json.Marshal(map[string]any{"type": "apps", "items": h.ctrl.Apps()})
	statusB, _ := json.Marshal(map[string]any{"type": "status", "connected": h.ctrl.Connected()})
	npB, _ := json.Marshal(map[string]any{"type": "now_playing", "session": h.pb.Snapshot()})
	msgs := [][]byte{appsB, statusB, npB}
	if vol, err := h.ctrl.Volume(); err == nil {
		volB, _ := json.Marshal(map[string]any{"type": "volume", "value": vol})
		msgs = append(msgs, volB)
	}
	for _, b := range msgs {
		select {
		case c.send <- b:
		default:
		}
	}
}

func (h *Hub) sendInitialSettings(c *wsClient) {
	<-h.settingsReady
	h.settingsMu.RLock()
	settings, err := h.settingsCache, h.settingsErr
	revision := h.settingsRevision.Load()
	if settings != nil {
		copy := cloneSettings(*settings)
		settings = &copy
	}
	h.settingsMu.RUnlock()
	if err != nil {
		h.sendSettingsError(c, err)
		return
	}
	if settings == nil {
		h.sendSettingsError(c, errors.New("projector settings cache unavailable"))
		return
	}
	b, _ := json.Marshal(map[string]any{"type": "projector_settings", "settings": settings, "revision": revision})
	h.sendTo(c, b)
}

func (h *Hub) warmSettingsCache() {
	started := time.Now()
	for {
		revision := h.settingsRevision.Load()
		settings, err := h.ctrl.ProjectorSettings()
		if err != nil {
			h.settingsMu.Lock()
			h.settingsErr = err
			h.settingsMu.Unlock()
			break
		}
		if revision != h.settingsRevision.Load() {
			continue
		}
		h.settingsMu.Lock()
		copy := cloneSettings(settings)
		h.settingsCache = &copy
		h.settingsMu.Unlock()
		break
	}
	close(h.settingsReady)
	log.Printf("wsserver: projector settings cache ready in %s", time.Since(started).Round(time.Millisecond))
}

func cloneSettings(settings deviceio.ProjectorSettings) deviceio.ProjectorSettings {
	features := make(map[string]bool, len(settings.Features))
	for key, value := range settings.Features {
		features[key] = value
	}
	settings.Features = features
	corners := make(map[string]deviceio.CornerPoint, len(settings.Corners))
	for key, value := range settings.Corners {
		corners[key] = value
	}
	settings.Corners = corners
	return settings
}

func (h *Hub) readPump(c *wsClient) {
	defer func() { _ = h.ctrl.StopFocus() }()
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		started := time.Now()
		if err := Dispatch(context.Background(), h.ctrl, h.pb, raw); err != nil {
			log.Printf("wsserver: dispatch error: %v", err)
			if isProjectorCommand(raw) {
				h.sendSettingsError(c, err)
			}
			continue
		}
		if isProjectorCommand(raw) {
			log.Printf("wsserver: projector command type=%s applied_in=%s", messageType(raw), time.Since(started).Round(time.Millisecond))
		}
		h.maybeBroadcastVolume(raw)
		h.maybeBroadcastSettings(raw)
	}
}

func messageType(raw []byte) string {
	var peek struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &peek) != nil {
		return ""
	}
	return peek.Type
}

func isProjectorCommand(raw []byte) bool {
	switch messageType(raw) {
	case "set_zoom", "set_keystone", "set_corner", "nudge_corner", "reset_keystone", "set_projection_mode", "set_projection_feature", "focus_start", "focus_stop":
		return true
	default:
		return false
	}
}

func (h *Hub) sendSettingsError(c *wsClient, err error) {
	b, _ := json.Marshal(map[string]any{"type": "settings_error", "message": err.Error()})
	h.sendTo(c, b)
}

func (h *Hub) sendTo(c *wsClient, b []byte) {
	select {
	case c.send <- b:
	case <-c.done:
	default:
	}
}

func (h *Hub) maybeBroadcastSettings(raw []byte) {
	var msg incoming
	if json.Unmarshal(raw, &msg) != nil {
		return
	}
	patch := map[string]any{}
	switch msg.Type {
	case "set_zoom":
		patch["zoom"] = msg.Percent
	case "set_keystone":
		patch[msg.Axis] = msg.Angle
	case "set_projection_mode":
		patch["mode"] = msg.Mode
	case "set_projection_feature":
		patch["features"] = map[string]bool{msg.Key: msg.Enabled}
	case "set_corner":
		patch["corners"] = map[string]deviceio.CornerPoint{msg.Corner: {X: msg.X, Y: msg.Y}}
	case "reset_keystone":
		patch["zoom"] = 100
		patch["corners"] = map[string]deviceio.CornerPoint{
			"lt": {}, "rt": {}, "rb": {}, "lb": {},
		}
	default:
		return
	}
	revision := h.applySettingsPatch(msg)
	b, _ := json.Marshal(map[string]any{"type": "projector_settings_patch", "patch": patch, "revision": revision})
	h.broadcast(b)
}

func (h *Hub) applySettingsPatch(msg incoming) int64 {
	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()
	revision := h.settingsRevision.Add(1)
	if h.settingsCache == nil {
		return revision
	}
	settings := h.settingsCache
	switch msg.Type {
	case "set_zoom":
		settings.Zoom = msg.Percent
	case "set_keystone":
		if msg.Axis == "horizontal" {
			settings.Horizontal = msg.Angle
		} else if msg.Axis == "vertical" {
			settings.Vertical = msg.Angle
		}
	case "set_projection_mode":
		settings.Mode = msg.Mode
	case "set_projection_feature":
		settings.Features[msg.Key] = msg.Enabled
	case "set_corner":
		settings.Corners[msg.Corner] = deviceio.CornerPoint{X: msg.X, Y: msg.Y}
	case "reset_keystone":
		settings.Zoom = 100
		for _, corner := range []string{"lt", "rt", "rb", "lb"} {
			settings.Corners[corner] = deviceio.CornerPoint{}
		}
	}
	return revision
}

func (h *Hub) broadcastSettingsSnapshot(revision int64) {
	settings, err := h.ctrl.ProjectorSettings()
	if err != nil || revision != h.settingsRevision.Load() {
		return
	}
	b, _ := json.Marshal(map[string]any{"type": "projector_settings", "settings": settings, "revision": revision})
	h.broadcast(b)
}

// maybeBroadcastVolume re-reads the device's volume and broadcasts it
// to every connected client after a successful set_volume dispatch,
// so simultaneous viewers' sliders stay in sync with each other (the
// LAN has no per-client isolation elsewhere either, e.g. Apps()).
func (h *Hub) maybeBroadcastVolume(raw []byte) {
	var peek struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil || peek.Type != "set_volume" {
		return
	}
	vol, err := h.ctrl.Volume()
	if err != nil {
		return
	}
	b, _ := json.Marshal(map[string]any{"type": "volume", "value": vol})
	h.broadcast(b)
}

func (h *Hub) writePump(c *wsClient) {
	for {
		select {
		case msg := <-c.send:
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				_ = c.conn.Close()
				return
			}
		case <-c.done:
			return
		}
	}
}

// BroadcastStatus notifies all connected clients of a device connection
// state change.
func (h *Hub) BroadcastStatus(connected bool) {
	b, _ := json.Marshal(map[string]any{"type": "status", "connected": connected})
	h.broadcast(b)
}

// BroadcastNowPlaying sends the playback session (nil once nothing is
// playing) to every connected phone.
func (h *Hub) BroadcastNowPlaying(np *playback.NowPlaying) {
	b, _ := json.Marshal(map[string]any{"type": "now_playing", "session": np})
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

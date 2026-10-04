package playback

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"projectorremote/internal/catalog"
	"projectorremote/internal/history"
)

// ServePlayer is the /player WebSocket the player app connects to. It
// only accepts connections from this machine: the remote has no auth,
// so anything on the LAN could otherwise pose as the player. A new
// connection replaces the previous one.
func (m *Manager) ServePlayer(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		http.Error(w, "player connections are local only", http.StatusForbidden)
		return
	}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("playback: player upgrade: %v", err)
		return
	}
	pc := &wsPlayer{conn: conn}
	m.attach(pc)
	defer m.detach(pc)
	defer conn.Close()
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		m.handlePlayer(pc, raw)
	}
}

type wsPlayer struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (p *wsPlayer) Send(v any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return p.conn.WriteJSON(v)
}

func (p *wsPlayer) Close() error { return p.conn.Close() }

func (m *Manager) attach(c playerConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.player != nil && m.player != c {
		_ = m.player.Close()
	}
	m.player = c
}

func (m *Manager) detach(c playerConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.player == c {
		m.player = nil
	}
}

func (m *Manager) handlePlayer(conn playerConn, raw []byte) {
	var msg playerMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		log.Printf("playback: bad player message: %v", err)
		return
	}
	m.mu.Lock()
	if conn != m.player {
		m.mu.Unlock()
		return
	}
	s := m.sess
	// Messages about something other than the current stream (a state
	// report racing a new load) are stale.
	current := s != nil && s.delivered && s.np.Status != "error" &&
		(msg.ContentID == "" || msg.ContentID == s.np.ContentID)

	switch msg.Type {
	case "hello":
		switch {
		case s == nil:
			m.sendLocked(map[string]any{"type": "stop"})
		case s.stream == nil:
			// Still resolving: Play sends the load when it's ready.
		case msg.ContentID == s.np.ContentID:
			// Reconnected mid-play; it already has the stream.
			s.delivered = true
			if s.connectTimer != nil {
				s.connectTimer.Stop()
			}
		default:
			m.sendLoadLocked()
		}
	case "state":
		if current {
			m.applyStateLocked(s, msg)
		}
	case "ended":
		if current {
			m.endedLocked(s)
		}
	case "error":
		if !current {
			break
		}
		// NetMirror playlists can understate the media's real length, and
		// the player then errors out past the stated end (Media3's
		// stuck-player timeout). That's the episode finishing, not a
		// broken stream.
		if s.np.Dur > 0 && float64(msg.Pos) >= history.FinishedRatio*float64(s.np.Dur) {
			m.endedLocked(s)
			break
		}
		now := m.d.Clock.Now()
		retryLimit := maxRetries
		if s.np.OTT == "anime" {
			retryLimit = 5
		}
		if s.retries >= retryLimit || (s.np.OTT != "anime" && !s.lastRetry.IsZero() && now.Sub(s.lastRetry) <= retryWindow) {
			m.failLocked("The stream stopped working. Try playing it again.")
			break
		}
		// Most likely the signed stream URLs expired mid-play (they
		// carry a timestamp): resolve a fresh stream once and carry on
		// from the same spot.
		log.Printf("playback: player error (http %d): %s; re-resolving", msg.HTTP, msg.Message)
		s.lastRetry = now
		s.retries++
		s.np.Status = "loading"
		s.delivered = false
		m.publishLocked()
		gen, ott, id := s.gen, s.np.OTT, s.np.ContentID
		m.mu.Unlock()
		m.reResolve(gen, ott, id, msg.Pos)
		return
	case "closed":
		if s != nil {
			m.endLocked()
			m.publishLocked()
		}
	case "cancel_autoplay":
		m.cancelCountdownLocked()
	default:
		log.Printf("playback: unknown player message %q", msg.Type)
	}
	m.mu.Unlock()
}

func (m *Manager) applyStateLocked(s *session, msg playerMsg) {
	prev := s.np.Status
	if msg.Status != "" {
		s.np.Status = msg.Status
	}
	s.np.Pos, s.np.Dur, s.np.Selected = msg.Pos, msg.Dur, msg.Selected
	if msg.Tracks != nil {
		t := *msg.Tracks
		if t.Audio == nil {
			t.Audio = []Track{}
		}
		if t.Text == nil {
			t.Text = []Track{}
		}
		if t.Video == nil {
			t.Video = []VideoTrack{}
		}
		s.np.Tracks = t
	}
	now := m.d.Clock.Now()
	pausedNow := s.np.Status == "paused" && prev != "paused"
	if s.np.Dur > 0 && (s.lastSave.IsZero() || now.Sub(s.lastSave) >= saveEvery || pausedNow) {
		m.d.History.SavePosition(s.np.OTT, s.np.ContentID, s.np.Pos, s.np.Dur)
		s.lastSave = now
	}
	m.publishLocked()
}

func (m *Manager) endedLocked(s *session) {
	s.np.Status = "ended"
	if s.np.Dur > 0 {
		s.np.Pos = s.np.Dur
		m.d.History.SavePosition(s.np.OTT, s.np.ContentID, s.np.Pos, s.np.Dur)
	}
	m.finishedLocked(s)
	if s.next != nil {
		m.startCountdownLocked(s)
	}
	m.publishLocked()
}

func (m *Manager) reResolve(gen int, ott, id string, pos int64) {
	m.mu.Lock()
	if m.sess == nil || m.sess.gen != gen {
		m.mu.Unlock()
		return
	}
	failed := m.sess.stream
	m.mu.Unlock()
	var stream *catalog.Stream
	var err error
	if recovery, ok := m.d.Catalog.(interface {
		Recover(context.Context, string, string, *catalog.Stream) (*catalog.Stream, error)
	}); ok {
		stream, err = recovery.Recover(context.Background(), ott, id, failed)
	} else {
		stream, err = m.d.Catalog.Resolve(context.Background(), ott, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sess
	if s == nil || s.gen != gen {
		return
	}
	if err != nil {
		m.failErrLocked(err)
		return
	}
	s.stream, s.startPos = stream, pos
	m.sendLoadLocked()
	m.publishLocked()
}

package playback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestServePlayer_RejectsNonLoopback(t *testing.T) {
	r := newRig(t)
	req := httptest.NewRequest(http.MethodGet, "/player", nil)
	req.RemoteAddr = "192.168.1.20:40000"
	rec := httptest.NewRecorder()
	r.m.ServePlayer(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}
}

// End to end over a real WebSocket: the app connects, says hello, and
// receives the queued load.
func TestServePlayer_DeliversQueuedLoad(t *testing.T) {
	r := newRig(t)
	r.play(t, "nf", "movie", "", "")

	srv := httptest.NewServer(http.HandlerFunc(r.m.ServePlayer))
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{"type": "hello", "version": 1}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var msg map[string]any
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatal(err)
	}
	if msg["type"] != "load" || msg["contentId"] != "movie" {
		t.Fatalf("got %v, want the movie's load", msg)
	}
}

func TestDisconnectedPlayerGetsNoCommands(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	_ = r.m.Play(context.Background(), "nf", "movie", "", "")
	r.m.detach(r.player)
	if err := r.m.SetPaused(true); err == nil {
		t.Fatal("SetPaused succeeded with no player connected")
	}
}

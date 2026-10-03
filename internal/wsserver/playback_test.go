package wsserver

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"projectorremote/internal/playback"
)

type mockPlayback struct {
	calls    chan string
	snapshot *playback.NowPlaying
}

func newMockPlayback() *mockPlayback { return &mockPlayback{calls: make(chan string, 32)} }

func (m *mockPlayback) note(format string, args ...any) { m.calls <- fmt.Sprintf(format, args...) }

func (m *mockPlayback) Play(_ context.Context, ott, titleID, seasonID, episodeID string) error {
	m.note("play:%s/%s/%s/%s", ott, titleID, seasonID, episodeID)
	return nil
}
func (m *mockPlayback) SetPaused(p bool) error { m.note("paused:%v", p); return nil }
func (m *mockPlayback) SeekTo(to int64) error  { m.note("seek:%d", to); return nil }
func (m *mockPlayback) Skip(by int64) error    { m.note("skip:%d", by); return nil }
func (m *mockPlayback) SelectTrack(kind, id string) error {
	m.note("track:%s/%s", kind, id)
	return nil
}
func (m *mockPlayback) Next(context.Context) error        { m.note("next"); return nil }
func (m *mockPlayback) Stop()                             { m.note("stop") }
func (m *mockPlayback) CancelAutoplay()                   { m.note("cancel_autoplay") }
func (m *mockPlayback) AutoplayNow(context.Context) error { m.note("autoplay_now"); return nil }
func (m *mockPlayback) Snapshot() *playback.NowPlaying    { return m.snapshot }

func TestDispatch_PlaybackCommands(t *testing.T) {
	cases := map[string]string{
		`{"type":"play","ott":"nf","titleId":"show","seasonId":"s1","episodeId":"e2"}`: "play:nf/show/s1/e2",
		`{"type":"play","ott":"pv","titleId":"movie"}`:                                 "play:pv/movie//",
		`{"type":"set_paused","paused":true}`:                                          "paused:true",
		`{"type":"seek","to":42000}`:                                                   "seek:42000",
		`{"type":"skip","by":-10000}`:                                                  "skip:-10000",
		`{"type":"select_track","kind":"text","id":"off"}`:                             "track:text/off",
		`{"type":"next"}`:                                                              "next",
		`{"type":"stop"}`:                                                              "stop",
		`{"type":"cancel_autoplay"}`:                                                   "cancel_autoplay",
		`{"type":"autoplay_now"}`:                                                      "autoplay_now",
	}
	for raw, want := range cases {
		pb := newMockPlayback()
		if err := Dispatch(context.Background(), newMockCtrl(), pb, []byte(raw)); err != nil {
			t.Errorf("%s: %v", raw, err)
			continue
		}
		select {
		case got := <-pb.calls:
			if got != want {
				t.Errorf("%s: got %q, want %q", raw, got, want)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("%s: no call", raw)
		}
	}
}

func TestHub_SendsNowPlayingOnConnect(t *testing.T) {
	pb := newMockPlayback()
	pb.snapshot = &playback.NowPlaying{Title: "A Show", Status: "playing"}
	hub := NewHub(newMockCtrl(), pb)
	srv := httptest.NewServer(hub)
	defer srv.Close()
	conn := dialWS(t, srv)
	defer conn.Close()

	for range 5 {
		typ, m := readTyped(t, conn)
		if typ != "now_playing" {
			continue
		}
		session, _ := m["session"].(map[string]any)
		if session["title"] != "A Show" {
			t.Fatalf("now_playing = %v", m)
		}
		return
	}
	t.Fatal("no now_playing in the initial state")
}

func TestHub_BroadcastNowPlaying(t *testing.T) {
	hub := NewHub(newMockCtrl(), newMockPlayback())
	srv := httptest.NewServer(hub)
	defer srv.Close()
	conn := dialWS(t, srv)
	defer conn.Close()
	for range 5 {
		readTyped(t, conn)
	}

	hub.BroadcastNowPlaying(nil)
	typ, m := readTyped(t, conn)
	if typ != "now_playing" || m["session"] != nil {
		t.Fatalf("got %s %v, want now_playing with null session", typ, m)
	}
}

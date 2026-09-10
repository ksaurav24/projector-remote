package wsserver

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func readTyped(t *testing.T, conn *websocket.Conn) (string, map[string]any) {
	t.Helper()
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	typ, _ := m["type"].(string)
	return typ, m
}

func TestHub_SendsInitialAppsAndStatusOnConnect(t *testing.T) {
	ctrl := newMockCtrl()
	ctrl.connected = true
	ctrl.volume = 55
	hub := NewHub(ctrl)
	srv := httptest.NewServer(hub)
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.Close()

	seen := map[string]map[string]any{}
	for i := 0; i < 3; i++ {
		typ, m := readTyped(t, conn)
		seen[typ] = m
	}

	appsMsg, ok := seen["apps"]
	if !ok {
		t.Fatal("did not receive an apps message")
	}
	items, _ := appsMsg["items"].([]any)
	if len(items) != 1 || items[0] != "com.netflix.ninja" {
		t.Errorf("apps items = %v", items)
	}

	statusMsg, ok := seen["status"]
	if !ok {
		t.Fatal("did not receive a status message")
	}
	if connected, _ := statusMsg["connected"].(bool); !connected {
		t.Errorf("status connected = %v, want true", statusMsg["connected"])
	}

	volMsg, ok := seen["volume"]
	if !ok {
		t.Fatal("did not receive a volume message")
	}
	if v, _ := volMsg["value"].(float64); v != 55 {
		t.Errorf("volume value = %v, want 55", volMsg["value"])
	}
}

func TestHub_BroadcastsVolumeAfterSetVolume(t *testing.T) {
	ctrl := newMockCtrl()
	hub := NewHub(ctrl)
	srv := httptest.NewServer(hub)
	defer srv.Close()

	conn1 := dialWS(t, srv)
	defer conn1.Close()
	conn2 := dialWS(t, srv)
	defer conn2.Close()

	for i := 0; i < 3; i++ {
		readTyped(t, conn1)
		readTyped(t, conn2)
	}

	if err := conn1.WriteJSON(map[string]any{"type": "set_volume", "level": 30}); err != nil {
		t.Fatalf("write: %v", err)
	}

	for i, c := range []*websocket.Conn{conn1, conn2} {
		typ, m := readTyped(t, c)
		if typ != "volume" {
			t.Fatalf("client %d got type %q, want volume", i, typ)
		}
		if v, _ := m["value"].(float64); v != 30 {
			t.Errorf("client %d volume = %v, want 30", i, m["value"])
		}
	}
}

func TestHub_DispatchesClientMessages(t *testing.T) {
	ctrl := newMockCtrl()
	hub := NewHub(ctrl)
	srv := httptest.NewServer(hub)
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.Close()
	readTyped(t, conn) // apps
	readTyped(t, conn) // status

	if err := conn.WriteJSON(map[string]any{"type": "key", "code": "KEYCODE_VOLUME_UP"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case call := <-ctrl.calls:
		if call != "key:KEYCODE_VOLUME_UP" {
			t.Errorf("got call %q, want key:KEYCODE_VOLUME_UP", call)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dispatch")
	}
}

func TestHub_BroadcastsStatusToAllClients(t *testing.T) {
	ctrl := newMockCtrl()
	hub := NewHub(ctrl)
	srv := httptest.NewServer(hub)
	defer srv.Close()

	conn1 := dialWS(t, srv)
	defer conn1.Close()
	conn2 := dialWS(t, srv)
	defer conn2.Close()

	for i := 0; i < 3; i++ {
		readTyped(t, conn1)
		readTyped(t, conn2)
	}

	hub.BroadcastStatus(false)

	for i, c := range []*websocket.Conn{conn1, conn2} {
		typ, m := readTyped(t, c)
		if typ != "status" {
			t.Fatalf("client %d got type %q, want status", i, typ)
		}
		if connected, _ := m["connected"].(bool); connected {
			t.Errorf("client %d connected = %v, want false", i, connected)
		}
	}
}

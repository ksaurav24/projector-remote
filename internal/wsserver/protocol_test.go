package wsserver

import (
	"context"
	"sync"
	"testing"
)

type mockCtrl struct {
	mu        sync.Mutex
	keyEvents []string
	texts     []string
	launches  []string
	moves     [][2]int
	buttons   []bool
	refreshed bool
	connected bool
	calls     chan string
}

func newMockCtrl() *mockCtrl {
	return &mockCtrl{calls: make(chan string, 32)}
}

func (m *mockCtrl) note(s string) {
	select {
	case m.calls <- s:
	default:
	}
}

func (m *mockCtrl) KeyEvent(code string) error {
	m.mu.Lock()
	m.keyEvents = append(m.keyEvents, code)
	m.mu.Unlock()
	m.note("key:" + code)
	return nil
}

func (m *mockCtrl) Text(s string) error {
	m.mu.Lock()
	m.texts = append(m.texts, s)
	m.mu.Unlock()
	m.note("text:" + s)
	return nil
}

func (m *mockCtrl) LaunchApp(pkg string) error {
	m.mu.Lock()
	m.launches = append(m.launches, pkg)
	m.mu.Unlock()
	m.note("launch:" + pkg)
	return nil
}

func (m *mockCtrl) PointerMove(dx, dy int) error {
	m.mu.Lock()
	m.moves = append(m.moves, [2]int{dx, dy})
	m.mu.Unlock()
	m.note("move")
	return nil
}

func (m *mockCtrl) PointerButton(down bool) error {
	m.mu.Lock()
	m.buttons = append(m.buttons, down)
	m.mu.Unlock()
	m.note("button")
	return nil
}

func (m *mockCtrl) Apps() []string { return []string{"com.netflix.ninja"} }

func (m *mockCtrl) RefreshApps(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	m.refreshed = true
	m.mu.Unlock()
	m.note("refresh")
	return m.Apps(), nil
}

func (m *mockCtrl) Connected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func TestDispatch_Move(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, []byte(`{"type":"move","dx":4,"dy":-2}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.moves) != 1 || m.moves[0] != [2]int{4, -2} {
		t.Errorf("got %v", m.moves)
	}
}

func TestDispatch_PointerDownUp(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, []byte(`{"type":"pointerdown"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := Dispatch(context.Background(), m, []byte(`{"type":"pointerup"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.buttons) != 2 || m.buttons[0] != true || m.buttons[1] != false {
		t.Errorf("got %v", m.buttons)
	}
}

func TestDispatch_Key(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, []byte(`{"type":"key","code":"KEYCODE_VOLUME_UP"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.keyEvents) != 1 || m.keyEvents[0] != "KEYCODE_VOLUME_UP" {
		t.Errorf("got %v", m.keyEvents)
	}
}

func TestDispatch_Text(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, []byte(`{"type":"text","value":"star wars"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.texts) != 1 || m.texts[0] != "star wars" {
		t.Errorf("got %v", m.texts)
	}
}

func TestDispatch_Launch(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, []byte(`{"type":"launch","pkg":"com.netflix.ninja"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.launches) != 1 || m.launches[0] != "com.netflix.ninja" {
		t.Errorf("got %v", m.launches)
	}
}

func TestDispatch_RefreshApps(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, []byte(`{"type":"refresh_apps"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.refreshed {
		t.Error("expected RefreshApps to be called")
	}
}

func TestDispatch_MalformedJson(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, []byte(`not json`)); err == nil {
		t.Error("expected error for malformed json")
	}
}

func TestDispatch_UnknownType(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, []byte(`{"type":"nope"}`)); err == nil {
		t.Error("expected error for unknown message type")
	}
}

package wsserver

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"projectorremote/internal/deviceio"
)

type mockCtrl struct {
	mu            sync.Mutex
	keyEvents     []string
	texts         []string
	launches      []string
	moves         [][2]int
	buttons       []bool
	refreshed     bool
	connected     bool
	volume        int
	volumeErr     error
	setVolumeLog  []int
	settings      deviceio.ProjectorSettings
	settingsDelay time.Duration
	zoomLog       []int
	keystoneLog   []string
	cornerLog     []string
	modeLog       []string
	featureLog    []string
	focusLog      []string
	calls         chan string
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

func (m *mockCtrl) Volume() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.volume, m.volumeErr
}

func (m *mockCtrl) SetVolume(level int) error {
	m.mu.Lock()
	m.volume = level
	m.setVolumeLog = append(m.setVolumeLog, level)
	m.mu.Unlock()
	m.note("set_volume")
	return nil
}

func (m *mockCtrl) ProjectorSettings() (deviceio.ProjectorSettings, error) {
	if m.settingsDelay > 0 {
		time.Sleep(m.settingsDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings, nil
}

func (m *mockCtrl) SetZoom(percent int) error {
	m.mu.Lock()
	m.zoomLog = append(m.zoomLog, percent)
	m.settings.Zoom = percent
	m.mu.Unlock()
	return nil
}

func (m *mockCtrl) SetKeystone(axis string, angle int) error {
	m.mu.Lock()
	m.keystoneLog = append(m.keystoneLog, axis+":"+strconv.Itoa(angle))
	if axis == "horizontal" {
		m.settings.Horizontal = angle
	} else {
		m.settings.Vertical = angle
	}
	m.mu.Unlock()
	return nil
}

func (m *mockCtrl) NudgeCorner(corner, direction string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cornerLog = append(m.cornerLog, corner+":"+direction)
	return nil
}

func (m *mockCtrl) SetCorner(corner string, x, y int, final bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cornerLog = append(m.cornerLog, fmt.Sprintf("%s:%d:%d:%t", corner, x, y, final))
	if m.settings.Corners == nil {
		m.settings.Corners = map[string]deviceio.CornerPoint{}
	}
	m.settings.Corners[corner] = deviceio.CornerPoint{X: x, Y: y}
	return nil
}

func (m *mockCtrl) ResetKeystone() error { m.note("reset_keystone"); return nil }

func (m *mockCtrl) SetProjectionMode(mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modeLog = append(m.modeLog, mode)
	m.settings.Mode = mode
	return nil
}

func (m *mockCtrl) SetProjectionFeature(key string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.featureLog = append(m.featureLog, fmt.Sprintf("%s:%t", key, enabled))
	if m.settings.Features == nil {
		m.settings.Features = map[string]bool{}
	}
	m.settings.Features[key] = enabled
	return nil
}

func (m *mockCtrl) StartFocus(direction string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.focusLog = append(m.focusLog, direction)
	return nil
}

func (m *mockCtrl) StopFocus() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.focusLog = append(m.focusLog, "stop")
	return nil
}

func TestDispatch_Move(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`{"type":"move","dx":4,"dy":-2}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.moves) != 1 || m.moves[0] != [2]int{4, -2} {
		t.Errorf("got %v", m.moves)
	}
}

func TestDispatch_PointerDownUp(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`{"type":"pointerdown"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`{"type":"pointerup"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.buttons) != 2 || m.buttons[0] != true || m.buttons[1] != false {
		t.Errorf("got %v", m.buttons)
	}
}

func TestDispatch_Key(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`{"type":"key","code":"KEYCODE_VOLUME_UP"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.keyEvents) != 1 || m.keyEvents[0] != "KEYCODE_VOLUME_UP" {
		t.Errorf("got %v", m.keyEvents)
	}
}

func TestDispatch_Text(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`{"type":"text","value":"star wars"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.texts) != 1 || m.texts[0] != "star wars" {
		t.Errorf("got %v", m.texts)
	}
}

func TestDispatch_Launch(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`{"type":"launch","pkg":"com.netflix.ninja"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.launches) != 1 || m.launches[0] != "com.netflix.ninja" {
		t.Errorf("got %v", m.launches)
	}
}

func TestDispatch_RefreshApps(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`{"type":"refresh_apps"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.refreshed {
		t.Error("expected RefreshApps to be called")
	}
}

func TestDispatch_SetVolume(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`{"type":"set_volume","level":42}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.setVolumeLog) != 1 || m.setVolumeLog[0] != 42 {
		t.Errorf("got %v, want [42]", m.setVolumeLog)
	}
}

func TestDispatch_ProjectorCommands(t *testing.T) {
	m := newMockCtrl()
	cases := []string{
		`{"type":"set_zoom","percent":62}`,
		`{"type":"set_keystone","axis":"horizontal","angle":-3}`,
		`{"type":"nudge_corner","corner":"rt","direction":"down"}`,
		`{"type":"set_corner","corner":"lt","x":12,"y":8,"final":true}`,
		`{"type":"set_projection_mode","mode":"ceiling-rear"}`,
		`{"type":"set_projection_feature","key":"autoFocus","enabled":true}`,
		`{"type":"focus_start","direction":"near"}`,
		`{"type":"focus_stop"}`,
	}
	for _, raw := range cases {
		if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(raw)); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	if fmt.Sprint(m.zoomLog) != "[62]" || fmt.Sprint(m.keystoneLog) != "[horizontal:-3]" || fmt.Sprint(m.cornerLog) != "[rt:down lt:12:8:true]" {
		t.Fatalf("unexpected projector calls: zoom=%v keystone=%v corner=%v", m.zoomLog, m.keystoneLog, m.cornerLog)
	}
	if fmt.Sprint(m.modeLog) != "[ceiling-rear]" || fmt.Sprint(m.featureLog) != "[autoFocus:true]" || fmt.Sprint(m.focusLog) != "[near stop]" {
		t.Fatalf("unexpected mode/feature/focus calls: %v %v %v", m.modeLog, m.featureLog, m.focusLog)
	}
}

func TestDispatch_MalformedJson(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`not json`)); err == nil {
		t.Error("expected error for malformed json")
	}
}

func TestDispatch_UnknownType(t *testing.T) {
	m := newMockCtrl()
	if err := Dispatch(context.Background(), m, newMockPlayback(), []byte(`{"type":"nope"}`)); err == nil {
		t.Error("expected error for unknown message type")
	}
}

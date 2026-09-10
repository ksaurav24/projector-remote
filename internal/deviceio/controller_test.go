package deviceio

import "testing"

type fakeEventWriter struct {
	events []inputEvent
}

func (f *fakeEventWriter) WriteEvent(typ, code uint16, value int32) error {
	f.events = append(f.events, inputEvent{Type: typ, Code: code, Value: value})
	return nil
}

type fakeTextRunner struct {
	typed []string
}

func (f *fakeTextRunner) RunText(s string) error {
	f.typed = append(f.typed, s)
	return nil
}

func TestKeyEvent_ValidCode(t *testing.T) {
	kb, mouse := &fakeEventWriter{}, &fakeEventWriter{}
	c := newController(kb, mouse)

	if err := c.KeyEvent("KEYCODE_HOME"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []inputEvent{
		{Type: evKey, Code: 172, Value: 1},
		{Type: evSyn, Code: synReport, Value: 0},
		{Type: evKey, Code: 172, Value: 0},
		{Type: evSyn, Code: synReport, Value: 0},
	}
	if !eventsEqual(kb.events, want) {
		t.Errorf("got %+v, want %+v", kb.events, want)
	}
	if len(mouse.events) != 0 {
		t.Errorf("expected no mouse events, got %v", mouse.events)
	}
}

func TestKeyEvent_RejectsUnknownCode(t *testing.T) {
	kb, mouse := &fakeEventWriter{}, &fakeEventWriter{}
	c := newController(kb, mouse)

	cases := []string{
		"volume_up",
		"KEYCODE_NOT_A_REAL_KEY",
		"KEYCODE_VOLUME_UP; rm -rf /",
		"",
	}
	for _, in := range cases {
		if err := c.KeyEvent(in); err == nil {
			t.Errorf("KeyEvent(%q) expected error, got nil", in)
		}
	}
	if len(kb.events) != 0 {
		t.Errorf("expected no writes for invalid input, got %v", kb.events)
	}
}

func TestPointerMove_WritesXYAndSyn(t *testing.T) {
	kb, mouse := &fakeEventWriter{}, &fakeEventWriter{}
	c := newController(kb, mouse)

	if err := c.PointerMove(5, -3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []inputEvent{
		{Type: evRel, Code: relX, Value: 5},
		{Type: evRel, Code: relY, Value: -3},
		{Type: evSyn, Code: synReport, Value: 0},
	}
	if !eventsEqual(mouse.events, want) {
		t.Errorf("got %+v, want %+v", mouse.events, want)
	}
	if len(kb.events) != 0 {
		t.Errorf("expected no keyboard events, got %v", kb.events)
	}
}

func TestPointerButton_DownAndUp(t *testing.T) {
	kb, mouse := &fakeEventWriter{}, &fakeEventWriter{}
	c := newController(kb, mouse)

	if err := c.PointerButton(true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDown := []inputEvent{
		{Type: evKey, Code: btnLeft, Value: 1},
		{Type: evSyn, Code: synReport, Value: 0},
	}
	if !eventsEqual(mouse.events, wantDown) {
		t.Errorf("down: got %+v, want %+v", mouse.events, wantDown)
	}

	mouse.events = nil
	if err := c.PointerButton(false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantUp := []inputEvent{
		{Type: evKey, Code: btnLeft, Value: 0},
		{Type: evSyn, Code: synReport, Value: 0},
	}
	if !eventsEqual(mouse.events, wantUp) {
		t.Errorf("up: got %+v, want %+v", mouse.events, wantUp)
	}
}

func TestLaunchApp_RejectsInvalidPackageName(t *testing.T) {
	kb, mouse := &fakeEventWriter{}, &fakeEventWriter{}
	c := newController(kb, mouse)

	if err := c.LaunchApp("com.evil; rm -rf /"); err == nil {
		t.Error("expected error for malicious package name")
	}
	if err := c.LaunchApp(""); err == nil {
		t.Error("expected error for empty package name")
	}
}

func TestApps_ReturnsCopyNotSharedSlice(t *testing.T) {
	c := newController(&fakeEventWriter{}, &fakeEventWriter{})
	c.apps = []string{"com.netflix.ninja"}

	got := c.Apps()
	got[0] = "mutated"

	if c.apps[0] != "com.netflix.ninja" {
		t.Errorf("Apps() caller mutation leaked into Controller state: %v", c.apps)
	}
}

func TestText_AppendOnlySendsSuffix(t *testing.T) {
	kb, mouse := &fakeEventWriter{}, &fakeEventWriter{}
	c := newController(kb, mouse)
	text := &fakeTextRunner{}
	c.textRun = text

	if err := c.Text("net"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := c.Text("netflix"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantTyped := []string{"net", "flix"}
	if len(text.typed) != len(wantTyped) {
		t.Fatalf("typed = %v, want %v", text.typed, wantTyped)
	}
	for i := range wantTyped {
		if text.typed[i] != wantTyped[i] {
			t.Errorf("typed[%d] = %q, want %q", i, text.typed[i], wantTyped[i])
		}
	}
	if len(kb.events) != 0 {
		t.Errorf("expected no key events for pure appends, got %v", kb.events)
	}
	if c.lastSyncedText != "netflix" {
		t.Errorf("lastSyncedText = %q, want netflix", c.lastSyncedText)
	}
}

func TestText_EditSendsBackspacesThenRetypes(t *testing.T) {
	kb, mouse := &fakeEventWriter{}, &fakeEventWriter{}
	c := newController(kb, mouse)
	text := &fakeTextRunner{}
	c.textRun = text

	if err := c.Text("netflix"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	kb.events = nil
	text.typed = nil

	if err := c.Text("neta"); err != nil { // not a prefix extension of "netflix"
		t.Fatalf("unexpected error: %v", err)
	}

	wantDelPresses := len("netflix") // one down+syn+up+syn per backspace
	if len(kb.events) != wantDelPresses*4 {
		t.Fatalf("got %d key events, want %d (4 per backspace)", len(kb.events), wantDelPresses*4)
	}
	for i := 0; i < wantDelPresses; i++ {
		down := kb.events[i*4]
		if down.Type != evKey || down.Code != keyCodes["KEYCODE_DEL"] || down.Value != 1 {
			t.Fatalf("backspace %d down event = %+v", i, down)
		}
	}
	if len(text.typed) != 1 || text.typed[0] != "neta" {
		t.Errorf("typed = %v, want [neta]", text.typed)
	}
	if c.lastSyncedText != "neta" {
		t.Errorf("lastSyncedText = %q, want neta", c.lastSyncedText)
	}
}

func TestText_NoOpWhenUnchanged(t *testing.T) {
	kb, mouse := &fakeEventWriter{}, &fakeEventWriter{}
	c := newController(kb, mouse)
	text := &fakeTextRunner{}
	c.textRun = text

	if err := c.Text("same"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	kb.events = nil
	text.typed = nil

	if err := c.Text("same"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(kb.events) != 0 || len(text.typed) != 0 {
		t.Errorf("expected no events for an unchanged value, got keys=%v typed=%v", kb.events, text.typed)
	}
}

func TestText_ClearingSendsBackspacesOnly(t *testing.T) {
	kb, mouse := &fakeEventWriter{}, &fakeEventWriter{}
	c := newController(kb, mouse)
	text := &fakeTextRunner{}
	c.textRun = text

	if err := c.Text("hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	kb.events = nil
	text.typed = nil

	if err := c.Text(""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(kb.events) != 2*4 {
		t.Errorf("got %d key events, want 8 (2 backspaces)", len(kb.events))
	}
	if len(text.typed) != 0 {
		t.Errorf("expected no RunText call when clearing to empty, got %v", text.typed)
	}
}

func TestClampVolume(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{-5, 0},
		{0, 0},
		{50, 50},
		{100, 100},
		{500, 100},
	}
	for _, c := range cases {
		if got := clampVolume(c.in); got != c.want {
			t.Errorf("clampVolume(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestConnected_AlwaysTrue(t *testing.T) {
	c := newController(&fakeEventWriter{}, &fakeEventWriter{})
	if !c.Connected() {
		t.Error("expected Connected() to always report true")
	}
}

func eventsEqual(a, b []inputEvent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].Code != b[i].Code || a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

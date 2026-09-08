package adbctl

import (
	"bytes"
	"testing"
)

type fakeWriter struct {
	lines []string
}

func (f *fakeWriter) WriteLine(s string) error {
	f.lines = append(f.lines, s)
	return nil
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLineWriter_AppendsNewline(t *testing.T) {
	var buf bytes.Buffer
	lw := &lineWriter{w: &buf}
	if err := lw.WriteLine("input keyevent KEYCODE_HOME"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != "input keyevent KEYCODE_HOME\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestKeyEvent_ValidCode(t *testing.T) {
	fw := &fakeWriter{}
	c := New(fw)
	if err := c.KeyEvent("KEYCODE_VOLUME_UP"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"input keyevent KEYCODE_VOLUME_UP"}
	if !equalLines(fw.lines, want) {
		t.Errorf("got %v, want %v", fw.lines, want)
	}
}

func TestKeyEvent_RejectsInvalidCode(t *testing.T) {
	fw := &fakeWriter{}
	c := New(fw)
	cases := []string{
		"volume_up",
		"KEYCODE_VOLUME_UP; rm -rf /",
		"KEYCODE_VOLUME_UP\ninput text pwned",
		"",
	}
	for _, in := range cases {
		if err := c.KeyEvent(in); err == nil {
			t.Errorf("KeyEvent(%q) expected error, got nil", in)
		}
	}
	if len(fw.lines) != 0 {
		t.Errorf("expected no writes for invalid input, got %v", fw.lines)
	}
}

func TestText_QuotesAndEscapesSingleQuotes(t *testing.T) {
	fw := &fakeWriter{}
	c := New(fw)
	if err := c.Text(`it's back`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `input text 'it'\''s back'`
	if len(fw.lines) != 1 || fw.lines[0] != want {
		t.Errorf("got %v, want [%s]", fw.lines, want)
	}
}

func TestLaunchApp_ValidatesPackageName(t *testing.T) {
	fw := &fakeWriter{}
	c := New(fw)
	if err := c.LaunchApp("com.netflix.ninja"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "monkey -p com.netflix.ninja -c android.intent.category.LAUNCHER 1"
	if len(fw.lines) != 1 || fw.lines[0] != want {
		t.Errorf("got %v, want [%s]", fw.lines, want)
	}

	fw.lines = nil
	if err := c.LaunchApp("com.evil; rm -rf /"); err == nil {
		t.Errorf("expected error for malicious package name")
	}
	if len(fw.lines) != 0 {
		t.Errorf("expected no writes for invalid package, got %v", fw.lines)
	}
}

func TestPointerMove_WritesXYAndSyn(t *testing.T) {
	fw := &fakeWriter{}
	c := New(fw)
	c.SetMouseNode("/dev/input/event10")
	if err := c.PointerMove(5, -3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"sendevent /dev/input/event10 2 0 5",
		"sendevent /dev/input/event10 2 1 -3",
		"sendevent /dev/input/event10 0 0 0",
	}
	if !equalLines(fw.lines, want) {
		t.Errorf("got %v, want %v", fw.lines, want)
	}
}

func TestPointerMove_ErrorsWithoutMouseNode(t *testing.T) {
	fw := &fakeWriter{}
	c := New(fw)
	if err := c.PointerMove(1, 1); err == nil {
		t.Errorf("expected error when mouse node not resolved")
	}
}

func TestPointerButton_DownAndUp(t *testing.T) {
	fw := &fakeWriter{}
	c := New(fw)
	c.SetMouseNode("/dev/input/event10")

	if err := c.PointerButton(true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDown := []string{
		"sendevent /dev/input/event10 1 0x110 1",
		"sendevent /dev/input/event10 0 0 0",
	}
	if !equalLines(fw.lines, wantDown) {
		t.Errorf("down: got %v, want %v", fw.lines, wantDown)
	}

	fw.lines = nil
	if err := c.PointerButton(false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantUp := []string{
		"sendevent /dev/input/event10 1 0x110 0",
		"sendevent /dev/input/event10 0 0 0",
	}
	if !equalLines(fw.lines, wantUp) {
		t.Errorf("up: got %v, want %v", fw.lines, wantUp)
	}
}

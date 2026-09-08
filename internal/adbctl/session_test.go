package adbctl

import (
	"testing"
	"time"
)

const sampleGeteventOutput = `add device 1: /dev/input/event10
  bus:      0000
  vendor    0000
  product   0000
  version   0000
  name:     "jixin_mouse"
  location: ""
  id:       ""
  version:  1.0.1
  events:
    KEY (0001): 0110
    REL (0002): 0000  0001  0006  0008
  input props:
    <none>
add device 2: /dev/input/event2
  bus:      0010
  vendor    0001
  product   0001
  version   0001
  name:     "input_hdmirx"
  location: ""
  id:       ""
  version:  1.0.1
  events:
    KEY (0001): 0074
  input props:
    <none>
add device 3: /dev/input/event1
  bus:      0010
  vendor    1b8e
  product   0cec
  version   0001
  name:     "cec_input"
  location: ""
  id:       ""
  version:  1.0.1
  events:
    KEY (0001): 0001  000f  001c
  input props:
    <none>
`

func TestParseMouseNodeFromGetevent_FindsNamedDevice(t *testing.T) {
	got, err := parseMouseNodeFromGetevent(sampleGeteventOutput, "jixin_mouse")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/dev/input/event10" {
		t.Errorf("got %q, want /dev/input/event10", got)
	}
}

func TestParseMouseNodeFromGetevent_NotFound(t *testing.T) {
	_, err := parseMouseNodeFromGetevent(sampleGeteventOutput, "nonexistent_device")
	if err == nil {
		t.Fatal("expected error for missing device name")
	}
}

func TestParsePackageList_ExtractsPackageNames(t *testing.T) {
	input := "package:com.netflix.ninja\npackage:com.google.android.youtube.tv\n\n"
	got := parsePackageList(input)
	want := []string{"com.netflix.ninja", "com.google.android.youtube.tv"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNextBackoff_CapsAtLastStep(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, time.Second},
		{1, 2 * time.Second},
		{2, 5 * time.Second},
		{3, 10 * time.Second},
		{10, 10 * time.Second},
	}
	for _, c := range cases {
		got := nextBackoff(c.attempt)
		if got != c.want {
			t.Errorf("nextBackoff(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestSession_DelegatesFailWhenNotConnected(t *testing.T) {
	s := NewSession("127.0.0.1:1", nil)
	if err := s.KeyEvent("KEYCODE_HOME"); err == nil {
		t.Error("expected error before any connection has been established")
	}
	if err := s.PointerMove(1, 1); err == nil {
		t.Error("expected error before any connection has been established")
	}
	if s.Connected() {
		t.Error("expected Connected() to be false before Run has succeeded")
	}
	if apps := s.Apps(); len(apps) != 0 {
		t.Errorf("expected no apps before first connect, got %v", apps)
	}
}

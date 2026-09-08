package deviceio

import "testing"

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

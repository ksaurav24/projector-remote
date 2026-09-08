package deviceio

import (
	"os"
	"testing"
)

// TestFileEventWriter_WireFormat locks in the on-disk byte layout of
// one injected event: 16 bytes total (4-byte Sec + 4-byte Usec +
// 2-byte Type + 2-byte Code + 4-byte Value, little-endian), matching
// struct input_event on this device's 32-bit ARM kernel ABI. A 64-bit
// timeval (the layout a 64-bit kernel would use) would silently
// misalign every field after it, so this is worth pinning down.
func TestFileEventWriter_WireFormat(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "evdev-test")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()

	w := &fileEventWriter{f: f}
	if err := w.WriteEvent(evKey, 172, 1); err != nil {
		t.Fatalf("WriteEvent: %v", err)
	}

	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := []byte{
		0, 0, 0, 0, // Sec
		0, 0, 0, 0, // Usec
		0x01, 0x00, // Type = evKey (1)
		172, 0x00, // Code = 172
		1, 0, 0, 0, // Value = 1
	}
	if len(got) != 16 {
		t.Fatalf("wrote %d bytes, want 16: % x", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte %d = 0x%02x, want 0x%02x (full: % x)", i, got[i], want[i], got)
			break
		}
	}
}

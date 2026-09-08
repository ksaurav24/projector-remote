// Package deviceio executes remote-control commands directly on the
// device it runs on: local uinput/evdev event injection for keys and
// the pointer, local subprocess execution for text entry and app
// launch. It replaces the old adbctl package, which drove these same
// actions over a remote `adb shell` pipe — that indirection doesn't
// exist once the server runs on the projector itself.
package deviceio

import (
	"bytes"
	"encoding/binary"
	"os"
)

// Linux evdev event types/codes this package uses. Values match
// linux/input-event-codes.h.
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02

	synReport = 0

	relX = 0x00
	relY = 0x01

	btnLeft = 0x110
)

// inputEvent mirrors the kernel's struct input_event on 32-bit ARM
// (this device's ABI, confirmed via docs/device-guide.md): a 32-bit
// timeval (Sec, Usec), not the 64-bit timeval a 64-bit kernel uses.
// The timestamp is left zeroed on write — the kernel fills it in.
type inputEvent struct {
	Sec, Usec  int32
	Type, Code uint16
	Value      int32
}

// eventWriter is the minimal surface for injecting one raw evdev
// event. Backed by an open uinput virtual device or a real evdev node
// in production (fileEventWriter); tests use an in-memory fake.
type eventWriter interface {
	WriteEvent(typ, code uint16, value int32) error
}

// fileEventWriter injects events by writing directly to an already-
// open device node. A uinput virtual device and a real evdev node
// (e.g. the mouse) accept the identical wire format, so both use this
// same writer.
type fileEventWriter struct {
	f *os.File
}

func (w *fileEventWriter) WriteEvent(typ, code uint16, value int32) error {
	var buf bytes.Buffer
	ev := inputEvent{Type: typ, Code: code, Value: value}
	if err := binary.Write(&buf, binary.LittleEndian, &ev); err != nil {
		return err
	}
	_, err := w.f.Write(buf.Bytes())
	return err
}

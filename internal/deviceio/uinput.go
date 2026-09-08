package deviceio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
)

// ioctl request codes for /dev/uinput. Values match linux/uinput.h
// (_IOW('U', 100, int), _IOW('U', 101, int), _IO('U', 1), _IO('U', 2)).
const (
	uiSetEvBit   = 0x40045564
	uiSetKeyBit  = 0x40045565
	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
)

// uinputUserDev mirrors struct uinput_user_dev from linux/uinput.h
// (the old, non-UI_DEV_SETUP registration API: write this struct to
// the fd, then ioctl UI_DEV_CREATE). ABS_CNT is 64 on this kernel.
type uinputUserDev struct {
	Name         [80]byte
	Bustype      uint16
	Vendor       uint16
	Product      uint16
	Version      uint16
	FFEffectsMax uint32
	AbsMax       [64]int32
	AbsMin       [64]int32
	AbsFuzz      [64]int32
	AbsFlat      [64]int32
}

// busVirtual is Linux's BUS_VIRTUAL, used for the synthetic keyboard
// device's input_id.bustype so it doesn't claim to be any real bus.
const busVirtual = 0x06

// openUinputKeyboard creates a virtual keyboard device via /dev/uinput
// registered with exactly the given key codes, and returns a writer
// for injecting press/release events into it.
//
// Verified end-to-end against the real device (t950s_be311): an
// injected key reaches Android's actual input pipeline, not just
// getevent — confirmed via dumpsys window focus (HOME, BACK) and
// dumpsys audio stream volume (VOLUME_UP/DOWN) changing in response
// to injected events. This works because a BUS_VIRTUAL device with no
// vendor-specific keylayout file falls back to the system's
// Generic.kl, same as any other uinput-created input device.
func openUinputKeyboard(path string, codes map[string]uint16) (*fileEventWriter, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("deviceio: open %s: %w", path, err)
	}
	fd := f.Fd()

	if err := ioctl(fd, uiSetEvBit, uintptr(evKey)); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("deviceio: UI_SET_EVBIT: %w", err)
	}
	for name, code := range codes {
		if err := ioctl(fd, uiSetKeyBit, uintptr(code)); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("deviceio: UI_SET_KEYBIT %s (%d): %w", name, code, err)
		}
	}

	var dev uinputUserDev
	copy(dev.Name[:], "projector-remote-kbd")
	dev.Bustype = busVirtual
	dev.Vendor, dev.Product, dev.Version = 1, 1, 1

	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, &dev); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("deviceio: encode uinput_user_dev: %w", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("deviceio: write uinput_user_dev: %w", err)
	}

	if err := ioctl(fd, uiDevCreate, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("deviceio: UI_DEV_CREATE: %w", err)
	}

	return &fileEventWriter{f: f}, nil
}

func ioctl(fd, cmd, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, cmd, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

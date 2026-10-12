package mediastream

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"projectorremote/internal/medialib"
)

// Thumbs grabs a still from a video for titles and episodes that have no
// artwork, once per file.
type Thumbs struct {
	FFmpeg string
	Dir    string
	Lib    Library

	once sync.Once
	sem  chan struct{}
	mu   sync.Mutex
	busy map[string]*sync.Mutex
}

// Path returns the thumbnail for f, making it if needed.
func (t *Thumbs) Path(ctx context.Context, f *medialib.File) (string, error) {
	t.once.Do(func() {
		t.sem = make(chan struct{}, 2)
		t.busy = map[string]*sync.Mutex{}
		_ = os.MkdirAll(t.Dir, 0o755)
	})
	out := filepath.Join(t.Dir, f.ID()+".jpg")
	if exists(out) {
		return out, nil
	}
	t.mu.Lock()
	l := t.busy[f.ID()]
	if l == nil {
		l = &sync.Mutex{}
		t.busy[f.ID()] = l
	}
	t.mu.Unlock()
	l.Lock()
	defer l.Unlock()
	if exists(out) {
		return out, nil
	}
	select {
	case t.sem <- struct{}{}:
		defer func() { <-t.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	// A quarter of the way in skips cold opens and title cards, capped so
	// a long film doesn't need a deep seek.
	at := min(f.Probe.Duration*0.25, 600)
	vf := "scale=w=-2:h=360:flags=bicubic"
	if v := f.Probe.Video; v.HDR() {
		vf += "," + tonemapChain(v)
	}
	tmp := out + ".tmp.jpg"
	cmd := exec.CommandContext(ctx, t.FFmpeg, "-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-ss", strconv.FormatFloat(at, 'f', 2, 64), "-i", t.Lib.Abs(f.Rel), "-map", "0:"+strconv.Itoa(f.Probe.Video.Index),
		"-frames:v", "1", "-vf", vf, "-q:v", "3", tmp)
	if b, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("thumbnail %s: %v: %s", f.Rel, err, strings.TrimSpace(string(b)))
	}
	return out, os.Rename(tmp, out)
}

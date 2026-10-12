package mediastream

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"projectorremote/internal/medialib"
)

// SubTrack is a subtitle file ready for the player, which loads it beside
// the HLS stream.
type SubTrack struct {
	// Name is the file name under the file's subtitle directory.
	Name     string
	Lang     string
	Label    string
	MimeType string
	Default  bool
	Forced   bool
}

// Subtitles extracts and converts subtitles once per file and keeps them
// on disk. Text formats become WebVTT, except ASS/SSA, which keeps its
// styling and positioning. Bitmap subtitles (PGS, VobSub) can't be sent
// beside an HLS stream and are skipped.
type Subtitles struct {
	FFmpeg string
	Dir    string
	Lib    Library

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (s *Subtitles) lock(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks == nil {
		s.locks = map[string]*sync.Mutex{}
	}
	l := s.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	return l
}

// Path is where a prepared subtitle lives, or "" for a bad name.
func (s *Subtitles) Path(fileID, name string) string {
	if strings.ContainsAny(fileID+name, "/\\") || strings.Contains(name, "..") {
		return ""
	}
	return filepath.Join(s.Dir, fileID, name)
}

func isASS(codec string) bool { return codec == "ass" || codec == "ssa" }

// Tracks prepares f's subtitles and lists them.
func (s *Subtitles) Tracks(ctx context.Context, f *medialib.File) []SubTrack {
	id := f.ID()
	l := s.lock(id)
	l.Lock()
	defer l.Unlock()
	dir := filepath.Join(s.Dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("mediastream: subtitles: %v", err)
		return nil
	}

	var tracks []SubTrack
	var args []string
	var pending []SubTrack
	for _, st := range f.Probe.Subs {
		if !medialib.TextSub(st.Codec) {
			continue
		}
		t := SubTrack{Lang: st.Lang, Label: subLabel(st.Title, st.Lang, st.Forced, st.SDH), Default: st.Default, Forced: st.Forced}
		ext, codec := "vtt", "webvtt"
		if isASS(st.Codec) {
			ext, codec = "ass", "copy"
		}
		t.Name = fmt.Sprintf("e%d.%s", st.Index, ext)
		t.MimeType = mimeFor(ext)
		if !exists(filepath.Join(dir, t.Name)) {
			args = append(args, "-map", "0:"+strconv.Itoa(st.Index), "-c:s", codec, filepath.Join(dir, t.Name+".tmp."+ext))
			pending = append(pending, t)
		}
		tracks = append(tracks, t)
	}
	if len(args) > 0 {
		// One pass over the file for every embedded track.
		cmd := exec.CommandContext(ctx, s.FFmpeg, append([]string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
			"-i", s.Lib.Abs(f.Rel)}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("mediastream: extract subtitles of %s: %v: %s", f.Rel, err, strings.TrimSpace(string(out)))
		}
		for _, t := range pending {
			ext := filepath.Ext(t.Name)
			_ = os.Rename(filepath.Join(dir, t.Name+".tmp"+ext), filepath.Join(dir, t.Name))
		}
	}

	for i, x := range f.ExtSubs {
		t := SubTrack{Lang: x.Lang, Label: subLabel(x.Title, x.Lang, x.Forced, x.SDH), Forced: x.Forced}
		ext := "vtt"
		if isASS(x.Codec) {
			ext = "ass"
		}
		t.Name = fmt.Sprintf("x%d.%s", i, ext)
		t.MimeType = mimeFor(ext)
		out := filepath.Join(dir, t.Name)
		if !exists(out) {
			if err := s.convert(ctx, s.Lib.Abs(x.Path), x, out); err != nil {
				log.Printf("mediastream: subtitle %s: %v", x.Path, err)
				continue
			}
		}
		tracks = append(tracks, t)
	}

	// Remove tracks whose extraction failed.
	ok := tracks[:0]
	for _, t := range tracks {
		if exists(filepath.Join(dir, t.Name)) {
			ok = append(ok, t)
		}
	}
	return ok
}

// convert prepares an external subtitle file: ASS and WebVTT are used as
// is (re-encoded to UTF-8 if needed), anything else becomes WebVTT.
func (s *Subtitles) convert(ctx context.Context, src string, x medialib.ExtSub, out string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := out + ".tmp" + filepath.Ext(out)
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y"}
	if !utf8.Valid(raw) {
		args = append(args, "-sub_charenc", legacyCharset(x.Lang))
	}
	if (x.Codec == "webvtt" || isASS(x.Codec)) && utf8.Valid(raw) {
		if err := os.WriteFile(tmp, raw, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, out)
	}
	codec := "webvtt"
	if isASS(x.Codec) {
		codec = "ass"
	}
	args = append(args, "-i", src, "-map", "0:s:0", "-c:s", codec, tmp)
	if b, err := exec.CommandContext(ctx, s.FFmpeg, args...).CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(b)))
	}
	return os.Rename(tmp, out)
}

// legacyCharset guesses the 8-bit code page an old subtitle file was
// saved in from its language.
func legacyCharset(lang string) string {
	switch lang {
	case "ru", "uk", "bg", "sr":
		return "CP1251"
	case "ar", "fa", "ur":
		return "CP1256"
	case "el":
		return "CP1253"
	case "he":
		return "CP1255"
	case "tr":
		return "CP1254"
	case "pl", "cs", "hu", "ro", "hr", "sk", "sl":
		return "CP1250"
	case "zh":
		return "GB18030"
	case "ja":
		return "SHIFT_JIS"
	case "ko":
		return "CP949"
	case "th":
		return "CP874"
	case "vi":
		return "CP1258"
	}
	return "CP1252"
}

func mimeFor(ext string) string {
	if ext == "ass" {
		return "text/x-ssa"
	}
	return "text/vtt"
}

func subLabel(title, lang string, forced, sdh bool) string {
	name := LangName(lang)
	t := strings.TrimSpace(title)
	label := name
	switch {
	case t != "" && name != "" && !strings.EqualFold(t, name):
		label = name + " · " + t
	case t != "":
		label = t
	case label == "":
		label = "Unknown"
	}
	lt := strings.ToLower(label)
	if forced && !strings.Contains(lt, "forced") {
		label += " (Forced)"
	}
	if sdh && !strings.Contains(lt, "sdh") {
		label += " (SDH)"
	}
	return label
}

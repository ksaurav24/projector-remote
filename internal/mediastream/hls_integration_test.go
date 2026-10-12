package mediastream

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"projectorremote/internal/medialib"
)

type fakeLib struct {
	root  string
	files map[string]*medialib.File
}

func (l *fakeLib) Abs(rel string) string                                { return filepath.Join(l.root, rel) }
func (l *fakeLib) File(rel string) *medialib.File                       { return l.files[rel] }
func (l *fakeLib) SetKeyframes(rel string, kf []float64, delay float64) {}

func needFFmpeg(t *testing.T) {
	t.Helper()
	for _, b := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skip("ffmpeg not installed")
		}
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// firstPTS is the first packet's presentation time in a TS segment.
func firstPTS(t *testing.T, path string) float64 {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "packet=pts_time", "-read_intervals", "%+#1",
		"-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	v, err := strconv.ParseFloat(strings.Trim(strings.TrimSpace(string(out)), ","), 64)
	if err != nil {
		t.Fatalf("pts of %s: %q", path, out)
	}
	return v
}

// TestSeekAlignment checks the property seeking depends on: a segment
// made by a run that started mid-file is the same segment, at the same
// time, as one made by a run from the start, for both stream copies and
// transcodes, including a file whose video starts late.
func TestSeekAlignment(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	_ = os.MkdirAll(src, 0o755)
	// Video starting 2s after the audio, keyframes every second, B-frames.
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-itsoffset", "2", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=25",
		"-f", "lavfi", "-i", "sine=f=440:sample_rate=48000", "-t", "30", "-c:v", "libx264", "-preset", "ultrafast", "-bf", "2",
		"-g", "25", "-keyint_min", "25", "-sc_threshold", "0", "-c:a", "ac3", filepath.Join(src, "late.mkv"))
	// MP4 with B-frames (an edit list shifts its start) and MPEG-TS with a
	// non-zero start time, irregular keyframes and AAC.
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30000/1001",
		"-f", "lavfi", "-i", "sine=f=440:sample_rate=44100", "-t", "30", "-c:v", "libx264", "-preset", "ultrafast", "-bf", "3",
		"-g", "60", "-c:a", "aac", filepath.Join(src, "bframes.mp4"))
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24",
		"-f", "lavfi", "-i", "sine=f=440:sample_rate=48000", "-t", "30", "-c:v", "libx264", "-preset", "ultrafast", "-bf", "2",
		"-g", "37", "-c:a", "aac", "-output_ts_offset", "100", filepath.Join(src, "offset.ts"))

	ctx := context.Background()
	type mode struct {
		name string
		caps Caps
		copy bool
	}
	modes := []mode{
		{"copy", Caps{Video: []VideoCap{{Codec: "h264", MaxWidth: 1920, MaxHeight: 1080}}, Audio: []string{"aac", "ac3"}}, true},
		{"transcode", Caps{Video: []VideoCap{{Codec: "hevc", MaxWidth: 1920, MaxHeight: 1080}}, Audio: []string{"aac"}}, false},
	}
	for _, file := range []string{"late.mkv", "bframes.mp4", "offset.ts"} {
		for _, tc := range modes {
			t.Run(file+"/"+tc.name, func(t *testing.T) {
				seekAlignment(t, ctx, dir, src, file, tc.name, tc.caps, tc.copy, Encoder{Name: "libx264"})
			})
		}
	}
	// The GPU path (NVDEC decode, CUDA scaling, NVENC) has its own filter
	// graph; check it too where there's an NVIDIA GPU.
	if enc := DetectEncoder("ffmpeg", "h264_nvenc"); enc.CUDA {
		t.Run("bframes.mp4/nvenc", func(t *testing.T) {
			seekAlignment(t, ctx, dir, src, "bframes.mp4", "nvenc", modes[1].caps, false, enc)
		})
	}
}

func seekAlignment(t *testing.T, ctx context.Context, dir, src, file, mode string, caps Caps, wantCopy bool, enc Encoder) {
	p, err := medialib.ProbeFile(ctx, "ffprobe", filepath.Join(src, file))
	if err != nil {
		t.Fatal(err)
	}
	f := &medialib.File{Rel: file, Size: 1, ModTime: 1, Probe: p}
	lib := &fakeLib{root: src, files: map[string]*medialib.File{file: f}}
	newManager := func(sub string) *Manager {
		m := &Manager{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Enc: enc, Lib: lib,
			Opt: Options{MaxHeight: 720, SegmentSeconds: 4}, Dir: filepath.Join(dir, file+"-"+mode, sub)}
		if err := m.Init(ctx); err != nil {
			t.Fatal(err)
		}
		return m
	}
	full, seek := newManager("full"), newManager("seek")
	key, plan, err := full.Prepare(ctx, f, caps)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Variants[0].Copy != wantCopy {
		t.Fatalf("top variant copy = %v, want %v", plan.Variants[0].Copy, wantCopy)
	}
	if _, _, err := seek.Prepare(ctx, f, caps); err != nil {
		t.Fatal(err)
	}
	n := plan.Segments()
	at := n / 2
	rends := []string{"a0"}
	for i := range plan.Variants {
		rends = append(rends, "v"+strconv.Itoa(i))
	}
	for _, r := range rends {
		// The full run makes every segment from the start, in order.
		var fromStart []string
		for k := 0; k < n; k++ {
			fp, err := full.Segment(ctx, key, r, k)
			if err != nil {
				t.Fatalf("full %s/%d: %v", r, k, err)
			}
			fromStart = append(fromStart, fp)
		}
		// The seek run starts at segment "at".
		for k := at; k < min(at+3, n); k++ {
			sp, err := seek.Segment(ctx, key, r, k)
			if err != nil {
				t.Fatalf("seek %s/%d: %v", r, k, err)
			}
			a, b := firstPTS(t, sp), firstPTS(t, fromStart[k])
			// Video to the frame; audio to within one AAC frame for a
			// transcode (each encode starts its own frame grid) and exactly
			// for a copy.
			tol := 0.045
			if r[0] == 'a' {
				tol = 0.003
				if !wantCopy {
					tol = 0.024
				}
			}
			if math.Abs(a-b) > tol {
				t.Errorf("%s segment %d starts at %.3f after a seek but %.3f from the start", r, k, a, b)
			}
		}
		// Every segment starts where the playlist says, plus MPEG-TS's
		// 1.4s mux delay and the source's own start offset.
		if r[0] == 'a' {
			continue
		}
		for k := 1; k < n; k++ {
			a, want := firstPTS(t, fromStart[k]), plan.Bounds[k]+1.4
			if math.Abs(a-want) > 0.045 {
				t.Errorf("%s segment %d starts at %.3f, playlist says %.3f", r, k, a, want)
			}
		}
	}
}

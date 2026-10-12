package localmedia

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"projectorremote/internal/medialib"
	"projectorremote/internal/mediaserver"
	"projectorremote/internal/mediastream"
)

// TestRoundTrip runs a real media server over a two-file library and
// browses and resolves it the way the projector does.
func TestRoundTrip(t *testing.T) {
	for _, b := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skip("ffmpeg not installed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root, cache := t.TempDir(), t.TempDir()
	mk := func(rel string, args ...string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		a := append([]string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24",
			"-f", "lavfi", "-i", "sine", "-t", "10"}, args...)
		if out, err := exec.Command("ffmpeg", append(a, p)...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg: %v\n%s", err, out)
		}
	}
	mk("Shows/Some Show/Season 1/Some.Show.S01E01.mkv", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-metadata:s:a:0", "language=eng")
	mk("Shows/Some Show/Season 1/Some.Show.S01E02.mkv", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac")
	mk("Movies/A Film (2001)/a.film.2001.avi", "-c:v", "mpeg4", "-c:a", "libmp3lame")
	_ = os.WriteFile(filepath.Join(root, "Movies/A Film (2001)/a.film.2001.en.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "Movies/A Film (2001)/poster.jpg"), []byte("\xff\xd8\xff\xe0fakejpeg"), 0o644)

	lib, err := medialib.Open(medialib.Config{Root: root, CacheDir: cache, FFprobe: "ffprobe"})
	if err != nil {
		t.Fatal(err)
	}
	lib.Scan(ctx)
	streams := &mediastream.Manager{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Enc: mediastream.Encoder{Name: "libx264"}, Lib: lib,
		Opt: mediastream.Options{MaxHeight: 720, SegmentSeconds: 4}, Dir: filepath.Join(cache, "hls")}
	if err := streams.Init(ctx); err != nil {
		t.Fatal(err)
	}
	srv := &mediaserver.Server{Lib: lib, Meta: medialib.NewMetadata("", "en-US", cache, nil), Streams: streams,
		Subs:   &mediastream.Subtitles{FFmpeg: "ffmpeg", Dir: filepath.Join(cache, "subs"), Lib: lib},
		Thumbs: &mediastream.Thumbs{FFmpeg: "ffmpeg", Dir: filepath.Join(cache, "thumbs"), Lib: lib}, Name: "test"}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	src := New(Config{Server: ts.URL, Caps: mediastream.DefaultCaps()})
	go src.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for !src.Available() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !src.Available() {
		t.Fatal("media server never became available")
	}

	home, err := src.Home(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	var showID, movieID, moviePoster string
	for _, r := range home.Rows {
		for _, it := range r.Items {
			switch it.Title {
			case "Some Show":
				showID = it.ID
			case "A Film":
				movieID, moviePoster = it.ID, it.Poster
			}
		}
	}
	if showID == "" || movieID == "" {
		t.Fatalf("home rows = %+v", home.Rows)
	}
	if !strings.HasPrefix(moviePoster, ArtPrefix+"file/") {
		t.Errorf("movie poster %q isn't relayed through the projector", moviePoster)
	}

	// Artwork relay.
	rec := httptest.NewRecorder()
	src.ArtHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, moviePoster, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "fakejpeg") {
		t.Errorf("art relay: %d %q", rec.Code, rec.Body.String())
	}

	show, err := src.Title(ctx, "local", showID)
	if err != nil {
		t.Fatal(err)
	}
	if show.Type != "series" || len(show.Seasons) != 1 || show.Episodes == nil || len(show.Episodes.Items) != 2 {
		t.Fatalf("show = %+v", show)
	}

	// The AVI's MPEG-4 video must be transcoded; its subtitle sidecar
	// becomes WebVTT; the playlist and a segment come straight from the
	// laptop.
	st, err := src.Resolve(ctx, "local", movieID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Subtitles) != 1 || st.Subtitles[0].Language != "en" || st.Subtitles[0].MimeType != "text/vtt" {
		t.Errorf("subtitles = %+v", st.Subtitles)
	}
	body := func(u string) string {
		resp, err := http.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: %d %s", u, resp.StatusCode, b)
		}
		return string(b)
	}
	if !strings.Contains(body(st.Subtitles[0].URL), "WEBVTT") {
		t.Error("subtitle isn't WebVTT")
	}
	master := body(st.URL)
	if !strings.Contains(master, "v0.m3u8") || !strings.Contains(master, `TYPE=AUDIO`) {
		t.Fatalf("master playlist:\n%s", master)
	}
	base := strings.TrimSuffix(st.URL, "master.m3u8")
	if seg := body(base + "v0/00001.ts"); len(seg) < 1000 || seg[0] != 0x47 {
		t.Errorf("segment isn't MPEG-TS (%d bytes)", len(seg))
	}
}

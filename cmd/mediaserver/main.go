// Command mediaserver runs on the laptop that holds the video files. It
// indexes a media folder into movies and series, keeps the index current
// as files come and go, and streams them to the projector as HLS,
// transcoding only what the projector can't play as is. The projector
// finds it over Tailscale.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"projectorremote/internal/medialib"
	"projectorremote/internal/mediaserver"
	"projectorremote/internal/mediastream"
)

func main() {
	home, _ := os.UserHomeDir()
	cacheHome, err := os.UserCacheDir()
	if err != nil {
		cacheHome = filepath.Join(home, ".cache")
	}
	hostname, _ := os.Hostname()

	mediaDir := flag.String("media", filepath.Join(home, "Media"), "folder holding the movies and shows")
	listen := flag.String("listen", ":8770", "address to listen on (only loopback and Tailscale clients are answered)")
	cacheDir := flag.String("cache", filepath.Join(cacheHome, "projector-media"), "index, metadata and segment cache folder")
	cacheGB := flag.Int64("cache-gb", 30, "disk space for cached HLS segments, in GB")
	tmdbKey := flag.String("tmdb-key", os.Getenv("TMDB_API_KEY"), "TMDB API key or read access token (default $TMDB_API_KEY); empty uses local metadata only")
	tmdbLang := flag.String("tmdb-lang", "en-US", "metadata language")
	maxHeight := flag.Int("max-height", 1080, "highest resolution transcodes are made at")
	maxCopyMbps := flag.Int64("max-copy-mbps", 80, "files above this bitrate are transcoded instead of streamed as is (0: no limit)")
	encoder := flag.String("encoder", "", "force an H.264 encoder (h264_nvenc, h264_qsv, libx264); default picks the best that works")
	ffmpeg := flag.String("ffmpeg", "ffmpeg", "ffmpeg binary")
	ffprobe := flag.String("ffprobe", "ffprobe", "ffprobe binary")
	name := flag.String("name", hostname, "name shown on the remote")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	for _, bin := range []string{*ffmpeg, *ffprobe} {
		if _, err := exec.LookPath(bin); err != nil {
			log.Fatalf("mediaserver: %s not found; install ffmpeg", bin)
		}
	}

	warm := make(chan *medialib.File, 256)
	lib, err := medialib.Open(medialib.Config{
		Root: *mediaDir, CacheDir: *cacheDir, FFprobe: *ffprobe,
		OnIndexed: func(files []*medialib.File) {
			for _, f := range files {
				select {
				case warm <- f:
				default:
				}
			}
		},
	})
	if err != nil {
		log.Fatalf("mediaserver: %v", err)
	}
	meta := medialib.NewMetadata(*tmdbKey, *tmdbLang, *cacheDir, nil)
	if !meta.Enabled() {
		log.Printf("mediaserver: no TMDB key; titles use folder names, .nfo files and local artwork")
	}

	enc := mediastream.DetectEncoder(*ffmpeg, *encoder)
	log.Printf("mediaserver: transcoding with %s", enc.Name)
	streams := &mediastream.Manager{
		FFmpeg: *ffmpeg, FFprobe: *ffprobe, Enc: enc, Lib: lib,
		Opt:        mediastream.Options{MaxHeight: *maxHeight, MaxCopyBitrate: *maxCopyMbps * 1_000_000, SegmentSeconds: 4},
		Dir:        filepath.Join(*cacheDir, "hls"),
		CacheLimit: *cacheGB << 30,
	}
	if err := streams.Init(ctx); err != nil {
		log.Fatalf("mediaserver: %v", err)
	}
	subs := &mediastream.Subtitles{FFmpeg: *ffmpeg, Dir: filepath.Join(*cacheDir, "subs"), Lib: lib}
	thumbs := &mediastream.Thumbs{FFmpeg: *ffmpeg, Dir: filepath.Join(*cacheDir, "thumbs"), Lib: lib}

	srv := &mediaserver.Server{Lib: lib, Meta: meta, Streams: streams, Subs: subs, Thumbs: thumbs, Name: *name,
		Warm: func(f *medialib.File) {
			select {
			case warm <- f:
			default:
			}
		}}

	// Serve the cached index right away; the scan refreshes it.
	go func() {
		lib.Scan(ctx)
		if err := lib.Watch(ctx); err != nil {
			log.Printf("mediaserver: watching %s failed, new files won't appear until restart: %v", lib.Root(), err)
		}
	}()
	go meta.Run(ctx)
	go func() {
		// One file at a time: this is background work.
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-warm:
				if cur := lib.File(f.Rel); cur != nil && cur.Probe != nil {
					streams.Warm(ctx, cur)
					subs.Tracks(ctx, cur)
				}
			}
		}
	}()

	hs := &http.Server{Addr: *listen, Handler: tailnetOnly(srv.Handler()), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	log.Printf("mediaserver: serving %s on %s", lib.Root(), *listen)
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("mediaserver: %v", err)
	}
	lib.Save()
}

// The tailnet's address ranges. The library has no login, so it answers
// only this machine and devices on the tailnet, never the LAN or a
// public network the laptop happens to be on.
var tailnets = []*net.IPNet{
	mustCIDR("100.64.0.0/10"),
	mustCIDR("fd7a:115c:a1e0::/48"),
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func tailnetOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		ok := err == nil && ip != nil && ip.IsLoopback()
		for _, n := range tailnets {
			if ip != nil && n.Contains(ip) {
				ok = true
			}
		}
		if !ok {
			http.Error(w, "this media server only answers devices on the tailnet", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

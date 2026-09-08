package main

//go:generate npm --prefix ../../web run build

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"projectorremote/internal/adbctl"
	"projectorremote/internal/discover"
	"projectorremote/internal/wsserver"
	webdist "projectorremote/web"
)

// discoverTimeout bounds a single mDNS browse attempt when no manual
// target is pending, so the reconnect loop's existing backoff still
// governs the retry cadence rather than blocking indefinitely.
const discoverTimeout = 5 * time.Second

func newMux(distFS fs.FS, hub http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(distFS)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/ws", hub)
	return mux
}

// discoverTarget adapts discover.Find to adbctl.DiscoverFunc.
func discoverTarget(ctx context.Context) (string, int, error) {
	return discover.Find(ctx, discoverTimeout)
}

func main() {
	listenAddr := flag.String("listen", ":8080", "address to listen on")
	target := flag.String("target", "", "initial projector ADB target, e.g. 192.168.1.50:5555 (optional; omit to start in mDNS discovery mode, same as a runtime manual override)")
	flag.Parse()

	if _, err := exec.LookPath("adb"); err != nil {
		log.Fatalf("main: adb not found in PATH: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	distFS, err := fs.Sub(webdist.FS, "dist")
	if err != nil {
		log.Fatalf("main: bad embedded dist: %v", err)
	}

	var hub *wsserver.Hub
	session := adbctl.NewSession(*target, discoverTarget, func(connected bool) {
		if hub != nil {
			hub.BroadcastStatus(connected)
		}
	})
	hub = wsserver.NewHub(session)

	mux := newMux(distFS, hub)
	srv := &http.Server{Addr: *listenAddr, Handler: mux}

	go session.Run(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if *target != "" {
		log.Printf("main: listening on %s, initial target %s (falls back to mDNS discovery on disconnect)", *listenAddr, *target)
	} else {
		log.Printf("main: listening on %s, searching for projector via mDNS", *listenAddr)
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("main: serve: %v", err)
	}
}

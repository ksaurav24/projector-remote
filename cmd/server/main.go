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
	"projectorremote/internal/wsserver"
	webdist "projectorremote/web"
)

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

func main() {
	listenAddr := flag.String("listen", ":8080", "address to listen on")
	target := flag.String("target", "", "projector ADB target, e.g. 192.168.1.50:5555")
	flag.Parse()

	if *target == "" {
		log.Fatal("main: -target is required, e.g. -target=192.168.1.50:5555")
	}
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
	session := adbctl.NewSession(*target, func(connected bool) {
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

	log.Printf("main: listening on %s, target device %s", *listenAddr, *target)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("main: serve: %v", err)
	}
}

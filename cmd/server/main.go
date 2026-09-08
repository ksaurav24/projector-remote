package main

//go:generate npm --prefix ../../web run build

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"projectorremote/internal/deviceio"
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
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	distFS, err := fs.Sub(webdist.FS, "dist")
	if err != nil {
		log.Fatalf("main: bad embedded dist: %v", err)
	}

	// Runs on the projector itself: device I/O setup failing here is
	// fatal, not something to retry in the background, since it's
	// local hardware rather than a network link that can be expected
	// to drop and reconnect.
	ctrl, err := deviceio.New(ctx)
	if err != nil {
		log.Fatalf("main: device setup: %v", err)
	}

	hub := wsserver.NewHub(ctrl)
	mux := newMux(distFS, hub)
	srv := &http.Server{Addr: *listenAddr, Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("main: listening on %s", *listenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("main: serve: %v", err)
	}
}

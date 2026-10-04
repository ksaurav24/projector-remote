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

	"projectorremote/internal/anime"
	"projectorremote/internal/catalog"
	"projectorremote/internal/catalogapi"
	"projectorremote/internal/deviceio"
	"projectorremote/internal/history"
	"projectorremote/internal/netmirror"
	"projectorremote/internal/playback"
	"projectorremote/internal/wsserver"
	webdist "projectorremote/web"
)

func newMux(distFS fs.FS, hub, catalog, player http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	static := http.FileServer(http.FS(distFS))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The app shell and service worker must always revalidate so a single
		// refresh picks up a deployment. Fingerprinted assets can be cached forever.
		if len(r.URL.Path) >= len("/assets/") && r.URL.Path[:len("/assets/")] == "/assets/" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		static.ServeHTTP(w, r)
	}))
	mux.Handle("/api/catalog/", catalog)
	mux.Handle("/player", player)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/ws", hub)
	return mux
}

// androidCACerts is Android's system CA store. A GOOS=linux build (see
// docs/device-guide.md §3) only looks in Linux locations like
// /etc/ssl/certs, which don't exist here, so without this every HTTPS
// request fails with "certificate signed by unknown authority".
const androidCACerts = "/system/etc/security/cacerts"

func main() {
	// Must run before the first TLS handshake: Go loads the system
	// roots once, lazily.
	if _, set := os.LookupEnv("SSL_CERT_DIR"); !set {
		os.Setenv("SSL_CERT_DIR", androidCACerts)
	}

	listenAddr := flag.String("listen", ":8080", "address to listen on")
	statePath := flag.String("state", "/data/local/tmp/projector-remote-state.json", "watch history and track preferences file")
	tokenPath := flag.String("token", "/data/local/tmp/projector-remote-token", "NetMirror login token file")
	animeSearch := flag.String("anime-search", "https://kitsu.io/api/edge/anime", "partial-title search endpoint")
	animeEpisodes := flag.String("anime-episodes", "https://api.ani.zip/mappings", "episode metadata endpoint")
	animeList := flag.String("anime-metadata", "https://graphql.anilist.co", "AniList GraphQL endpoint")
	animeNeko := flag.String("anime-anineko", "https://anineko.to", "AniNeko site URL")
	animeKoto := flag.String("anime-anikoto", "https://anikototv.to", "AniKoto site URL")
	animeRe := flag.String("anime-reanime", "", "self-hosted ReAnime API base URL; empty disables it")
	animeZone := flag.String("anime-anizone", "https://anizone.to", "AniZone site URL")
	animeGG := flag.String("anime-animegg", "https://www.animegg.org", "AnimeGG site URL")
	animeBD := flag.String("anime-anibd", "https://epeng.animeapps.top", "AniBD API URL")
	animeSenshi := flag.String("anime-senshi", "https://senshi.to", "Senshi site URL")
	animeKAA := flag.String("anime-kaa", "https://kaa.lt", "Kickassanime API URL")
	animeDunya := flag.String("anime-animedunya", "https://anime-dunya.com", "AnimeDunya site URL")
	animeDBApp := flag.String("anime-anidbapp", "https://anidb.app", "AniDB.app site URL")
	animeNoSub := flag.String("anime-animenosub", "https://animenosub.to", "AnimeNoSub site URL")
	animeOnsen := flag.String("anime-animeonsen", "https://www.animeonsen.xyz", "AnimeOnsen site URL")
	animeWaves := flag.String("anime-aniwaves", "https://aniwaves.ru", "AniWaves site URL")
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

	hist, err := history.Open(*statePath)
	if err != nil {
		// A corrupt history file shouldn't keep the remote from
		// starting; it gets overwritten on the next save.
		log.Printf("main: %v; starting with empty history", err)
		hist = history.New(*statePath)
	}
	tokens := netmirror.TokenFile{Path: *tokenPath, Fallback: netmirror.MMKVToken{Path: netmirror.DefaultMMKVPath}}
	nm := netmirror.New(tokens)
	animeService := anime.New(anime.Config{
		SearchURL: *animeSearch, EpisodesURL: *animeEpisodes, AniListURL: *animeList, AniNekoURL: *animeNeko, AniKotoURL: *animeKoto,
		ReAnimeURL: *animeRe, AniZoneURL: *animeZone, AnimeGGURL: *animeGG, AniBDURL: *animeBD,
		SenshiURL: *animeSenshi, KAAURL: *animeKAA, AnimeDunyaURL: *animeDunya,
		AniDBAppURL: *animeDBApp, AnimeNoSubURL: *animeNoSub,
		AnimeOnsenURL: *animeOnsen,
		AniWavesURL:   *animeWaves,
	})
	cat := &catalog.Router{Default: nm, Anime: animeService}

	var hub *wsserver.Hub
	pb := playback.New(playback.Deps{
		Catalog: cat,
		History: hist,
		Launch:  playback.LaunchPlayer,
		// hub is set below, before the server accepts anything that
		// could start playback.
		OnChange: func(np *playback.NowPlaying) { hub.BroadcastNowPlaying(np) },
	})
	hub = wsserver.NewHub(ctrl, pb)
	mux := newMux(distFS, hub, catalogapi.New(cat, hist, tokens), http.HandlerFunc(pb.ServePlayer))
	compat := anime.CompatHandler(animeService)
	mux.Handle("/map/", compat)
	mux.Handle("/episodes/", compat)
	mux.Handle("/watch/", compat)
	mux.Handle("/stream/", compat)
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

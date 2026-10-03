package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestNewMux_ServesEmbeddedIndex(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("<h1>placeholder</h1>")},
	}
	mux := newMux(fsys, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "<h1>placeholder</h1>" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
}

func TestNewMux_CachesFingerprintedAssets(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":        {Data: []byte("x")},
		"assets/app-123.js": {Data: []byte("js")},
	}
	mux := newMux(fsys, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-123.js", nil))

	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestNewMux_Healthz(t *testing.T) {
	fsys := fstest.MapFS{"index.html": {Data: []byte("x")}}
	mux := newMux(fsys, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestNewMux_RoutesWsPathToHub(t *testing.T) {
	fsys := fstest.MapFS{"index.html": {Data: []byte("x")}}
	called := false
	hub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mux := newMux(fsys, hub, http.NotFoundHandler(), http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected /ws to route to hub handler")
	}
}

func TestNewMux_RoutesCatalogAPI(t *testing.T) {
	fsys := fstest.MapFS{"index.html": {Data: []byte("x")}}
	var gotPath string
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	mux := newMux(fsys, http.NotFoundHandler(), api, http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/home?ott=nf", nil)
	mux.ServeHTTP(httptest.NewRecorder(), req)

	if gotPath != "/api/catalog/home" {
		t.Fatalf("catalog handler got path %q, want /api/catalog/home", gotPath)
	}
}

func TestNewMux_RoutesPlayerPath(t *testing.T) {
	fsys := fstest.MapFS{"index.html": {Data: []byte("x")}}
	called := false
	player := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	mux := newMux(fsys, http.NotFoundHandler(), http.NotFoundHandler(), player)

	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/player", nil))

	if !called {
		t.Fatal("expected /player to route to the player handler")
	}
}

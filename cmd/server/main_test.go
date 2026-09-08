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
	mux := newMux(fsys, http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "<h1>placeholder</h1>" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestNewMux_Healthz(t *testing.T) {
	fsys := fstest.MapFS{"index.html": {Data: []byte("x")}}
	mux := newMux(fsys, http.NotFoundHandler())

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
	mux := newMux(fsys, hub)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected /ws to route to hub handler")
	}
}

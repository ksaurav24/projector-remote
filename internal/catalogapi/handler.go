// Package catalogapi serves provider-neutral catalogue JSON over HTTP,
// enriched with resume points and watch history. NetMirror's OTP login is
// retained for the existing movie and TV catalogues.
package catalogapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"projectorremote/internal/catalog"
	"projectorremote/internal/history"
	"projectorremote/internal/netmirror"
)

// Catalog is the normalized catalogue and login interface.
type Catalog interface {
	Home(ctx context.Context, ott string) (*catalog.Home, error)
	Search(ctx context.Context, ott, q string) ([]catalog.SearchItem, error)
	Title(ctx context.Context, ott, id string) (*catalog.Title, error)
	Season(ctx context.Context, ott, seasonID string, page int) (*catalog.EpisodePage, error)
	LoginImage(ctx context.Context) (string, error)
	Login(ctx context.Context, otp string) (string, error)
}

// Tokens keeps the usertoken a login issues (*netmirror.TokenFile).
type Tokens interface {
	Save(token string) error
}

// History is the subset of *history.Store this package uses.
type History interface {
	Bookmarks() []history.Bookmark
	SetBookmark(history.Bookmark, bool) error
	Continue() []history.ContinueItem
	Resume(ott, titleID string) *history.Resume
	Position(ott, id string) (history.Position, bool)
}

type handler struct {
	cat    Catalog
	hist   History
	tokens Tokens
}

func New(cat Catalog, hist History, tokens Tokens) http.Handler {
	h := &handler{cat: cat, hist: hist, tokens: tokens}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/catalog/bookmarks", h.bookmarks)
	mux.HandleFunc("PUT /api/catalog/bookmarks", h.setBookmark)
	mux.HandleFunc("DELETE /api/catalog/bookmarks", h.setBookmark)
	mux.HandleFunc("GET /api/catalog/sources", h.sources)
	mux.HandleFunc("GET /api/catalog/home", h.home)
	mux.HandleFunc("GET /api/catalog/search", h.search)
	mux.HandleFunc("GET /api/catalog/title", h.title)
	mux.HandleFunc("GET /api/catalog/season", h.season)
	mux.HandleFunc("GET /api/catalog/continue", h.continueWatching)
	mux.HandleFunc("GET /api/catalog/login", h.loginImage)
	mux.HandleFunc("POST /api/catalog/login", h.login)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// upstreamError maps a netmirror failure to a response: 404 for an
// unknown title, 502 for anything else (NetMirror down, slow, or
// answering something unparseable).
func upstreamError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, catalog.ErrInvalidArgument) {
		writeError(w, http.StatusBadRequest, "invalid catalogue request")
		return
	}
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "title not found")
		return
	}
	if errors.Is(err, catalog.ErrOffline) {
		writeError(w, http.StatusServiceUnavailable, "The media server on your laptop isn't reachable. Check it's running and on Tailscale.")
		return
	}
	log.Printf("catalogapi: %s: %v", r.URL.Path, err)
	writeError(w, http.StatusBadGateway, "The catalogue is not responding. Try again.")
}

// ott reads and validates the ott query parameter, writing a 400 and
// returning false if it's missing or unknown.
func ott(w http.ResponseWriter, r *http.Request) (string, bool) {
	o := r.URL.Query().Get("ott")
	if !catalog.ValidOTT(o) {
		writeError(w, http.StatusBadRequest, "ott must be nf, pv, hs, anime, or local")
		return "", false
	}
	return o, true
}

func required(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		writeError(w, http.StatusBadRequest, name+" is required")
		return "", false
	}
	return v, true
}

func (h *handler) home(w http.ResponseWriter, r *http.Request) {
	o, ok := ott(w, r)
	if !ok {
		return
	}
	home, err := h.cat.Home(r.Context(), o)
	if err != nil {
		upstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, home)
}

func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	o, ok := ott(w, r)
	if !ok {
		return
	}
	q, ok := required(w, r, "q")
	if !ok {
		return
	}
	items, err := h.cat.Search(r.Context(), o, q)
	if err != nil {
		upstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type titleResponse struct {
	*catalog.Title
	Resume *history.Resume `json:"resume"`
	// Progress holds saved positions for the movie, or for the started
	// episodes in Episodes, keyed by ID.
	Progress map[string]history.Position `json:"progress"`
}

type seasonResponse struct {
	*catalog.EpisodePage
	Progress map[string]history.Position `json:"progress"`
}

func (h *handler) progress(ott string, ids ...string) map[string]history.Position {
	m := make(map[string]history.Position)
	for _, id := range ids {
		if p, ok := h.hist.Position(ott, id); ok {
			m[id] = p
		}
	}
	return m
}

func episodeIDs(page *catalog.EpisodePage) []string {
	if page == nil {
		return nil
	}
	ids := make([]string, len(page.Items))
	for i, e := range page.Items {
		ids[i] = e.ID
	}
	return ids
}

func (h *handler) title(w http.ResponseWriter, r *http.Request) {
	o, ok := ott(w, r)
	if !ok {
		return
	}
	id, ok := required(w, r, "id")
	if !ok {
		return
	}
	t, err := h.cat.Title(r.Context(), o, id)
	if err != nil {
		upstreamError(w, r, err)
		return
	}
	resume := h.hist.Resume(o, t.ID)

	// NetMirror opens a series on its latest season. The detail screen
	// opens where Play would start instead: the season being watched,
	// or Season 1.
	if t.Type == "series" && len(t.Seasons) > 0 {
		target := t.Seasons[0].ID
		if o == "anime" && t.SeasonID != "" {
			target = t.SeasonID
		}
		if resume != nil && resume.SeasonID != "" {
			target = resume.SeasonID
		}
		if target != t.SeasonID {
			page, err := h.cat.Season(r.Context(), o, target, 1)
			if err != nil {
				upstreamError(w, r, err)
				return
			}
			t.SeasonID, t.Episodes = target, page
		}
	}
	ids := episodeIDs(t.Episodes)
	if t.Type == "movie" {
		ids = []string{t.ID}
	}
	writeJSON(w, http.StatusOK, titleResponse{Title: t, Resume: resume, Progress: h.progress(o, ids...)})
}

func (h *handler) season(w http.ResponseWriter, r *http.Request) {
	o, ok := ott(w, r)
	if !ok {
		return
	}
	id, ok := required(w, r, "id")
	if !ok {
		return
	}
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "page must be a positive integer")
			return
		}
		page = n
	}
	eps, err := h.cat.Season(r.Context(), o, id, page)
	if err != nil {
		upstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, seasonResponse{EpisodePage: eps, Progress: h.progress(o, episodeIDs(eps)...)})
}

// sources says which optional catalogues are reachable now, so the remote
// shows the laptop library only while its server is up.
func (h *handler) sources(w http.ResponseWriter, r *http.Request) {
	local := false
	if a, ok := h.cat.(interface{ Available(string) bool }); ok {
		local = a.Available("local")
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]bool{"local": local})
}

func (h *handler) continueWatching(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.hist.Continue()})
}

func (h *handler) loginImage(w http.ResponseWriter, r *http.Request) {
	img, err := h.cat.LoginImage(r.Context())
	if err != nil {
		upstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"image": img})
}

// login runs NetMirror's OTP step from the projector, so the token is
// issued to the same public IP that will play the streams.
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OTP string `json:"otp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.OTP == "" {
		writeError(w, http.StatusBadRequest, "otp is required")
		return
	}
	tok, err := h.cat.Login(r.Context(), body.OTP)
	if errors.Is(err, netmirror.ErrBadOTP) {
		writeError(w, http.StatusUnauthorized, "Wrong code. Check the one in the picture.")
		return
	}
	if err != nil {
		upstreamError(w, r, err)
		return
	}
	if err := h.tokens.Save(tok); err != nil {
		log.Printf("catalogapi: save token: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save the login on the projector")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) bookmarks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": h.hist.Bookmarks()})
}
func (h *handler) setBookmark(w http.ResponseWriter, r *http.Request) {
	o, ok := ott(w, r)
	if !ok {
		return
	}
	id, ok := required(w, r, "id")
	if !ok {
		return
	}
	if len(id) > 512 {
		writeError(w, 400, "title ID is too long")
		return
	}
	b := history.Bookmark{OTT: o, TitleID: id}
	saved := r.Method == http.MethodPut
	if saved {
		var body struct {
			Title  string `json:"title"`
			Poster string `json:"poster"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" || len(body.Title) > 1024 || len(body.Poster) > 8192 {
			writeError(w, 400, "a title and valid poster are required")
			return
		}
		b.Title, b.Poster = body.Title, body.Poster
	}
	if err := h.hist.SetBookmark(b, saved); err != nil {
		log.Printf("catalogapi: bookmark: %v", err)
		writeError(w, 500, "Couldn't save bookmarks on the projector. Try again.")
		return
	}
	h.bookmarks(w, r)
}

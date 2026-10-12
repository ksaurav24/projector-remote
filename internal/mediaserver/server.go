// Package mediaserver is the HTTP face of the laptop's media library: the
// catalogue in the projector's normalized shapes, HLS playlists and
// segments, subtitles, and artwork. The projector discovers it over
// Tailscale and plays what it serves.
package mediaserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"projectorremote/internal/catalog"
	"projectorremote/internal/medialib"
	"projectorremote/internal/mediastream"
)

const pageSize = 50

type Server struct {
	Lib     *medialib.Library
	Meta    *medialib.Metadata
	Streams *mediastream.Manager
	Subs    *mediastream.Subtitles
	Thumbs  *mediastream.Thumbs
	Name    string
	// Warm queues background preparation (keyframes, subtitles) of a
	// file that's likely to play soon.
	Warm func(*medialib.File)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/home", s.home)
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("GET /api/title", s.title)
	mux.HandleFunc("GET /api/season", s.season)
	mux.HandleFunc("GET /api/resolve", s.resolve)
	mux.HandleFunc("GET /hls/{key}/master.m3u8", s.master)
	mux.HandleFunc("GET /hls/{key}/{rend}/{seg}", s.segment)
	mux.HandleFunc("GET /hls/{key}/{file}", s.media)
	mux.HandleFunc("GET /sub/{file}/{name}", s.subtitle)
	mux.HandleFunc("GET /art/file/", s.artFile)
	mux.HandleFunc("GET /art/thumb/{id}", s.artThumb)
	return logRequests(mux)
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, code: 200}
		h.ServeHTTP(rw, r)
		// Segment fetches are constant during playback; log only the slow
		// or failed ones.
		d := time.Since(start)
		if !strings.HasPrefix(r.URL.Path, "/hls/") || rw.code >= 400 || d > 2*time.Second {
			log.Printf("http: %s %s %d %s", r.Method, r.URL.RequestURI(), rw.code, d.Round(time.Millisecond))
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	c := s.Lib.Catalog()
	writeJSON(w, 200, map[string]any{"service": "projector-media", "version": 1, "name": s.Name,
		"shows": len(c.Shows), "movies": len(c.Movies), "metadata": s.Meta.Enabled()})
}

// --- Presentation: merge parsed names, .nfo files, TMDB and local art ---

type view struct {
	id, typ, title, desc, poster, backdrop, rating string
	year                                           int
	genres                                         []string
}

func (s *Server) showView(sh *medialib.Show) view {
	v := view{id: sh.ID, typ: "series", title: sh.Name, year: sh.Year}
	if m := s.Meta.Show(sh); m != nil {
		v.title, v.desc, v.poster, v.backdrop, v.genres = orStr(m.Title, v.title), m.Overview, m.Poster, m.Backdrop, m.Genres
		v.year = orInt(m.Year, v.year)
		v.rating = score(m.Rating)
	}
	if n := sh.NFO; n != nil {
		v.title, v.desc, v.year = orStr(n.Title, v.title), orStr(n.Plot, v.desc), orInt(n.Year, v.year)
		if n.Rating > 0 {
			v.rating = score(n.Rating)
		}
		if len(n.Genres) > 0 {
			v.genres = n.Genres
		}
	}
	if sh.Art.Poster != "" {
		v.poster = artFile(sh.Art.Poster)
	}
	if sh.Art.Backdrop != "" {
		v.backdrop = artFile(sh.Art.Backdrop)
	}
	if first := firstEpisode(sh); first != nil {
		thumb := "/art/thumb/" + first.ID
		v.poster, v.backdrop = orStr(v.poster, thumb), orStr(v.backdrop, thumb)
	}
	return v
}

func (s *Server) movieView(mv *medialib.Movie) view {
	v := view{id: mv.ID, typ: "movie", title: mv.Name, year: mv.Year}
	if m := s.Meta.Movie(mv); m != nil {
		v.title, v.desc, v.poster, v.backdrop, v.genres = orStr(m.Title, v.title), m.Overview, m.Poster, m.Backdrop, m.Genres
		v.year = orInt(m.Year, v.year)
		v.rating = score(m.Rating)
	}
	if n := mv.NFO; n != nil {
		v.title, v.desc, v.year = orStr(n.Title, v.title), orStr(n.Plot, v.desc), orInt(n.Year, v.year)
		if n.Rating > 0 {
			v.rating = score(n.Rating)
		}
	}
	if mv.Art.Poster != "" {
		v.poster = artFile(mv.Art.Poster)
	}
	if mv.Art.Backdrop != "" {
		v.backdrop = artFile(mv.Art.Backdrop)
	}
	thumb := "/art/thumb/" + mv.ID
	v.poster, v.backdrop = orStr(v.poster, thumb), orStr(v.backdrop, thumb)
	return v
}

func (s *Server) titleView(t medialib.Title) view {
	switch t := t.(type) {
	case *medialib.Show:
		return s.showView(t)
	case *medialib.Movie:
		return s.movieView(t)
	}
	return view{}
}

func firstEpisode(sh *medialib.Show) *medialib.Episode {
	for _, se := range sh.Seasons {
		if len(se.Episodes) > 0 && se.Num != 0 {
			return se.Episodes[0]
		}
	}
	if len(sh.Seasons) > 0 && len(sh.Seasons[0].Episodes) > 0 {
		return sh.Seasons[0].Episodes[0]
	}
	return nil
}

// artFile is the URL of an image inside the library.
func artFile(rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "/art/file/" + strings.Join(parts, "/")
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func orInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func score(r float64) string {
	if r <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f / 10", r)
}

func (v view) card() catalog.Poster {
	return catalog.Poster{ID: v.id, Title: v.title, Poster: v.poster}
}

// --- Catalogue ---

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	c := s.Lib.Catalog()
	h := catalog.Home{Slider: []catalog.Slide{}, Rows: []catalog.Row{}}
	for _, t := range c.Recent {
		if len(h.Slider) == 6 {
			break
		}
		v := s.titleView(t)
		h.Slider = append(h.Slider, catalog.Slide{ID: v.id, Title: v.title, Img: v.backdrop, Desc: v.desc, Rating: v.rating})
	}
	row := func(title string, items []catalog.Poster) {
		if len(items) > 0 {
			h.Rows = append(h.Rows, catalog.Row{Title: title, Layout: "v", Items: items})
		}
	}
	var recent, shows, movies []catalog.Poster
	for i, t := range c.Recent {
		if i == 20 {
			break
		}
		recent = append(recent, s.titleView(t).card())
	}
	for _, sh := range c.Shows {
		shows = append(shows, s.showView(sh).card())
	}
	for _, mv := range c.Movies {
		movies = append(movies, s.movieView(mv).card())
	}
	row("Recently Added", recent)
	row("TV Shows", shows)
	row("Movies", movies)

	// Genre rows once TMDB has tagged enough titles.
	byGenre := map[string][]catalog.Poster{}
	for _, t := range c.Recent {
		v := s.titleView(t)
		for _, g := range v.genres {
			byGenre[g] = append(byGenre[g], v.card())
		}
	}
	genres := make([]string, 0, len(byGenre))
	for g, items := range byGenre {
		if len(items) >= 4 {
			genres = append(genres, g)
		}
	}
	sort.Slice(genres, func(i, j int) bool { return len(byGenre[genres[i]]) > len(byGenre[genres[j]]) })
	for _, g := range genres[:min(len(genres), 6)] {
		row(g, byGenre[g])
	}
	writeJSON(w, 200, h)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := normalize(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 200, map[string]any{"items": []catalog.SearchItem{}})
		return
	}
	type hit struct {
		item  catalog.SearchItem
		score int
	}
	var hits []hit
	c := s.Lib.Catalog()
	for _, t := range c.Recent {
		v := s.titleView(t)
		best := 0
		for _, name := range []string{v.title, t.DisplayName()} {
			best = max(best, matchScore(q, normalize(name)))
		}
		if best > 0 {
			hits = append(hits, hit{catalog.SearchItem{ID: v.id, Title: v.title, Poster: v.poster}, best})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	items := make([]catalog.SearchItem, 0, len(hits))
	for _, h := range hits {
		items = append(items, h.item)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func normalize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	space := true
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127 {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// matchScore ranks a title against a query: whole-title match, then
// prefix, then every query word starting some title word, then a typo
// within one or two edits of a title word.
func matchScore(q, name string) int {
	switch {
	case name == q:
		return 100
	case strings.HasPrefix(name, q):
		return 80
	case strings.Contains(name, q):
		return 60
	}
	words := strings.Fields(name)
	all := true
	fuzzy := 0
	for _, qw := range strings.Fields(q) {
		found := false
		for _, w := range words {
			if strings.HasPrefix(w, qw) {
				found = true
				break
			}
			if len(qw) >= 4 && editDistance(qw, w[:min(len(w), len(qw)+1)]) <= 1+len(qw)/6 {
				found = true
				fuzzy++
				break
			}
		}
		if !found {
			all = false
			break
		}
	}
	if all {
		return 40 - fuzzy
	}
	return 0
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func runtimeOf(sec float64) string {
	m := int(sec / 60)
	if m >= 60 {
		return fmt.Sprintf("%dh %dm", m/60, m%60)
	}
	return fmt.Sprintf("%d min", max(m, 1))
}

func quality(f *medialib.File) string {
	v := f.Probe.Video
	q := ""
	switch {
	case v.Height >= 2000 || v.Width >= 3800:
		q = "4K"
	case v.Height >= 1000 || v.Width >= 1900:
		q = "1080p"
	case v.Height >= 700 || v.Width >= 1260:
		q = "720p"
	default:
		q = "SD"
	}
	if v.DolbyVision {
		q += " Dolby Vision"
	} else if v.HDR() {
		q += " HDR"
	}
	return q
}

func langs(f *medialib.File) []catalog.Lang {
	out := []catalog.Lang{}
	seen := map[string]bool{}
	for _, a := range f.Probe.Audio {
		if a.Lang == "" || seen[a.Lang] {
			continue
		}
		seen[a.Lang] = true
		name := mediastream.LangName(a.Lang)
		if name == "" {
			name = a.Lang
		}
		out = append(out, catalog.Lang{Code: a.Lang, Label: name})
	}
	return out
}

func seasonLabel(n int) string {
	if n == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(n)
}

func (s *Server) suggestions(c *medialib.Catalog, except string) []catalog.Poster {
	out := []catalog.Poster{}
	for _, t := range c.Recent {
		if len(out) == 12 {
			break
		}
		if t.TitleID() != except {
			out = append(out, s.titleView(t).card())
		}
	}
	return out
}

func (s *Server) title(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	c := s.Lib.Catalog()
	if mv := c.Movie(id); mv != nil {
		v := s.movieView(mv)
		writeJSON(w, 200, catalog.Title{ID: mv.ID, Type: "movie", Title: v.title, Year: yearStr(v.year), Rating: v.rating,
			Runtime: runtimeOf(mv.File.Probe.Duration), Quality: quality(mv.File), Desc: v.desc, Backdrop: v.backdrop,
			Poster: v.poster, Langs: langs(mv.File), Seasons: []catalog.Season{}, Suggest: s.suggestions(c, mv.ID)})
		return
	}
	sh := c.Show(id)
	if sh == nil {
		writeErr(w, 404, "title not found")
		return
	}
	v := s.showView(sh)
	t := catalog.Title{ID: sh.ID, Type: "series", Title: v.title, Year: yearStr(v.year), Rating: v.rating, Desc: v.desc,
		Backdrop: v.backdrop, Poster: v.poster, Langs: []catalog.Lang{}, Seasons: []catalog.Season{}, Suggest: s.suggestions(c, sh.ID)}
	if first := firstEpisode(sh); first != nil {
		t.Quality, t.Langs, t.Runtime = quality(first.File), langs(first.File), runtimeOf(first.File.Probe.Duration)
	}
	for _, se := range sh.Seasons {
		t.Seasons = append(t.Seasons, catalog.Season{ID: se.ID, Number: se.Num, Label: seasonLabel(se.Num), Episodes: len(se.Episodes)})
	}
	if len(sh.Seasons) > 0 {
		se := sh.Seasons[0]
		t.SeasonID = se.ID
		t.Episodes = s.episodePage(se, 1)
	}
	writeJSON(w, 200, t)
}

func yearStr(y int) string {
	if y == 0 {
		return ""
	}
	return strconv.Itoa(y)
}

func (s *Server) episodePage(se *medialib.Season, page int) *catalog.EpisodePage {
	from := (page - 1) * pageSize
	p := &catalog.EpisodePage{Items: []catalog.Episode{}}
	if from >= len(se.Episodes) {
		return p
	}
	to := min(from+pageSize, len(se.Episodes))
	for _, e := range se.Episodes[from:to] {
		ep := catalog.Episode{ID: e.ID, Num: e.Num(), Title: e.Title, Runtime: runtimeOf(e.File.Probe.Duration),
			Still: "/art/thumb/" + e.ID}
		if m := s.Meta.Episode(e); m != nil {
			ep.Title, ep.Desc = orStr(m.Title, ep.Title), m.Overview
			if m.Still != "" {
				ep.Still = m.Still
			}
		}
		if n := e.File.NFO; n != nil {
			ep.Title, ep.Desc = orStr(n.Title, ep.Title), orStr(n.Plot, ep.Desc)
		}
		if e.File.Thumb != "" {
			ep.Still = artFile(e.File.Thumb)
		}
		if ep.Title == "" {
			ep.Title = "Episode " + strconv.Itoa(e.Num())
		}
		if len(e.Nums) > 1 {
			ep.Title = fmt.Sprintf("%s (E%d-E%d)", ep.Title, e.Nums[0], e.Nums[len(e.Nums)-1])
		}
		p.Items = append(p.Items, ep)
	}
	if to < len(se.Episodes) {
		next := page + 1
		p.NextPage = &next
	}
	return p
}

func (s *Server) season(w http.ResponseWriter, r *http.Request) {
	se := s.Lib.Catalog().Season(r.URL.Query().Get("id"))
	if se == nil {
		writeErr(w, 404, "season not found")
		return
	}
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	writeJSON(w, 200, s.episodePage(se, page))
}

// --- Playback ---

type resolveResponse struct {
	URL       string             `json:"url"`
	MimeType  string             `json:"mimeType"`
	Subtitles []catalog.Subtitle `json:"subtitles"`
}

func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	f := s.Lib.Catalog().Playable(id)
	if f == nil {
		writeErr(w, 404, "not in the library")
		return
	}
	caps := mediastream.DefaultCaps()
	if raw := r.URL.Query().Get("caps"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &caps); err != nil {
			writeErr(w, 400, "bad caps")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	key, _, err := s.Streams.Prepare(ctx, f, caps)
	if err != nil {
		log.Printf("mediaserver: resolve %s: %v", id, err)
		writeErr(w, 500, "can't stream this file")
		return
	}
	if next := nextEpisode(s.Lib.Catalog(), id); next != nil && s.Warm != nil {
		s.Warm(next.File)
	}
	base := "http://" + r.Host
	resp := resolveResponse{URL: base + "/hls/" + key + "/master.m3u8", MimeType: "application/x-mpegURL", Subtitles: []catalog.Subtitle{}}
	for _, t := range s.Subs.Tracks(ctx, f) {
		resp.Subtitles = append(resp.Subtitles, catalog.Subtitle{URL: base + "/sub/" + f.ID() + "/" + t.Name,
			Label: t.Label, Language: t.Lang, MimeType: t.MimeType, Default: t.Default || t.Forced})
	}
	writeJSON(w, 200, resp)
}

// nextEpisode is the episode after id, in this season or the next.
func nextEpisode(c *medialib.Catalog, id string) *medialib.Episode {
	e := c.Episode(id)
	if e == nil {
		return nil
	}
	seasons := e.Show.Seasons
	for si, se := range seasons {
		if se != e.Season {
			continue
		}
		for i, x := range se.Episodes {
			if x == e && i+1 < len(se.Episodes) {
				return se.Episodes[i+1]
			}
		}
		if si+1 < len(seasons) && len(seasons[si+1].Episodes) > 0 && seasons[si+1].Num != 0 {
			return seasons[si+1].Episodes[0]
		}
	}
	return nil
}

func (s *Server) master(w http.ResponseWriter, r *http.Request) {
	body, err := s.Streams.Master(r.PathValue("key"))
	s.playlist(w, body, err)
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !strings.HasSuffix(name, ".m3u8") {
		http.NotFound(w, r)
		return
	}
	body, err := s.Streams.Media(r.PathValue("key"), strings.TrimSuffix(name, ".m3u8"))
	s.playlist(w, body, err)
}

func (s *Server) playlist(w http.ResponseWriter, body string, err error) {
	if errors.Is(err, mediastream.ErrUnknownStream) {
		writeErr(w, 404, "unknown stream")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(body))
}

func (s *Server) segment(w http.ResponseWriter, r *http.Request) {
	seg := r.PathValue("seg")
	k, err := strconv.Atoi(strings.TrimSuffix(seg, ".ts"))
	if err != nil || !strings.HasSuffix(seg, ".ts") {
		http.NotFound(w, r)
		return
	}
	p, err := s.Streams.Segment(r.Context(), r.PathValue("key"), r.PathValue("rend"), k)
	if errors.Is(err, mediastream.ErrUnknownStream) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if r.Context().Err() == nil {
			log.Printf("mediaserver: %v", err)
			http.Error(w, "segment unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, p)
}

func (s *Server) subtitle(w http.ResponseWriter, r *http.Request) {
	p := s.Subs.Path(r.PathValue("file"), r.PathValue("name"))
	if p == "" {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(p, ".ass") {
		w.Header().Set("Content-Type", "text/x-ssa; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	}
	http.ServeFile(w, r, p)
}

var artExts = []string{".jpg", ".jpeg", ".png", ".webp", ".tbn"}

func (s *Server) artFile(w http.ResponseWriter, r *http.Request) {
	rel, err := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/art/file/"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rel = path.Clean("/" + rel)[1:]
	if rel == "" || !slices.Contains(artExts, strings.ToLower(path.Ext(rel))) {
		http.NotFound(w, r)
		return
	}
	if strings.EqualFold(path.Ext(rel), ".tbn") {
		w.Header().Set("Content-Type", "image/jpeg")
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, filepath.Join(s.Lib.Root(), filepath.FromSlash(rel)))
}

func (s *Server) artThumb(w http.ResponseWriter, r *http.Request) {
	f := s.Lib.Catalog().Playable(r.PathValue("id"))
	if f == nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.Thumbs.Path(r.Context(), f)
	if err != nil {
		log.Printf("mediaserver: %v", err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, p)
}

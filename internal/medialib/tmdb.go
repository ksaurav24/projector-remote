package medialib

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Meta is online metadata for a show or movie.
type Meta struct {
	TMDBID   int      `json:"tmdbId"`
	Title    string   `json:"title"`
	Overview string   `json:"overview"`
	Year     int      `json:"year"`
	Rating   float64  `json:"rating"`
	Runtime  int      `json:"runtime,omitempty"`
	Poster   string   `json:"poster"`
	Backdrop string   `json:"backdrop"`
	Genres   []string `json:"genres,omitempty"`
	// SeasonCounts is each TMDB season's episode count, used to place
	// absolutely numbered anime episodes ("One Piece - 1085").
	SeasonCounts map[int]int         `json:"seasonCounts,omitempty"`
	Seasons      map[int]*SeasonMeta `json:"seasons,omitempty"`
	// Miss marks a lookup that found nothing, retried after missRetry.
	Miss    bool  `json:"miss,omitempty"`
	Fetched int64 `json:"fetched"`
}

type SeasonMeta struct {
	Poster   string          `json:"poster"`
	Overview string          `json:"overview"`
	Episodes map[int]*EpMeta `json:"episodes"`
}

type EpMeta struct {
	Title    string `json:"title"`
	Overview string `json:"overview"`
	Still    string `json:"still"`
	Runtime  int    `json:"runtime"`
}

const (
	tmdbAPI   = "https://api.themoviedb.org/3"
	tmdbImg   = "https://image.tmdb.org/t/p/"
	missRetry = 7 * 24 * time.Hour
	metaTTL   = 30 * 24 * time.Hour
)

// Metadata looks titles up on TMDB in the background and keeps the
// results on disk. Lookups never block a request: an uncached title is
// queued and shows parsed names until its metadata arrives.
type Metadata struct {
	key, lang string
	path      string
	client    *http.Client

	mu      sync.Mutex
	items   map[string]*Meta
	queued  map[string]bool
	queue   chan metaReq
	dirty   bool
	updated func()
}

type metaReq struct {
	cacheKey string
	tv       bool
	name     string
	year     int
	tmdbID   int
	season   int // >0 (or 0 for specials) to fetch a season's episodes; -1 for the title
}

// NewMetadata returns a store; with an empty apiKey it never goes online.
// updated is called after new metadata lands.
func NewMetadata(apiKey, lang, cacheDir string, updated func()) *Metadata {
	m := &Metadata{key: apiKey, lang: lang, path: filepath.Join(cacheDir, "metadata.json"),
		client: &http.Client{Timeout: 15 * time.Second}, items: map[string]*Meta{}, queued: map[string]bool{},
		queue: make(chan metaReq, 4096), updated: updated}
	if raw, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(raw, &m.items)
	}
	return m
}

func (m *Metadata) Enabled() bool { return m.key != "" }

func showKey(s *Show) string   { return "tv:" + s.Key }
func movieKey(v *Movie) string { return "mv:" + v.Key + "|" + strconv.Itoa(v.Year) }

// Show returns cached metadata for s (nil if none yet) and queues a
// lookup when needed.
func (m *Metadata) Show(s *Show) *Meta {
	id := 0
	if s.NFO != nil {
		id = s.NFO.TMDBID
	}
	return m.get(metaReq{cacheKey: showKey(s), tv: true, name: s.Name, year: s.Year, tmdbID: id, season: -1})
}

func (m *Metadata) Movie(v *Movie) *Meta {
	id := 0
	if v.NFO != nil {
		id = v.NFO.TMDBID
	}
	return m.get(metaReq{cacheKey: movieKey(v), name: v.Name, year: v.Year, tmdbID: id, season: -1})
}

// Episode returns TMDB's details for an episode, mapping absolute
// numbering onto TMDB's seasons when the local season doesn't have it.
func (m *Metadata) Episode(e *Episode) *EpMeta {
	meta := m.Show(e.Show)
	if meta == nil || meta.Miss {
		return nil
	}
	season, num := e.Season.Num, e.Num()
	if c, ok := meta.SeasonCounts[season]; ok && num > c && season <= 1 {
		// "One Piece - 1085" in a single local season: count through
		// TMDB's seasons to find where episode 1085 falls.
		left := num
		for s := 1; s <= len(meta.SeasonCounts); s++ {
			c, ok := meta.SeasonCounts[s]
			if !ok {
				break
			}
			if left <= c {
				season, num = s, left
				break
			}
			left -= c
		}
	}
	sm := m.Season(e.Show, meta, season)
	if sm == nil {
		return nil
	}
	return sm.Episodes[num]
}

// Season returns a season's metadata, queueing its fetch if needed.
func (m *Metadata) Season(s *Show, meta *Meta, num int) *SeasonMeta {
	if meta == nil || meta.TMDBID == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if sm := meta.Seasons[num]; sm != nil {
		return sm
	}
	if _, exists := meta.SeasonCounts[num]; !exists {
		return nil
	}
	m.enqueueLocked(metaReq{cacheKey: showKey(s), tv: true, tmdbID: meta.TMDBID, season: num})
	return nil
}

func (m *Metadata) get(r metaReq) *Meta {
	if m.key == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	meta := m.items[r.cacheKey]
	stale := meta == nil ||
		(meta.Miss && time.Since(time.Unix(meta.Fetched, 0)) > missRetry) ||
		(!meta.Miss && time.Since(time.Unix(meta.Fetched, 0)) > metaTTL) ||
		(r.tmdbID != 0 && meta.TMDBID != r.tmdbID)
	if stale {
		m.enqueueLocked(r)
	}
	if meta == nil || meta.Miss {
		return nil
	}
	return meta
}

func (m *Metadata) enqueueLocked(r metaReq) {
	qk := r.cacheKey + "#" + strconv.Itoa(r.season)
	if m.queued[qk] {
		return
	}
	select {
	case m.queue <- r:
		m.queued[qk] = true
	default:
	}
}

// Run processes lookups until ctx ends.
func (m *Metadata) Run(ctx context.Context) {
	if m.key == "" {
		return
	}
	save := time.NewTicker(10 * time.Second)
	defer save.Stop()
	for {
		select {
		case <-ctx.Done():
			m.save()
			return
		case <-save.C:
			m.save()
		case r := <-m.queue:
			m.fetch(ctx, r)
			m.mu.Lock()
			delete(m.queued, r.cacheKey+"#"+strconv.Itoa(r.season))
			m.mu.Unlock()
			if m.updated != nil {
				m.updated()
			}
			// Well under TMDB's rate limit.
			time.Sleep(60 * time.Millisecond)
		}
	}
}

func (m *Metadata) save() {
	m.mu.Lock()
	if !m.dirty {
		m.mu.Unlock()
		return
	}
	raw, err := json.Marshal(m.items)
	m.dirty = false
	m.mu.Unlock()
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err == nil {
		_ = os.Rename(tmp, m.path)
	}
}

func (m *Metadata) fetch(ctx context.Context, r metaReq) {
	if r.season >= 0 {
		sm, err := m.fetchSeason(ctx, r.tmdbID, r.season)
		if err != nil {
			log.Printf("medialib: tmdb season %d of %d: %v", r.season, r.tmdbID, err)
			return
		}
		m.mu.Lock()
		if meta := m.items[r.cacheKey]; meta != nil {
			if meta.Seasons == nil {
				meta.Seasons = map[int]*SeasonMeta{}
			}
			meta.Seasons[r.season] = sm
			m.dirty = true
		}
		m.mu.Unlock()
		return
	}
	meta, err := m.fetchTitle(ctx, r)
	if err != nil {
		// Network trouble: don't record a miss, try again next time.
		log.Printf("medialib: tmdb %q: %v", r.name, err)
		return
	}
	if meta == nil {
		log.Printf("medialib: tmdb has no match for %q (%d)", r.name, r.year)
		meta = &Meta{Miss: true}
	}
	meta.Fetched = time.Now().Unix()
	m.mu.Lock()
	m.items[r.cacheKey] = meta
	m.dirty = true
	m.mu.Unlock()
}

func (m *Metadata) call(ctx context.Context, path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("language", m.lang)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tmdbAPI+path, nil)
	// TMDB has two credential kinds: a v4 read token (a JWT) goes in the
	// Authorization header, a v3 key in the query.
	if strings.HasPrefix(m.key, "eyJ") {
		req.Header.Set("Authorization", "Bearer "+m.key)
	} else {
		q.Set("api_key", m.key)
	}
	req.URL.RawQuery = q.Encode()
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type tmdbResult struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalName  string  `json:"original_name"`
	OriginalTitle string  `json:"original_title"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	Popularity    float64 `json:"popularity"`
}

func (m *Metadata) fetchTitle(ctx context.Context, r metaReq) (*Meta, error) {
	kind := "movie"
	if r.tv {
		kind = "tv"
	}
	id := r.tmdbID
	if id == 0 {
		var err error
		if id, err = m.search(ctx, kind, r.name, r.year); err != nil || id == 0 {
			return nil, err
		}
	}
	var d struct {
		Title        string  `json:"title"`
		Name         string  `json:"name"`
		Overview     string  `json:"overview"`
		VoteAverage  float64 `json:"vote_average"`
		Runtime      int     `json:"runtime"`
		EpisodeRun   []int   `json:"episode_run_time"`
		PosterPath   string  `json:"poster_path"`
		BackdropPath string  `json:"backdrop_path"`
		ReleaseDate  string  `json:"release_date"`
		FirstAirDate string  `json:"first_air_date"`
		Genres       []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Seasons []struct {
			SeasonNumber int `json:"season_number"`
			EpisodeCount int `json:"episode_count"`
		} `json:"seasons"`
	}
	if err := m.call(ctx, fmt.Sprintf("/%s/%d", kind, id), nil, &d); err != nil {
		return nil, err
	}
	meta := &Meta{TMDBID: id, Title: d.Title, Overview: d.Overview, Rating: d.VoteAverage, Runtime: d.Runtime,
		Poster: img("w500", d.PosterPath), Backdrop: img("w1280", d.BackdropPath)}
	if meta.Title == "" {
		meta.Title = d.Name
	}
	date := d.ReleaseDate
	if date == "" {
		date = d.FirstAirDate
	}
	if len(date) >= 4 {
		meta.Year, _ = strconv.Atoi(date[:4])
	}
	if meta.Runtime == 0 && len(d.EpisodeRun) > 0 {
		meta.Runtime = d.EpisodeRun[0]
	}
	for _, g := range d.Genres {
		meta.Genres = append(meta.Genres, g.Name)
	}
	if r.tv {
		meta.SeasonCounts = map[int]int{}
		for _, s := range d.Seasons {
			meta.SeasonCounts[s.SeasonNumber] = s.EpisodeCount
		}
	}
	return meta, nil
}

// search finds the best TMDB match for a parsed name: an exact name
// match (ignoring case and punctuation) with the right year wins, then a
// name match, then TMDB's own top hit.
func (m *Metadata) search(ctx context.Context, kind, name string, year int) (int, error) {
	try := func(withYear bool) (int, error) {
		q := url.Values{"query": {name}, "include_adult": {"false"}}
		if withYear && year != 0 {
			if kind == "tv" {
				q.Set("first_air_date_year", strconv.Itoa(year))
			} else {
				q.Set("year", strconv.Itoa(year))
			}
		}
		var res struct {
			Results []tmdbResult `json:"results"`
		}
		if err := m.call(ctx, "/search/"+kind, q, &res); err != nil {
			return 0, err
		}
		if len(res.Results) == 0 {
			return 0, nil
		}
		want := normalizeKey(name)
		best, bestScore := res.Results[0].ID, -1
		for i, c := range res.Results {
			score := 0
			for _, n := range []string{c.Title, c.Name, c.OriginalTitle, c.OriginalName} {
				if n != "" && normalizeKey(n) == want {
					score = 4
				}
			}
			d := c.ReleaseDate + c.FirstAirDate
			if year != 0 && len(d) >= 4 && d[:4] == strconv.Itoa(year) {
				score += 2
			}
			if i == 0 {
				score++
			}
			if score > bestScore {
				best, bestScore = c.ID, score
			}
		}
		return best, nil
	}
	id, err := try(true)
	if err != nil || id != 0 || year == 0 {
		return id, err
	}
	return try(false)
}

func (m *Metadata) fetchSeason(ctx context.Context, id, season int) (*SeasonMeta, error) {
	var d struct {
		PosterPath string `json:"poster_path"`
		Overview   string `json:"overview"`
		Episodes   []struct {
			EpisodeNumber int    `json:"episode_number"`
			Name          string `json:"name"`
			Overview      string `json:"overview"`
			StillPath     string `json:"still_path"`
			Runtime       int    `json:"runtime"`
		} `json:"episodes"`
	}
	if err := m.call(ctx, fmt.Sprintf("/tv/%d/season/%d", id, season), nil, &d); err != nil {
		return nil, err
	}
	sm := &SeasonMeta{Poster: img("w500", d.PosterPath), Overview: d.Overview, Episodes: map[int]*EpMeta{}}
	for _, e := range d.Episodes {
		sm.Episodes[e.EpisodeNumber] = &EpMeta{Title: e.Name, Overview: e.Overview, Still: img("w500", e.StillPath), Runtime: e.Runtime}
	}
	return sm, nil
}

func img(size, p string) string {
	if p == "" {
		return ""
	}
	return tmdbImg + size + p
}

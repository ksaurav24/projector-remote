package anime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"projectorremote/internal/catalog"
)

type episodeInfo struct {
	Title         map[string]string `json:"title"`
	Summary       string            `json:"summary"`
	Overview      string            `json:"overview"`
	Image         string            `json:"image"`
	Length        int               `json:"length"`
	EpisodeNumber int               `json:"episodeNumber"`
}
type cachedEpisodes struct {
	items map[string]episodeInfo
	until time.Time
}
type EpisodeMetadata struct {
	URL   string
	mu    sync.Mutex
	cache map[int]cachedEpisodes
}

func NewEpisodeMetadata(raw string) *EpisodeMetadata {
	return &EpisodeMetadata{URL: raw, cache: map[int]cachedEpisodes{}}
}
func (e *EpisodeMetadata) get(ctx context.Context, id int) map[string]episodeInfo {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	cached, ok := e.cache[id]
	e.mu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return cached.items
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	u, err := url.Parse(e.URL)
	if err != nil {
		return nil
	}
	q := u.Query()
	q.Set("anilist_id", strconv.Itoa(id))
	u.RawQuery = q.Encode()
	var out struct {
		Mappings struct {
			AniListID int `json:"anilist_id"`
		} `json:"mappings"`
		Episodes map[string]episodeInfo `json:"episodes"`
	}
	b, err := request(ctx, defaultHTTP, "GET", u.String(), nil, nil)
	ttl := time.Minute
	var items map[string]episodeInfo
	if err == nil && json.Unmarshal(b, &out) == nil && out.Mappings.AniListID == id {
		items = out.Episodes
		ttl = 30 * time.Minute
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.cache) > 1000 {
		e.cache = map[int]cachedEpisodes{}
	}
	e.cache[id] = cachedEpisodes{items, time.Now().Add(ttl)}
	return items
}
func (s *Service) episodePage(ctx context.Context, m Media, audio string, page int) *catalog.EpisodePage {
	out := episodePage(m, audio, page)
	if len(out.Items) == 0 {
		return out
	}
	info := s.EpisodeMetadata.get(ctx, m.ID)
	for i := range out.Items {
		ep := &out.Items[i]
		v, ok := info[strconv.Itoa(ep.Num)]
		if !ok {
			continue
		}
		for _, lang := range []string{"en", "x-jat", "ja"} {
			if title := plain(v.Title[lang]); title != "" {
				ep.Title = title
				break
			}
		}
		ep.Desc = plain(v.Summary)
		// TVDB may use a different order. Only borrow its artwork/overview when
		// its episode number agrees with the release-local mapping key.
		if v.EpisodeNumber == ep.Num {
			if ep.Desc == "" {
				ep.Desc = plain(v.Overview)
			}
			if validURL(v.Image) {
				ep.Still = v.Image
			}
		}
		if v.Length > 0 {
			ep.Runtime = fmt.Sprintf("%d min", v.Length)
		}
	}
	return out
}

package anime

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"projectorremote/internal/catalog"
)

// PrefixSearch supplements AniList's whole-word index. Kitsu's explicit
// AniList mappings preserve the canonical IDs used by playback and bookmarks.
type PrefixSearch struct {
	URL   string
	mu    sync.Mutex
	cache map[string]cachedSearch
}
type cachedSearch struct {
	items []catalog.SearchItem
	until time.Time
}

func (s *PrefixSearch) Search(ctx context.Context, q string) ([]catalog.SearchItem, error) {
	key := strings.ToLower(strings.TrimSpace(q))
	s.mu.Lock()
	cached, ok := s.cache[key]
	s.mu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return cached.items, nil
	}
	u, err := url.Parse(s.URL)
	if err != nil {
		return nil, err
	}
	params := u.Query()
	params.Set("filter[text]", q)
	params.Set("page[limit]", "20")
	params.Set("include", "mappings")
	u.RawQuery = params.Encode()
	var out struct {
		Data []struct {
			Attributes struct {
				CanonicalTitle string            `json:"canonicalTitle"`
				Titles         map[string]string `json:"titles"`
				NSFW           bool              `json:"nsfw"`
				AgeRating      string            `json:"ageRating"`
				PosterImage    struct {
					Large  string `json:"large"`
					Medium string `json:"medium"`
				} `json:"posterImage"`
			} `json:"attributes"`
			Relationships struct {
				Mappings struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				} `json:"mappings"`
			} `json:"relationships"`
		} `json:"data"`
		Included []struct {
			ID         string `json:"id"`
			Type       string `json:"type"`
			Attributes struct {
				ExternalSite string `json:"externalSite"`
				ExternalID   string `json:"externalId"`
			} `json:"attributes"`
		} `json:"included"`
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := getJSON(ctx, defaultHTTP, u.String(), &out); err != nil {
		return nil, err
	}
	ids := map[string]string{}
	for _, v := range out.Included {
		if v.Type == "mappings" && v.Attributes.ExternalSite == "anilist/anime" {
			if id, err := strconv.Atoi(v.Attributes.ExternalID); err == nil && id > 0 {
				ids[v.ID] = strconv.Itoa(id)
			}
		}
	}
	items := []catalog.SearchItem{}
	seen := map[string]bool{}
	for _, v := range out.Data {
		if v.Attributes.NSFW || v.Attributes.AgeRating == "Rx" {
			continue
		}
		id := ""
		for _, m := range v.Relationships.Mappings.Data {
			if ids[m.ID] != "" {
				id = ids[m.ID]
				break
			}
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := v.Attributes.Titles["en"]
		if name == "" {
			name = v.Attributes.CanonicalTitle
		}
		poster := v.Attributes.PosterImage.Large
		if poster == "" {
			poster = v.Attributes.PosterImage.Medium
		}
		items = append(items, catalog.SearchItem{ID: id, Title: name, Poster: poster})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil || len(s.cache) > 500 {
		s.cache = map[string]cachedSearch{}
	}
	s.cache[key] = cachedSearch{items, time.Now().Add(10 * time.Minute)}
	return items, nil
}

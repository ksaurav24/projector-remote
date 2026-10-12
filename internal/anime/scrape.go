package anime

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"projectorremote/internal/catalog"
)

type Scraper struct {
	Kind, Base string
	Client     *http.Client
	mu         sync.Mutex
	matches    map[int]cachedMatch
	pages      map[string]cachedHTML
}

type cachedHTML struct {
	body  string
	until time.Time
}

type cachedMatch struct {
	slug  string
	until time.Time
}

func (s *Scraper) Name() string { return s.Kind }
func (s *Scraper) fetch(ctx context.Context, path, referer string) (string, error) {
	target := path
	if !validURL(path) {
		target = endpoint(s.Base, path)
	}
	// Series pages contain stable episode routes, not expiring stream URLs.
	cacheable := s.Kind == "animegg" && strings.Contains(path, "/series/")
	if cacheable {
		s.mu.Lock()
		v, ok := s.pages[target]
		s.mu.Unlock()
		if ok && time.Now().Before(v.until) {
			return v.body, nil
		}
	}
	b, e := request(ctx, s.Client, "GET", target, nil, map[string]string{"Referer": referer})
	if cacheable && e == nil && strings.Contains(string(b), "anm_det_pop") {
		s.mu.Lock()
		if s.pages == nil || len(s.pages) > 100 {
			s.pages = map[string]cachedHTML{}
		}
		s.pages[target] = cachedHTML{string(b), time.Now().Add(10 * time.Minute)}
		s.mu.Unlock()
	}
	return string(b), e
}
func attr(tag, name string) string {
	r := regexp.MustCompile(`(?i)(?:^|\s)` + regexp.QuoteMeta(name) + `\s*=\s*["']([^"']*)["']`)
	m := r.FindStringSubmatch(tag)
	if len(m) > 1 {
		return html.UnescapeString(m[1])
	}
	return ""
}
func capture(pattern, body string) string {
	m := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
func absolute(base, raw string) string {
	u, e := url.Parse(html.UnescapeString(strings.ReplaceAll(raw, `\/`, `/`)))
	if e != nil {
		return ""
	}
	b, e := url.Parse(base)
	if e != nil {
		return ""
	}
	return b.ResolveReference(u).String()
}
func (s *Scraper) match(ctx context.Context, m Media) (string, error) {
	s.mu.Lock()
	cached, ok := s.matches[m.ID]
	s.mu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return cached.slug, nil
	}
	path := "/browser?keyword="
	if s.Kind == "animegg" {
		path = "/search/?q="
	}
	if s.Kind == "anizone" {
		path = "/anime?search="
	}
	queries := []string{}
	// Short distinctive search terms avoid providers that mishandle long
	// subtitles/punctuation. Candidate matching still checks the full release.
	short := strings.FieldsFunc(m.name(), func(r rune) bool { return r == ':' || r == '-' })
	if len(short) > 0 && len(strings.TrimSpace(short[0])) >= 4 {
		queries = append(queries, strings.TrimSpace(short[0]))
	}
	queries = append(queries, m.titles()...)
	seenQueries := map[string]bool{}
	allCandidates := []candidate{}
	for i, q := range queries {
		if i >= 4 {
			break
		}
		if q == "" || seenQueries[q] {
			continue
		}
		seenQueries[q] = true
		page, e := s.fetch(ctx, path+url.QueryEscape(q), s.Base+"/")
		if e != nil {
			return "", e
		}
		candidates := []candidate{}
		if s.Kind == "anizone" {
			var items []struct {
				Slug      string            `json:"slug"`
				MainTitle string            `json:"main_title"`
				Titles    map[string]string `json:"title_list"`
				Year      int               `json:"start_year"`
			}
			if e := jsonArgument(page, `items\s*:\s*`, &items); e != nil {
				return "", e
			}
			for _, item := range items {
				if item.Year > 0 && m.StartDate.Year > 0 && item.Year != m.StartDate.Year {
					continue
				}
				names := []string{item.MainTitle}
				for _, t := range item.Titles {
					names = append(names, t)
				}
				candidates = append(candidates, candidate{item.Slug, names})
			}
		} else {
			for _, block := range regexp.MustCompile(`(?is)<a\b[^>]*>.*?</a>`).FindAllString(page, -1) {
				href := attr(block, "href")
				pattern := `/watch/([^/?#]+)`
				titlePattern := `(?is)<h3[^>]*>(.*?)</h3>`
				if s.Kind == "animegg" {
					pattern = `/series/([^/?#]+)`
					titlePattern = `(?is)<strong[^>]*>(.*?)</strong>`
				}
				slug := capture(pattern, href)
				if slug == "" {
					continue
				}
				name := plain(capture(titlePattern, block))
				if name == "" {
					name = plain(capture(`(?is)<h2[^>]*>(.*?)</h2>`, block))
				}
				if name == "" {
					name = attr(block, "title")
				}
				if name == "" {
					name = plain(block)
				}
				candidates = append(candidates, candidate{slug, []string{name}})
			}
		}
		allCandidates = append(allCandidates, candidates...)
	}
	if slug, e := uniqueMatch(m, allCandidates); e == nil {
		s.mu.Lock()
		if s.matches == nil || len(s.matches) > 1000 {
			s.matches = map[int]cachedMatch{}
		}
		s.matches[m.ID] = cachedMatch{slug, time.Now().Add(time.Hour)}
		s.mu.Unlock()
		return slug, nil
	}
	return "", fmt.Errorf("%s: no unambiguous match", s.Kind)
}
func (s *Scraper) Resolve(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	if s.Kind == "anizone" && audio == "dub" {
		return nil, fmt.Errorf("AniZone has no dub")
	}
	slug, e := s.match(ctx, m)
	if e != nil {
		return nil, e
	}
	switch s.Kind {
	case "anineko":
		return s.neko(ctx, slug, ep, audio)
	case "animegg":
		return s.gg(ctx, slug, ep, audio)
	case "anizone":
		return s.zone(ctx, slug, ep)
	}
	return nil, fmt.Errorf("unknown extractor")
}
func hlsFromHTML(page string) string {
	return html.UnescapeString(capture(`(?i)["'](https?://[^"'\s]+\.m3u8[^"'\s]*)["']`, strings.ReplaceAll(page, `\/`, `/`)))
}
func (s *Scraper) neko(ctx context.Context, slug string, ep int, audio string) (*catalog.Stream, error) {
	page, e := s.fetch(ctx, fmt.Sprintf("/watch/%s/ep-%d", url.PathEscape(slug), ep), s.Base+"/watch/"+slug)
	if e != nil {
		return nil, e
	}
	// Partition panels at their opening tags; never use a dub panel for sub.
	panels := regexp.MustCompile(`(?is)<div\b[^>]*class=["'][^"']*nv-server-grid[^"']*["'][^>]*>`).FindAllStringIndex(page, -1)
	for i, bounds := range panels {
		tag := page[bounds[0]:bounds[1]]
		kind := "sub"
		if strings.Contains(strings.ToLower(attr(tag, "data-id")), "dub") {
			kind = "dub"
		}
		if kind != audio {
			continue
		}
		end := len(page)
		if i+1 < len(panels) {
			end = panels[i+1][0]
		}
		for _, v := range regexp.MustCompile(`(?i)data-video=["']([^"']+)["']`).FindAllStringSubmatch(page[bounds[1]:end], -1) {
			embed := absolute(s.Base, v[1])
			if !validURL(embed) {
				continue
			}
			body, err := s.fetch(ctx, embed, s.Base+"/")
			if err != nil {
				continue
			}
			if hls := hlsFromHTML(body); validURL(hls) {
				u, _ := url.Parse(embed)
				return &catalog.Stream{URL: hls, Referer: u.Scheme + "://" + u.Host + "/", MimeType: "application/x-mpegURL"}, nil
			}
		}
	}
	return nil, fmt.Errorf("AniNeko: no playable %s episode", audio)
}
func (s *Scraper) gg(ctx context.Context, slug string, ep int, audio string) (*catalog.Stream, error) {
	page, e := s.fetch(ctx, "/series/"+url.PathEscape(slug), s.Base)
	if e != nil {
		return nil, e
	}
	epPath := ""
	for _, block := range regexp.MustCompile(`(?is)<li\b[^>]*>.*?</li>`).FindAllString(page, -1) {
		if !strings.Contains(block, "anm_det_pop") {
			continue
		}
		strong := plain(capture(`(?is)<strong[^>]*>(.*?)</strong>`, block))
		number, _ := strconv.Atoi(capture(`(?:^|\s)(\d+)\s*$`, strong))
		if number != ep {
			continue
		}
		if !strings.Contains(block, "btn-"+map[string]string{"sub": "subbed", "dub": "dubbed"}[audio]) {
			continue
		}
		tag := capture(`(?is)(<a\b[^>]*class=["'][^"']*anm_det_pop[^"']*["'][^>]*>)`, block)
		epPath = attr(tag, "href")
		break
	}
	if epPath == "" {
		return nil, fmt.Errorf("AnimeGG: episode unavailable")
	}
	page, e = s.fetch(ctx, absolute(s.Base, epPath), s.Base)
	if e != nil {
		return nil, e
	}
	for _, tag := range regexp.MustCompile(`(?is)<a\b[^>]*data-toggle=["']tab["'][^>]*>`).FindAllString(page, -1) {
		version := "sub"
		if strings.HasPrefix(attr(tag, "data-version"), "dub") {
			version = "dub"
		}
		if version != audio {
			continue
		}
		id := attr(tag, "data-id")
		if id == "" {
			continue
		}
		body, err := s.fetch(ctx, "/embed/"+url.PathEscape(id), s.Base)
		if err != nil {
			continue
		}
		raw := capture(`(?s)var\s+videoSources\s*=\s*(\[.*?\]);`, body)
		raw = regexp.MustCompile(`([{,]\s*)([A-Za-z_][A-Za-z0-9_]*)\s*:`).ReplaceAllString(raw, `${1}"${2}":`)
		raw = regexp.MustCompile(`:\s*'([^']*)'`).ReplaceAllString(raw, `: "${1}"`)
		var sources []struct {
			File string `json:"file"`
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(raw), &sources) != nil {
			continue
		}
		for _, source := range sources {
			if source.File == "" {
				continue
			}
			target := absolute(s.Base, source.File)
			if validURL(target) {
				mime := "video/mp4"
				if strings.Contains(target, ".m3u8") {
					mime = "application/x-mpegURL"
				}
				return &catalog.Stream{URL: target, Referer: s.Base + "/", MimeType: mime}, nil
			}
		}
	}
	return nil, fmt.Errorf("AnimeGG: no playable source")
}

// Laravel emits JSON inside a JS string, escaping quotes as unicode. Decode
// only string escapes, never execute the scraped script.
func jsonArgument(page, prefix string, out any) error {
	raw := capture(`(?is)`+prefix+`JSON\.parse\('((?:[^'\\]|\\.)*)'\)`, html.UnescapeString(page))
	if raw == "" {
		return fmt.Errorf("missing JSON payload")
	}
	var decoded string
	raw = strings.ReplaceAll(raw, `\'`, `'`)
	if err := json.Unmarshal([]byte(`"`+strings.ReplaceAll(raw, `"`, `\"`)+`"`), &decoded); err != nil {
		return err
	}
	return json.Unmarshal([]byte(decoded), out)
}
func (s *Scraper) zone(ctx context.Context, slug string, ep int) (*catalog.Stream, error) {
	path := fmt.Sprintf("/anime/%s/%d", url.PathEscape(slug), ep)
	page, e := s.fetch(ctx, path, s.Base+"/anime/"+slug)
	if e != nil {
		return nil, e
	}
	var player struct {
		Chapter   string `json:"chapter"`
		Src       string `json:"src"`
		Subtitles []struct {
			File     string `json:"file"`
			Title    string `json:"title"`
			Language string `json:"language"`
			Format   string `json:"format"`
			Default  bool   `json:"default"`
		} `json:"subtitles"`
	}
	if e = jsonArgument(page, `vidstackPlayer\s*\(\s*`, &player); e != nil {
		return nil, e
	}
	if player.Src == "" {
		return nil, fmt.Errorf("AniZone: missing HLS")
	}
	stream := &catalog.Stream{URL: absolute(s.Base, player.Src), Referer: s.Base + path, MimeType: "application/x-mpegURL"}
	if chapter := absolute(s.Base, player.Chapter); player.Chapter != "" && validURL(chapter) {
		stream.ChapterURL = chapter
	}
	for _, sub := range player.Subtitles {
		if sub.File != "" {
			subURL := absolute(s.Base, sub.File)
			if validURL(subURL) {
				stream.Subtitles = append(stream.Subtitles, catalog.Subtitle{URL: subURL, Label: sub.Title, Language: sub.Language, MimeType: subtitleMime(sub.Format), Default: sub.Default})
			}
		}
	}
	return stream, nil
}
func subtitleMime(format string) string {
	switch strings.ToLower(format) {
	case "srt":
		return "application/x-subrip"
	case "ass", "ssa":
		return "text/x-ssa"
	default:
		return "text/vtt"
	}
}

package anime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"projectorremote/internal/catalog"
)

// ReferenceProvider ports direct provider flows from Anivexa that return
// provider APIs or pages without requiring an embed player in the client.
type ReferenceProvider struct {
	Kind, Base, Secondary string
	Client                *http.Client
	mu                    sync.Mutex
	session               *animeOnsenSession
}

type animeOnsenSession struct {
	token, apiOrigin, searchOrigin, searchToken string
}

func (p *ReferenceProvider) Name() string { return p.Kind }

func (p *ReferenceProvider) Resolve(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	switch p.Kind {
	case "anibd":
		return p.resolveAniBD(ctx, m, ep, audio)
	case "animedunya":
		return p.resolveAnimeDunya(ctx, m, ep, audio)
	case "senshi":
		return p.resolveSenshi(ctx, m, ep, audio)
	case "kaa":
		return p.resolveKAA(ctx, m, ep, audio)
	case "anidbapp":
		return p.resolveAniDBApp(ctx, m, ep, audio)
	case "animenosub":
		return p.resolveAnimeNoSub(ctx, m, ep, audio)
	case "animeonsen":
		return p.resolveAnimeOnsen(ctx, m, ep, audio)
	case "anikoto":
		return p.resolveAniKoto(ctx, m, ep, audio)
	case "aniwaves":
		return p.resolveAniWaves(ctx, m, ep, audio)
	default:
		return nil, fmt.Errorf("unsupported Anivexa provider %q", p.Kind)
	}
}

func (p *ReferenceProvider) resolveAniDBApp(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	titles := m.titles()
	queries := map[string]bool{}
	candidates := map[string]bool{}
	for _, title := range titles {
		for _, query := range []string{title, strings.Join(strings.Fields(title)[:min(4, len(strings.Fields(title)))], " ")} {
			if query == "" || queries[normalized(query)] {
				continue
			}
			queries[normalized(query)] = true
			u := endpoint(p.Base, "/search/suggestions") + "?q=" + url.QueryEscape(query)
			page, err := request(ctx, p.Client, http.MethodGet, u, nil, map[string]string{"Referer": p.Base + "/home"})
			if err != nil {
				continue
			}
			for _, block := range regexp.MustCompile(`(?is)<a\b[^>]*data-search-item[^>]*>.*?</a>`).FindAllString(string(page), -1) {
				href := attr(block, "href")
				if parsed, err := url.Parse(href); err == nil && parsed.IsAbs() {
					href = parsed.Path
				}
				if slug := capture(`^/anime/([^/?#]+)`, href); slug != "" {
					candidates[slug] = true
				}
			}
		}
	}
	selected := ""
	for slug := range candidates {
		page, err := request(ctx, p.Client, http.MethodGet, endpoint(p.Base, "/anime/"+url.PathEscape(slug)), nil, map[string]string{"Referer": p.Base + "/home"})
		if err != nil {
			continue
		}
		html := string(page)
		anilistID := capture(`(?i)https?://(?:www\.)?anilist\.co/anime/(\d+)`, html)
		malID := capture(`(?i)https?://(?:www\.)?myanimelist\.net/anime/(\d+)`, html)
		if anilistID == strconv.Itoa(m.ID) || (anilistID == "" && m.IDMal > 0 && malID == strconv.Itoa(m.IDMal)) {
			if selected != "" && selected != slug {
				return nil, fmt.Errorf("AniDB.app: ambiguous external ID match")
			}
			selected = slug
		}
	}
	if selected == "" {
		return nil, fmt.Errorf("AniDB.app: no AniList/MAL identity match")
	}
	siteID, err := strconv.Atoi(capture(`-(\d+)$`, selected))
	if err != nil || siteID < 1 {
		return nil, fmt.Errorf("AniDB.app: invalid site ID")
	}
	var episodes struct {
		Episodes map[string]struct {
			ID     int  `json:"id"`
			Filler bool `json:"filler"`
		} `json:"episodes"`
	}
	if err := p.json(ctx, fmt.Sprintf("/api/frontend/anime/%d/episodes", siteID), nil, &episodes); err != nil {
		return nil, err
	}
	expected := m.aired()
	offset, sourceEpisode := episodeOffset(episodes.Episodes, expected, ep)
	if sourceEpisode < 1 {
		return nil, fmt.Errorf("AniDB.app: episode %d unavailable", ep)
	}
	var languages struct {
		Languages []struct {
			Code     string `json:"code"`
			Name     string `json:"name"`
			EmbedURL string `json:"embed_url"`
		} `json:"languages"`
	}
	selectedEpisode := episodes.Episodes[strconv.Itoa(sourceEpisode)]
	if selectedEpisode.ID == 0 {
		return nil, fmt.Errorf("AniDB.app: episode metadata missing")
	}
	if err := p.json(ctx, fmt.Sprintf("/api/frontend/episode/%d/languages", selectedEpisode.ID), nil, &languages); err != nil {
		return nil, err
	}
	want := map[string]bool{"sub": true, "jpn": true, "ja": true, "japanese": true}
	if audio == "dub" {
		want = map[string]bool{"dub": true, "eng": true, "en": true, "english": true}
	}
	for _, language := range languages.Languages {
		code := strings.ToLower(language.Code)
		if !want[code] && !want[strings.ToLower(language.Name)] || !validURL(language.EmbedURL) {
			continue
		}
		page, err := request(ctx, p.Client, http.MethodGet, language.EmbedURL, nil, map[string]string{"Referer": p.Base + "/"})
		if err != nil {
			continue
		}
		hls := hlsFromHTML(string(page))
		if validURL(hls) {
			u, _ := url.Parse(language.EmbedURL)
			return &catalog.Stream{URL: hls, MimeType: "application/x-mpegURL", Referer: u.Scheme + "://" + u.Host + "/"}, nil
		}
	}
	_ = offset
	return nil, fmt.Errorf("AniDB.app: no direct %s stream for episode %d", audio, ep)
}

func episodeOffset[T any](items map[string]T, expected, requested int) (int, int) {
	if _, ok := items[strconv.Itoa(requested)]; ok {
		return 0, requested
	}
	if expected < 1 {
		return 0, 0
	}
	minNumber, maxNumber := 0, 0
	for raw := range items {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			continue
		}
		if minNumber == 0 || n < minNumber {
			minNumber = n
		}
		if n > maxNumber {
			maxNumber = n
		}
	}
	if minNumber > 1 && maxNumber-minNumber+1 >= expected {
		sourceNumber := requested + minNumber - 1
		if _, ok := items[strconv.Itoa(sourceNumber)]; ok {
			return minNumber - 1, sourceNumber
		}
	}
	return 0, 0
}

func (p *ReferenceProvider) resolveAnimeNoSub(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	queries := m.titles()
	results := map[string]bool{}
	for _, query := range queries[:min(5, len(queries))] {
		form := url.Values{"action": {"ts_ac_do_search"}, "ts_ac_query": {query}}
		body := []byte(form.Encode())
		b, err := requestForm(ctx, p.Client, endpoint(p.Base, "/wp-admin/admin-ajax.php"), body, p.Base+"/")
		if err != nil {
			continue
		}
		var response struct {
			Anime []struct {
				All []struct {
					Link  string `json:"post_link"`
					Title string `json:"post_title"`
				} `json:"all"`
			} `json:"anime"`
		}
		if json.Unmarshal(b, &response) != nil {
			continue
		}
		for _, group := range response.Anime {
			for _, item := range group.All {
				slug := capture(`/anime/([^/?#]+)/?$`, item.Link)
				if slug != "" && normalized(item.Title) != "" && matches(m, item.Title) {
					results[slug] = true
				}
			}
		}
	}
	if len(results) != 1 {
		return nil, fmt.Errorf("AnimeNoSub: no unique exact title match")
	}
	var slug string
	for key := range results {
		slug = key
	}
	page, err := request(ctx, p.Client, http.MethodGet, endpoint(p.Base, "/anime/"+url.PathEscape(slug)+"/"), nil, map[string]string{"Referer": p.Base + "/"})
	if err != nil {
		return nil, err
	}
	pageText := string(page)
	for _, item := range regexp.MustCompile(`(?is)<li\b[^>]*data-index="\d+"[^>]*>.*?</li>`).FindAllString(pageText, -1) {
		label := plain(capture(`(?is)<div\s+class=["']epl-num["'][^>]*>([^<]+)`, item))
		number, _ := strconv.Atoi(label)
		if number != ep || (audio == "dub") != strings.Contains(strings.ToLower(capture(`(?is)<a\s+href=["']([^"']+)`, item)), "-dub") {
			continue
		}
		urlPath := capture(`(?is)<a\s+href=["'](https?://[^"']+)`, item)
		if urlPath == "" {
			continue
		}
		episodePage, err := request(ctx, p.Client, http.MethodGet, urlPath, nil, map[string]string{"Referer": p.Base + "/"})
		if err != nil {
			continue
		}
		for _, encoded := range regexp.MustCompile(`(?is)<option\s+value=["']([A-Za-z0-9+/=]+)["'][^>]*>`).FindAllStringSubmatch(string(episodePage), -1) {
			decoded, err := base64Decode(encoded[1])
			if err != nil {
				continue
			}
			embed := capture(`(?is)src=["']([^"']+)`, decoded)
			if !validURL(embed) {
				continue
			}
			streamPage, err := request(ctx, p.Client, http.MethodGet, embed, nil, map[string]string{"Referer": p.Base + "/"})
			if err != nil {
				continue
			}
			hls := hlsFromHTML(string(streamPage))
			if validURL(hls) {
				u, _ := url.Parse(embed)
				return &catalog.Stream{URL: hls, MimeType: "application/x-mpegURL", Referer: u.Scheme + "://" + u.Host + "/"}, nil
			}
		}
	}
	return nil, fmt.Errorf("AnimeNoSub: direct %s stream not found for episode %d", audio, ep)
}

func (p *ReferenceProvider) resolveAnimeOnsen(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	if audio != "sub" {
		return nil, fmt.Errorf("AnimeOnsen only provides subtitled audio")
	}
	session, err := p.onsenSession(ctx, false)
	if err != nil {
		return nil, err
	}
	titles := m.titles()
	queries := map[string]bool{}
	candidates := map[string]map[string]any{}
	for _, title := range titles[:min(8, len(titles))] {
		for _, query := range []string{title, strings.Join(strings.Fields(normalizeQuery(title)), " ")} {
			key := normalized(query)
			if key == "" || queries[key] {
				continue
			}
			queries[key] = true
			var result struct {
				Results []struct {
					Hits []map[string]any `json:"hits"`
				} `json:"results"`
			}
			if err := p.onsenSearch(ctx, session, query, &result); err != nil {
				continue
			}
			for _, group := range result.Results {
				for _, hit := range group.Hits {
					id := fmt.Sprint(hit["content_id"])
					if id != "" && id != "<nil>" {
						candidates[id] = hit
					}
				}
			}
		}
	}
	type scored struct {
		id      string
		title   string
		malID   int
		count   int
		isMovie bool
		score   float64
	}
	valid := []scored{}
	for id, hit := range candidates {
		var video struct {
			Metadata struct {
				MALID         int  `json:"mal_id"`
				TotalEpisodes int  `json:"total_episodes"`
				IsMovie       bool `json:"is_movie"`
			} `json:"metadata"`
		}
		if err := p.onsenAPI(ctx, session, "/v4/content/"+url.PathEscape(id)+"/video/1", &video); err != nil {
			continue
		}
		candidateTitle := anyString(hit["content_title_en"])
		if candidateTitle == "" {
			candidateTitle = anyString(hit["content_title"])
		}
		if (m.Format == "MOVIE") != video.Metadata.IsMovie {
			continue
		}
		score := diceScore(titles, candidateTitle)
		if video.Metadata.TotalEpisodes > 0 && m.aired() > 1 {
			ratio := float64(min(video.Metadata.TotalEpisodes, m.aired())) / float64(max(video.Metadata.TotalEpisodes, m.aired()))
			score = score*0.8 + ratio*0.2
		}
		if m.IDMal > 0 && video.Metadata.MALID != m.IDMal {
			continue
		}
		valid = append(valid, scored{id, candidateTitle, video.Metadata.MALID, video.Metadata.TotalEpisodes, video.Metadata.IsMovie, score})
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("AnimeOnsen: no identity-verified title match")
	}
	// A MAL match is definitive. Without it, require one clear title winner.
	sort.Slice(valid, func(i, j int) bool { return valid[i].score > valid[j].score })
	if m.IDMal == 0 && (valid[0].score < 0.82 || len(valid) > 1 && valid[0].score-valid[1].score < 0.08) {
		return nil, fmt.Errorf("AnimeOnsen: ambiguous title match")
	}
	var episodes map[string]struct {
		TitleEN string `json:"contentTitle_episode_en"`
		TitleJP string `json:"contentTitle_episode_jp"`
	}
	if err := p.onsenAPI(ctx, session, "/v4/content/"+url.PathEscape(valid[0].id)+"/episodes", &episodes); err != nil {
		return nil, err
	}
	sourceEpisode := strconv.Itoa(ep)
	if _, ok := episodes[sourceEpisode]; !ok {
		return nil, fmt.Errorf("AnimeOnsen episode %d unavailable", ep)
	}
	var video struct {
		URI struct {
			Stream    string            `json:"stream"`
			Subtitles map[string]string `json:"subtitles"`
		} `json:"uri"`
		Metadata struct {
			Subtitles map[string]string `json:"subtitles"`
		} `json:"metadata"`
	}
	if err := p.onsenAPI(ctx, session, "/v4/content/"+url.PathEscape(valid[0].id)+"/video/"+sourceEpisode, &video); err != nil {
		return nil, err
	}
	if !isHLS(video.URI.Stream) && !isMP4(video.URI.Stream) {
		return nil, fmt.Errorf("AnimeOnsen returned a non-HLS/MP4 stream")
	}
	stream := &catalog.Stream{URL: video.URI.Stream, MimeType: streamMime("", video.URI.Stream), Referer: p.Base + "/", Headers: map[string]string{"Authorization": "Bearer " + session.token}}
	for code, raw := range video.URI.Subtitles {
		if !validURL(raw) {
			continue
		}
		label := video.Metadata.Subtitles[code]
		if label == "" {
			label = code
		}
		stream.Subtitles = append(stream.Subtitles, catalog.Subtitle{URL: raw, Label: label, Language: code, MimeType: "text/vtt", Default: code == "en-US"})
	}
	return stream, nil
}

func normalizeQuery(raw string) string {
	return regexp.MustCompile(`[\pL\pN]+`).ReplaceAllString(strings.ToLower(raw), " ")
}

func anyString(value any) string {
	text, _ := value.(string)
	return text
}

func (p *ReferenceProvider) onsenSession(ctx context.Context, force bool) (*animeOnsenSession, error) {
	p.mu.Lock()
	if force {
		p.session = nil
	}
	if p.session != nil {
		current := p.session
		p.mu.Unlock()
		return current, nil
	}
	p.mu.Unlock()
	var cookie string
	client := p.Client
	if client == nil {
		client = defaultHTTP
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.Base, "/")+"/", nil)
	req.Header.Set("User-Agent", userAgent)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if readErr != nil {
		res.Body.Close()
		return nil, readErr
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		return nil, upstreamStatusError(res.StatusCode)
	}
	for _, value := range res.Cookies() {
		if value.Name == "ao.session" {
			cookie = value.Value
			break
		}
	}
	res.Body.Close()
	if cookie == "" {
		return nil, fmt.Errorf("AnimeOnsen session cookie missing")
	}
	decodedCookie, err := url.QueryUnescape(cookie)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(decodedCookie)
	if err != nil {
		return nil, fmt.Errorf("AnimeOnsen session token invalid")
	}
	for i := range decoded {
		decoded[i]++
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("AnimeOnsen session token empty")
	}
	html := string(body)
	apiOrigin := metaContent(html, "ao-api-origin")
	searchOrigin := metaContent(html, "ao-search-origin")
	searchToken := metaContent(html, "ao-search-token")
	if apiOrigin == "" || searchOrigin == "" || searchToken == "" {
		return nil, fmt.Errorf("AnimeOnsen session metadata missing")
	}
	apiURL, err := url.Parse(apiOrigin)
	if err != nil || (apiURL.Scheme != "https" && apiURL.Scheme != "http") || apiURL.Host == "" {
		return nil, fmt.Errorf("AnimeOnsen API origin invalid")
	}
	searchURL, err := url.Parse(searchOrigin)
	if err != nil || (searchURL.Scheme != "https" && searchURL.Scheme != "http") || searchURL.Host == "" {
		return nil, fmt.Errorf("AnimeOnsen search origin invalid")
	}
	current := &animeOnsenSession{token: string(decoded), apiOrigin: apiURL.Scheme + "://" + apiURL.Host, searchOrigin: searchURL.Scheme + "://" + searchURL.Host, searchToken: searchToken}
	p.mu.Lock()
	p.session = current
	p.mu.Unlock()
	return current, nil
}

func metaContent(page, name string) string {
	for _, tag := range regexp.MustCompile(`(?is)<meta\b[^>]*>`).FindAllString(page, -1) {
		if strings.EqualFold(attr(tag, "name"), name) {
			return attr(tag, "content")
		}
	}
	return ""
}

func (p *ReferenceProvider) onsenAPI(ctx context.Context, current *animeOnsenSession, path string, out any) error {
	return p.onsenRequest(ctx, current.apiOrigin+path, map[string]string{"Authorization": "Bearer " + current.token, "Origin": p.Base, "Referer": strings.TrimRight(p.Base, "/") + "/"}, nil, out)
}

func (p *ReferenceProvider) onsenSearch(ctx context.Context, current *animeOnsenSession, query string, out any) error {
	body, _ := json.Marshal(map[string]any{"queries": []any{map[string]any{"indexUid": "content", "q": query, "limit": 20}}})
	headers := map[string]string{"Authorization": "Bearer " + current.searchToken, "Origin": p.Base, "Referer": strings.TrimRight(p.Base, "/") + "/"}
	headers["Content-Type"] = "application/json"
	return p.onsenRequest(ctx, current.searchOrigin+"/multi-search", headers, body, out)
}

func (p *ReferenceProvider) onsenRequest(ctx context.Context, target string, headers map[string]string, body []byte, out any) error {
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	client := p.Client
	if client == nil {
		client = defaultHTTP
	}
	for attempt := 0; attempt < 2; attempt++ {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
		if err != nil {
			return err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json, text/plain, */*")
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if (res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden) && attempt == 0 {
			res.Body.Close()
			current, err := p.onsenSession(ctx, true)
			if err != nil {
				return err
			}
			headers["Authorization"] = "Bearer " + current.token
			if strings.Contains(target, "/multi-search") {
				headers["Authorization"] = "Bearer " + current.searchToken
			}
			continue
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return upstreamStatusError(res.StatusCode)
		}
		return json.NewDecoder(io.LimitReader(res.Body, (8<<20)+1)).Decode(out)
	}
	return fmt.Errorf("AnimeOnsen authorization retry failed")
}

func (p *ReferenceProvider) json(ctx context.Context, path string, query url.Values, out any) error {
	u, err := url.Parse(endpoint(p.Base, path))
	if err != nil {
		return err
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return getJSON(ctx, p.Client, u.String(), out)
}

func (p *ReferenceProvider) html(ctx context.Context, path, referer string) (string, error) {
	var headers map[string]string
	if referer != "" {
		headers = map[string]string{"Referer": referer}
	}
	b, err := request(ctx, p.Client, http.MethodGet, endpoint(p.Base, path), nil, headers)
	return string(b), err
}

func (p *ReferenceProvider) resolveAniBD(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	var groups []struct {
		Name string `json:"server_name"`
		Data []struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
			Link string `json:"link"`
		} `json:"server_data"`
	}
	if err := p.json(ctx, "/api2.php", url.Values{"epid": {strconv.Itoa(m.ID)}}, &groups); err != nil {
		return nil, err
	}
	for _, group := range groups {
		if audioFromName(group.Name) != audio {
			continue
		}
		for _, item := range group.Data {
			n := item.Name
			if n == "" {
				n = item.Slug
			}
			if n != strconv.Itoa(ep) || item.Link == "" {
				continue
			}
			var players []struct {
				Link   string `json:"link"`
				Server string `json:"server"`
			}
			if err := p.json(ctx, "/apilink.php", url.Values{"data": {item.Link}}, &players); err != nil {
				continue
			}
			for _, player := range players {
				if !validURL(player.Link) {
					continue
				}
				playerURL, _ := url.Parse(player.Link)
				ref := playerURL.Scheme + "://" + playerURL.Host + "/"
				page, err := request(ctx, p.Client, http.MethodGet, player.Link, nil, map[string]string{"Referer": ref})
				if err != nil {
					continue
				}
				hls := absolute(ref, capture(`(?i)videoUrl\s*:\s*["']([^"']+)["']`, string(page)))
				if isHLS(hls) {
					return &catalog.Stream{URL: hls, Referer: ref, MimeType: "application/x-mpegURL"}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("AniBD: no direct %s stream for episode %d", audio, ep)
}

func audioFromName(name string) string {
	if strings.Contains(strings.ToLower(name), "dub") {
		return "dub"
	}
	return "sub"
}

func (p *ReferenceProvider) resolveAnimeDunya(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	if audio == "dub" {
		return nil, fmt.Errorf("AnimeDunya only provides subtitled audio")
	}
	if m.IDMal < 1 {
		return nil, fmt.Errorf("AnimeDunya: AniList title has no MAL mapping")
	}
	page, err := p.html(ctx, fmt.Sprintf("/en/play/%d/%d", m.IDMal, ep), p.Base+"/")
	if err != nil {
		return nil, err
	}
	stream := parseEmbeddedObject(page, `\\?"stream\\?"\s*:\s*`)
	if len(stream) == 0 {
		return nil, fmt.Errorf("AnimeDunya: stream data missing")
	}
	var data struct {
		Source    string `json:"source"`
		Subtitles []struct {
			Src      string `json:"src"`
			URL      string `json:"url"`
			Label    string `json:"label"`
			Language string `json:"srclang"`
			Default  bool   `json:"default"`
		} `json:"subtitles"`
	}
	if json.Unmarshal(stream, &data) != nil || data.Source == "" {
		// Some responses escape the JSON one additional time in their HTML.
		data.Source = capture(`"source"\s*:\s*"([^"\\]+)`, page)
	}
	if !isHLS(data.Source) && !isMP4(data.Source) {
		return nil, fmt.Errorf("AnimeDunya: no direct stream")
	}
	out := &catalog.Stream{URL: data.Source, Referer: p.Base + "/", MimeType: streamMime("", data.Source)}
	for _, sub := range data.Subtitles {
		u := sub.Src
		if u == "" {
			u = sub.URL
		}
		if validURL(u) {
			out.Subtitles = append(out.Subtitles, catalog.Subtitle{URL: u, Label: sub.Label, Language: sub.Language, MimeType: subtitleMimeFromURL(u), Default: sub.Default})
		}
	}
	return out, nil
}

func (p *ReferenceProvider) resolveSenshi(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	if m.IDMal < 1 {
		return nil, fmt.Errorf("Senshi: AniList title has no MAL mapping")
	}
	var embeds []struct {
		Status         string `json:"status"`
		RemoteSourceID string `json:"remote_source_id"`
	}
	if err := p.json(ctx, fmt.Sprintf("/episode-embeds/%d/%d", m.IDMal, ep), nil, &embeds); err != nil {
		return nil, err
	}
	for _, embed := range embeds {
		if audioFromName(embed.Status) != audio || embed.RemoteSourceID == "" {
			continue
		}
		var sources []struct {
			Source struct {
				Src   string `json:"src"`
				Audio string `json:"audio"`
			} `json:"source"`
			Tracks []struct {
				URL     string `json:"url"`
				VTTURL  string `json:"vtt_url"`
				Label   string `json:"label"`
				Default bool   `json:"default"`
			} `json:"tracks"`
		}
		sourceBase := p.Secondary
		if sourceBase == "" {
			sourceBase = "https://s.vidcloud.se"
		}
		if err := getJSON(ctx, p.Client, endpoint(sourceBase, "/_v1/sources")+"?id="+url.QueryEscape(embed.RemoteSourceID), &sources); err != nil {
			continue
		}
		for _, source := range sources {
			if !validURL(source.Source.Src) || (source.Source.Audio != "" && source.Source.Audio != "both" && source.Source.Audio != audio) {
				continue
			}
			out := &catalog.Stream{URL: source.Source.Src, Referer: p.Base + "/", MimeType: streamMime("", source.Source.Src)}
			if out.MimeType == "" {
				continue
			}
			for _, track := range source.Tracks {
				subURL := track.VTTURL
				if subURL == "" {
					subURL = track.URL
				}
				if validURL(subURL) && !strings.EqualFold(track.Label, "chapter") {
					out.Subtitles = append(out.Subtitles, catalog.Subtitle{URL: subURL, Label: track.Label, Language: languageCode(track.Label), MimeType: "text/vtt", Default: track.Default})
				}
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("Senshi: no direct %s stream for episode %d", audio, ep)
}

func (p *ReferenceProvider) resolveKAA(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	titles := m.titles()
	queries := map[string]bool{}
	var best struct {
		Slug    string   `json:"slug"`
		Title   string   `json:"title"`
		TitleEn string   `json:"title_en"`
		Type    string   `json:"type"`
		Year    int      `json:"year"`
		Locales []string `json:"locales"`
	}
	bestScore, secondScore := 0.0, 0.0
	seen := map[string]bool{}
	for _, raw := range titles[:min(4, len(titles))] {
		q := strings.TrimSpace(regexp.MustCompile(`[^\pL\pN\s]+`).ReplaceAllString(raw, " "))
		words := strings.Fields(q)
		qs := []string{q}
		if len(words) > 3 {
			qs = []string{strings.Join(words[:2], " "), strings.Join(words[:3], " ")}
		}
		for _, query := range qs {
			if len([]rune(query)) < 3 || queries[normalized(query)] {
				continue
			}
			queries[normalized(query)] = true
			var result struct {
				Result []struct {
					Slug    string   `json:"slug"`
					Title   string   `json:"title"`
					TitleEn string   `json:"title_en"`
					Type    string   `json:"type"`
					Year    int      `json:"year"`
					Locales []string `json:"locales"`
				} `json:"result"`
			}
			b, _ := json.Marshal(map[string]any{"page": 1, "query": query})
			if err := requestJSON(ctx, p.Client, http.MethodPost, endpoint(p.Base, "/api/fsearch"), b, &result); err != nil {
				continue
			}
			for _, candidate := range result.Result {
				if candidate.Slug == "" || seen[candidate.Slug] {
					continue
				}
				seen[candidate.Slug] = true
				title := candidate.TitleEn
				if title == "" {
					title = candidate.Title
				}
				score := diceScore(titles, title)
				if m.StartDate.Year > 0 && candidate.Year > 0 {
					diff := abs(m.StartDate.Year, candidate.Year)
					switch diff {
					case 0:
						score = math.Min(1, score*1.2)
					case 1:
						score *= 0.8
					default:
						score *= 0.5
					}
				}
				isMovie := strings.EqualFold(candidate.Type, "movie")
				if (m.Format == "MOVIE") != isMovie {
					score *= 0.25
				}
				if score > bestScore {
					secondScore, bestScore, best = bestScore, score, candidate
				} else if score > secondScore {
					secondScore = score
				}
			}
		}
	}
	if best.Slug == "" || bestScore < 0.6 || (secondScore > 0 && bestScore-secondScore < 0.08) {
		return nil, fmt.Errorf("KAA: no unambiguous title match")
	}
	var info struct {
		Locales  []string `json:"locales"`
		Type     string   `json:"type"`
		WatchURI string   `json:"watch_uri"`
	}
	if err := p.json(ctx, "/api/show/"+url.PathEscape(best.Slug), nil, &info); err != nil {
		return nil, err
	}
	if audio == "dub" && !contains(info.Locales, "en-US") && !contains(best.Locales, "en-US") {
		return nil, fmt.Errorf("KAA has no English dub")
	}
	var first struct {
		Result []struct {
			Episode int    `json:"episode_number"`
			Slug    string `json:"slug"`
			Title   string `json:"title"`
		} `json:"result"`
		Pages []struct {
			Episodes []int `json:"eps"`
		} `json:"pages"`
	}
	if err := p.json(ctx, "/api/show/"+url.PathEscape(best.Slug)+"/episodes", url.Values{"ep": {"1"}, "lang": {"ja-JP"}}, &first); err != nil {
		return nil, err
	}
	episodeSlug := ""
	find := func(items []struct {
		Episode int    `json:"episode_number"`
		Slug    string `json:"slug"`
		Title   string `json:"title"`
	}) {
		for _, item := range items {
			if item.Episode == ep && item.Slug != "" {
				episodeSlug = fmt.Sprintf("ep-%d-%s", ep, item.Slug)
			}
		}
	}
	find(first.Result)
	if episodeSlug == "" {
		for _, page := range first.Pages {
			if len(page.Episodes) == 0 || ep < page.Episodes[0] {
				continue
			}
			var next struct {
				Result []struct {
					Episode int    `json:"episode_number"`
					Slug    string `json:"slug"`
					Title   string `json:"title"`
				} `json:"result"`
			}
			if p.json(ctx, "/api/show/"+url.PathEscape(best.Slug)+"/episodes", url.Values{"ep": {strconv.Itoa(page.Episodes[0])}, "lang": {"ja-JP"}}, &next) == nil {
				find(next.Result)
			}
			if episodeSlug != "" {
				break
			}
		}
	}
	if episodeSlug == "" && strings.EqualFold(info.Type, "movie") {
		if found := regexp.MustCompile(`/ep-(\d+-[a-f0-9]+)$`).FindStringSubmatch(info.WatchURI); len(found) > 1 {
			episodeSlug = found[1]
		}
	}
	if episodeSlug == "" {
		return nil, fmt.Errorf("KAA episode %d is unavailable", ep)
	}
	var servers struct {
		Servers []struct {
			Src  string `json:"src"`
			Name string `json:"name"`
		} `json:"servers"`
	}
	if err := p.json(ctx, "/api/show/"+url.PathEscape(best.Slug)+"/episode/"+url.PathEscape(episodeSlug), nil, &servers); err != nil {
		return nil, err
	}
	for _, server := range servers.Servers {
		u, err := url.Parse(server.Src)
		if err != nil {
			continue
		}
		id := u.Query().Get("id")
		if id == "" {
			continue
		}
		hls := "https://hls.krussdomi.com/manifest/" + url.PathEscape(id) + "/master.m3u8"
		return &catalog.Stream{URL: hls, MimeType: "application/x-mpegURL", Referer: "https://krussdomi.com/", Headers: map[string]string{"Referer": "https://krussdomi.com/"}}, nil
	}
	return nil, fmt.Errorf("KAA has no direct stream for episode %d", ep)
}

func requestJSON(ctx context.Context, client *http.Client, method, target string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = defaultHTTP
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return upstreamStatusError(res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 8<<20 {
		return fmt.Errorf("upstream response too large")
	}
	return json.Unmarshal(b, out)
}

func isHLS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && validURL(raw) && strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
}
func isMP4(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && validURL(raw) && strings.HasSuffix(strings.ToLower(u.Path), ".mp4")
}
func subtitleMimeFromURL(raw string) string {
	u, _ := url.Parse(raw)
	switch strings.ToLower(u.Path[strings.LastIndex(u.Path, ".")+1:]) {
	case "srt":
		return "application/x-subrip"
	case "ass", "ssa":
		return "text/x-ssa"
	default:
		return "text/vtt"
	}
}
func languageCode(label string) string {
	fields := strings.Fields(label)
	if len(fields) == 0 {
		return ""
	}
	lower := strings.ToLower(fields[0])
	if lower == "english" {
		return "en"
	}
	if len(lower) > 2 {
		return lower[:2]
	}
	return lower
}
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func diceScore(titles []string, candidate string) float64 {
	best := 0.0
	b := normalized(candidate)
	if b == "" {
		return 0
	}
	for _, title := range titles {
		a := normalized(title)
		if a == b {
			return 1
		}
		if len(a) < 2 || len(b) < 2 {
			continue
		}
		pairs := map[string]int{}
		for i := 0; i+1 < len(a); i++ {
			pairs[a[i:i+2]]++
		}
		hits := 0
		for i := 0; i+1 < len(b); i++ {
			if pairs[b[i:i+2]] > 0 {
				hits++
				pairs[b[i:i+2]]--
			}
		}
		score := float64(2*hits) / float64(len(a)+len(b)-2)
		if score > best {
			best = score
		}
	}
	return best
}
func parseEmbeddedObject(input, prefix string) []byte {
	loc := regexp.MustCompile(prefix).FindStringIndex(input)
	if loc == nil {
		return nil
	}
	start := loc[1]
	for start < len(input) && input[start] != '{' && input[start] != '[' {
		start++
	}
	if start == len(input) {
		return nil
	}
	open := input[start]
	close := byte('}')
	if open == '[' {
		close = ']'
	}
	depth, quote, escaped := 0, byte(0), false
	for i := start; i < len(input); i++ {
		ch := input[i]
		if quote != 0 {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
		} else if ch == open {
			depth++
		} else if ch == close {
			depth--
			if depth == 0 {
				candidate := []byte(input[start : i+1])
				candidate = regexp.MustCompile(`\\u0026`).ReplaceAll(candidate, []byte(`&`))
				candidate = regexp.MustCompile(`\\"`).ReplaceAll(candidate, []byte(`"`))
				return candidate
			}
		}
	}
	return nil
}

func abs(a, b int) int {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}

func base64Decode(raw string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		b, err = base64.RawStdEncoding.DecodeString(raw)
	}
	return string(b), err
}

func requestForm(ctx context.Context, client *http.Client, target string, body []byte, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", strings.TrimRight(referer, "/"))
	req.Header.Set("Referer", referer)
	if client == nil {
		client = defaultHTTP
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, upstreamStatusError(res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 8<<20 {
		return nil, fmt.Errorf("upstream response too large")
	}
	return b, nil
}

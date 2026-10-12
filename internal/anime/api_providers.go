package anime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"projectorremote/internal/catalog"
)

// wireID accepts APIs that alternate between a numeric ID and a string.
type wireID string

func (s *wireID) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var text string
	if len(b) > 0 && b[0] == '"' {
		if e := json.Unmarshal(b, &text); e != nil {
			return e
		}
	} else {
		text = string(b)
	}
	*s = wireID(text)
	return nil
}

type APISubtitle struct {
	URL      string `json:"url"`
	Src      string `json:"src"`
	File     string `json:"file"`
	Label    string `json:"label"`
	Language string `json:"language"`
	Lang     string `json:"lang"`
	SrcLang  string `json:"srclang"`
	Format   string `json:"format"`
	Default  bool   `json:"default"`
}

func subtitles(in []APISubtitle) []catalog.Subtitle {
	out := []catalog.Subtitle{}
	for _, s := range in {
		u := s.URL
		if u == "" {
			u = s.Src
		}
		if u == "" {
			u = s.File
		}
		if !validURL(u) {
			continue
		}
		lang := s.Language
		if lang == "" {
			lang = s.SrcLang
		}
		if lang == "" {
			lang = s.Lang
		}
		format := s.Format
		if format == "" {
			parsed, _ := url.Parse(u)
			for _, extension := range []string{"srt", "ass", "ssa"} {
				if strings.HasSuffix(strings.ToLower(parsed.Path), "."+extension) {
					format = extension
					break
				}
			}
		}
		out = append(out, catalog.Subtitle{URL: u, Label: s.Label, Language: lang, MimeType: subtitleMime(format), Default: s.Default})
	}
	return out
}
func streamMime(kind, raw string) string {
	switch strings.ToLower(kind) {
	case "hls", "m3u8":
		return "application/x-mpegURL"
	case "mp4":
		return "video/mp4"
	case "embed":
		return ""
	}
	u, e := url.Parse(raw)
	if e == nil {
		if strings.HasSuffix(u.Path, ".m3u8") {
			return "application/x-mpegURL"
		}
		if strings.HasSuffix(u.Path, ".mp4") {
			return "video/mp4"
		}
	}
	return ""
}

type AniKoto struct {
	Base   string
	Client *http.Client
}

func (*AniKoto) Name() string { return "anikoto" }
func (a *AniKoto) get(ctx context.Context, path string, out any) error {
	var envelope struct {
		Success *bool           `json:"success"`
		Results json.RawMessage `json:"results"`
	}
	if err := getJSON(ctx, a.Client, endpoint(a.Base, path), &envelope); err != nil {
		return err
	}
	if envelope.Success != nil && !*envelope.Success {
		return fmt.Errorf("AniKoto request failed")
	}
	if len(envelope.Results) == 0 || string(envelope.Results) == "null" {
		return fmt.Errorf("AniKoto missing results")
	}
	return json.Unmarshal(envelope.Results, out)
}
func (a *AniKoto) Resolve(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	if strings.TrimSpace(a.Base) == "" {
		return nil, fmt.Errorf("AniKoto API URL is not configured")
	}
	slug := ""
	candidates := []candidate{}
	seenQueries := map[string]bool{}
	for i, q := range m.titles() {
		if i >= 2 {
			break
		}
		key := normalized(q)
		if key == "" || seenQueries[key] {
			continue
		}
		seenQueries[key] = true
		var search struct {
			Data []struct {
				Slug          string `json:"slug"`
				Title         string `json:"title"`
				JapaneseTitle string `json:"japaneseTitle"`
			} `json:"data"`
		}
		if err := a.get(ctx, "/search?keyword="+url.QueryEscape(q), &search); err != nil {
			return nil, err
		}
		for _, v := range search.Data {
			candidates = append(candidates, candidate{strings.Split(v.Slug, "/")[0], []string{v.Title, v.JapaneseTitle}})
		}
	}
	if found, err := uniqueMatch(m, candidates); err == nil {
		slug = found
	}
	if slug == "" {
		return nil, fmt.Errorf("AniKoto: no exact match")
	}
	var episodes struct {
		Episodes []struct {
			Number    wireID `json:"episode_no"`
			ServerIDs string `json:"server_ids"`
			MALID     wireID `json:"mal_id"`
		} `json:"episodes"`
	}
	if err := a.get(ctx, "/episodes/"+url.PathEscape(slug), &episodes); err != nil {
		return nil, err
	}
	ids := ""
	for _, v := range episodes.Episodes {
		if string(v.Number) == strconv.Itoa(ep) {
			if v.MALID != "" && m.IDMal > 0 && string(v.MALID) != strconv.Itoa(m.IDMal) {
				return nil, fmt.Errorf("AniKoto: MAL identity mismatch")
			}
			ids = v.ServerIDs
			break
		}
	}
	if ids == "" {
		return nil, fmt.Errorf("AniKoto: episode unavailable")
	}
	var servers []struct {
		Type   string `json:"type"`
		LinkID wireID `json:"link_id"`
	}
	// server_ids is opaque. The maintained endpoint forwards it unchanged.
	if err := a.get(ctx, "/servers?ids="+url.QueryEscape(ids), &servers); err != nil {
		return nil, err
	}
	for _, server := range servers {
		if server.Type != audio || server.LinkID == "" {
			continue
		}
		var v struct {
			skipFields
			URL       string            `json:"url"`
			Type      string            `json:"type"`
			EmbedURL  string            `json:"embedUrl"`
			Referer   string            `json:"referer"`
			Headers   map[string]string `json:"headers"`
			Subtitles []APISubtitle     `json:"subtitles"`
		}
		if err := a.get(ctx, "/stream/resolve?"+url.Values{"id": {string(server.LinkID)}, "slug": {slug}}.Encode(), &v); err != nil {
			continue
		}
		mime := streamMime(v.Type, v.URL)
		if !validURL(v.URL) || mime == "" {
			continue
		}
		referer := v.Referer
		if referer == "" && validURL(v.EmbedURL) {
			u, _ := url.Parse(v.EmbedURL)
			referer = u.Scheme + "://" + u.Host + "/"
		}
		return &catalog.Stream{URL: v.URL, MimeType: mime, Referer: referer, Headers: v.Headers, Subtitles: subtitles(v.Subtitles), SkipRanges: v.ranges()}, nil
	}
	return nil, fmt.Errorf("AniKoto: no playable %s source", audio)
}

// ReAnime uses the HTTP server documented by n1yshi/ReAnime-API. There is
// no documented public deployment; its base URL must be configured.
type ReAnime struct {
	Base   string
	Client *http.Client
}

func (*ReAnime) Name() string { return "reanime" }
func (a *ReAnime) Resolve(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	if a.Base == "" {
		return nil, fmt.Errorf("ReAnime API URL is not configured")
	}
	var out struct {
		skipFields
		Audio   string `json:"audio"`
		Streams []struct {
			skipFields
			URL     string            `json:"url"`
			Type    string            `json:"type"`
			Referer string            `json:"referer"`
			Headers map[string]string `json:"headers"`
		} `json:"streams"`
		Subtitles []APISubtitle `json:"subtitles"`
	}
	if err := getJSON(ctx, a.Client, endpoint(a.Base, fmt.Sprintf("/anime/%d/%d/%s", m.ID, ep, audio)), &out); err != nil {
		return nil, err
	}
	if out.Audio != "" && out.Audio != audio {
		return nil, fmt.Errorf("ReAnime returned wrong audio")
	}
	for _, v := range out.Streams {
		mime := streamMime(v.Type, v.URL)
		if mime == "" || !validURL(v.URL) {
			continue
		}
		ref := v.Referer
		if ref == "" {
			ref = "https://flixcloud.cc/"
		}
		return &catalog.Stream{URL: v.URL, Referer: ref, Headers: v.Headers, MimeType: mime, Subtitles: subtitles(out.Subtitles), SkipRanges: v.ranges().Fill(out.ranges())}, nil
	}
	return nil, fmt.Errorf("ReAnime: no playable source")
}

type Config struct {
	SearchURL, EpisodesURL, AniListURL                         string
	AniNekoURL, AniKotoURL, ReAnimeURL, AniZoneURL, AnimeGGURL string
	AniBDURL, SenshiURL, KAAURL, AnimeDunyaURL                 string
	AniDBAppURL, AnimeNoSubURL                                 string
	AnimeOnsenURL                                              string
	AniWavesURL                                                string
}

func New(c Config) *Service {
	defaults := []struct {
		value    *string
		fallback string
	}{{&c.SearchURL, "https://kitsu.io/api/edge/anime"}, {&c.EpisodesURL, "https://api.ani.zip/mappings"}, {&c.AniListURL, "https://graphql.anilist.co"}, {&c.AniNekoURL, "https://anineko.to"}, {&c.AniKotoURL, "https://anikototv.to"}, {&c.AniZoneURL, "https://anizone.to"}, {&c.AnimeGGURL, "https://www.animegg.org"}, {&c.AniBDURL, "https://epeng.animeapps.top"}, {&c.SenshiURL, "https://senshi.to"}, {&c.KAAURL, "https://kaa.lt"}, {&c.AnimeDunyaURL, "https://anime-dunya.com"}, {&c.AniDBAppURL, "https://anidb.app"}, {&c.AnimeNoSubURL, "https://animenosub.to"}, {&c.AnimeOnsenURL, "https://www.animeonsen.xyz"}, {&c.AniWavesURL, "https://aniwaves.ru"}}
	for _, v := range defaults {
		if *v.value == "" {
			*v.value = v.fallback
		}
	}
	return &Service{SkipClient: &SkipClient{}, PrefixSearch: &PrefixSearch{URL: c.SearchURL}, Concurrent: true, EpisodeMetadata: NewEpisodeMetadata(c.EpisodesURL), Metadata: NewMetadata(c.AniListURL), Providers: []Provider{
		&ReAnime{Base: c.ReAnimeURL, Client: defaultHTTP},
		&Scraper{Kind: "animegg", Base: c.AnimeGGURL, Client: defaultHTTP},
		&Scraper{Kind: "anineko", Base: c.AniNekoURL, Client: defaultHTTP},
		&ReferenceProvider{Kind: "anikoto", Base: c.AniKotoURL, Client: defaultHTTP},
		&Scraper{Kind: "anizone", Base: c.AniZoneURL, Client: defaultHTTP},
		&ReferenceProvider{Kind: "anibd", Base: c.AniBDURL, Client: defaultHTTP},
		&ReferenceProvider{Kind: "senshi", Base: c.SenshiURL, Client: defaultHTTP},
		&ReferenceProvider{Kind: "kaa", Base: c.KAAURL, Client: defaultHTTP},
		&ReferenceProvider{Kind: "animedunya", Base: c.AnimeDunyaURL, Client: defaultHTTP},
		&ReferenceProvider{Kind: "anidbapp", Base: c.AniDBAppURL, Client: defaultHTTP},
		&ReferenceProvider{Kind: "animenosub", Base: c.AnimeNoSubURL, Client: defaultHTTP},
		&ReferenceProvider{Kind: "animeonsen", Base: c.AnimeOnsenURL, Client: defaultHTTP},
		&ReferenceProvider{Kind: "aniwaves", Base: c.AniWavesURL, Client: defaultHTTP},
	}}
}

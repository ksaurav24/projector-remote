package anime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"projectorremote/internal/catalog"
)

type siteCandidate struct {
	slug, title, japanese string
	siteID                string
}

func (p *ReferenceProvider) ajax(ctx context.Context, path, referer string, out any) error {
	headers := map[string]string{"Referer": referer, "X-Requested-With": "XMLHttpRequest", "Accept": "application/json, text/javascript, */*; q=0.01"}
	b, err := request(ctx, p.Client, http.MethodGet, endpoint(p.Base, path), nil, headers)
	if err != nil {
		return err
	}
	var envelope struct {
		Status json.RawMessage `json:"status"`
		Result json.RawMessage `json:"result"`
		URL    string          `json:"url"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return fmt.Errorf("ajax %s: %w", path, err)
	}
	if len(envelope.Result) == 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("ajax %s result: %w", path, err)
		}
		return nil
	}
	var text string
	if json.Unmarshal(envelope.Result, &text) == nil {
		if target, ok := out.(*string); ok {
			*target = text
			return nil
		}
		if err := json.Unmarshal([]byte(text), out); err != nil {
			return fmt.Errorf("ajax %s payload: %w", path, err)
		}
		return nil
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return fmt.Errorf("ajax %s payload: %w", path, err)
	}
	return nil
}

func (p *ReferenceProvider) resolveAniKoto(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	candidates := []candidate{}
	queries := map[string]bool{}
	for _, query := range m.titles()[:min(5, len(m.titles()))] {
		key := normalized(query)
		if key == "" || queries[key] {
			continue
		}
		queries[key] = true
		page, err := request(ctx, p.Client, http.MethodGet, endpoint(p.Base, "/filter?keyword="+url.QueryEscape(query)), nil, map[string]string{"Referer": p.Base + "/"})
		if err != nil {
			continue
		}
		for _, block := range regexp.MustCompile(`(?is)<a\b[^>]*>.*?</a>`).FindAllString(string(page), -1) {
			tag := regexp.MustCompile(`(?is)<a\b[^>]*>`).FindString(block)
			if !strings.Contains(strings.ToLower(attr(tag, "class")), "d-title") {
				continue
			}
			href := attr(tag, "href")
			slug := capture(`/watch/([^/?#]+)`, href)
			if slug == "" {
				continue
			}
			name := plain(block)
			candidates = append(candidates, candidate{slug, []string{name, attr(tag, "data-jp")}})
		}
	}
	slug, err := uniqueMatch(m, candidates)
	if err != nil {
		return nil, fmt.Errorf("AniKoto: %w", err)
	}
	showPage, err := request(ctx, p.Client, http.MethodGet, endpoint(p.Base, "/watch/"+url.PathEscape(slug)), nil, map[string]string{"Referer": p.Base + "/"})
	if err != nil {
		return nil, err
	}
	showID := capture(`data-id=["'](\d+)["']`, string(showPage))
	if showID == "" {
		return nil, fmt.Errorf("AniKoto show ID missing")
	}
	var episodeHTML string
	if err := p.ajax(ctx, "/ajax/episode/list/"+url.PathEscape(showID), p.Base+"/watch/"+slug, &episodeHTML); err != nil {
		return nil, err
	}
	var episode struct{ ids, slug, timestamp, mal string }
	for _, block := range regexp.MustCompile(`(?is)<a\b[^>]*data-id=["'][^"']*["'][^>]*>`).FindAllString(episodeHTML, -1) {
		if _, err := strconv.Atoi(attr(block, "data-num")); err == nil && attr(block, "data-num") == strconv.Itoa(ep) {
			episode.ids, episode.slug, episode.timestamp, episode.mal = attr(block, "data-ids"), attr(block, "data-slug"), attr(block, "data-timestamp"), attr(block, "data-mal")
			break
		}
	}
	if episode.ids == "" {
		return nil, fmt.Errorf("AniKoto: episode %d unavailable", ep)
	}
	if episode.mal != "" && m.IDMal > 0 && episode.mal != strconv.Itoa(m.IDMal) {
		return nil, fmt.Errorf("AniKoto: MAL identity mismatch")
	}
	var serverHTML string
	if err := p.ajax(ctx, "/ajax/server/list?servers="+url.QueryEscape(episode.ids), p.Base+"/", &serverHTML); err != nil {
		return nil, err
	}
	type link struct{ id, name string }
	links := []link{}
	for _, group := range regexp.MustCompile(`(?is)<div\b[^>]*data-type=["']([^"']+)["'][^>]*>.*?</ul>\s*</div>`).FindAllStringSubmatch(serverHTML, -1) {
		if group[1] != audio && !(audio == "sub" && group[1] == "hsub") {
			continue
		}
		for _, li := range regexp.MustCompile(`(?is)<li\b[^>]*data-link-id=["'][^"']+["'][^>]*>.*?</li>`).FindAllString(group[0], -1) {
			id := attr(li, "data-link-id")
			name := plain(li)
			if id != "" && !strings.Contains(strings.ToLower(name), "download") && !strings.Contains(strings.ToLower(name), "kiwi") {
				links = append(links, link{id, name})
			}
		}
	}
	for _, item := range links {
		var resolved struct {
			skipFields
			URL string `json:"url"`
		}
		if validURL(item.id) {
			resolved.URL = item.id
		} else if err := p.ajax(ctx, "/ajax/server?get="+url.QueryEscape(item.id), p.Base+"/", &resolved); err != nil {
			continue
		}
		stream := p.directOrEmbed(ctx, resolved.URL, p.Base+"/")
		if stream != nil {
			stream.SkipRanges = resolved.ranges()
			return stream, nil
		}
	}
	return nil, fmt.Errorf("AniKoto: no direct %s HLS/MP4 source", audio)
}

func (p *ReferenceProvider) resolveAniWaves(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	candidates := []candidate{}
	queries := map[string]bool{}
	for _, query := range m.titles()[:min(8, len(m.titles()))] {
		key := normalized(query)
		if key == "" || queries[key] {
			continue
		}
		queries[key] = true
		page, err := request(ctx, p.Client, http.MethodGet, endpoint(p.Base, "/filter?keyword="+url.QueryEscape(query)), nil, map[string]string{"Referer": p.Base + "/"})
		if err != nil {
			continue
		}
		for _, block := range regexp.MustCompile(`(?is)<a\b[^>]*>.*?</a>`).FindAllString(string(page), -1) {
			tag := regexp.MustCompile(`(?is)<a\b[^>]*>`).FindString(block)
			if !strings.Contains(strings.ToLower(attr(tag, "class")), "d-title") {
				continue
			}
			slug := capture(`/watch/([a-z0-9-]+)`, attr(tag, "href"))
			if slug == "" || capture(`-(\d+)$`, slug) == "" {
				continue
			}
			candidates = append(candidates, candidate{slug, []string{plain(block), attr(tag, "data-jp")}})
		}
	}
	slug, err := uniqueMatch(m, candidates)
	if err != nil {
		return nil, fmt.Errorf("AniWaves: %w", err)
	}
	siteID := capture(`-(\d+)$`, slug)
	var episodeHTML string
	watchPath := "/watch/" + slug
	if err := p.ajax(ctx, "/ajax/episode/list/"+url.PathEscape(siteID)+"?vrf=", p.Base+watchPath, &episodeHTML); err != nil {
		return nil, err
	}
	var episode struct{ ids, source string }
	for _, tag := range regexp.MustCompile(`(?is)<a\b[^>]*data-num=["'][^"']+["'][^>]*>`).FindAllString(episodeHTML, -1) {
		if attr(tag, "data-num") == strconv.Itoa(ep) && attr(tag, "data-ids") != "" {
			if (audio == "sub" && attr(tag, "data-sub") == "1") || (audio == "dub" && attr(tag, "data-dub") == "1") {
				episode.ids, episode.source = attr(tag, "data-ids"), attr(tag, "data-slug")
				break
			}
		}
	}
	if episode.ids == "" {
		return nil, fmt.Errorf("AniWaves: episode unavailable")
	}
	referer := p.Base + watchPath + "/ep-" + episode.source
	var serversHTML string
	if err := p.ajax(ctx, "/ajax/server/list?servers="+url.QueryEscape(siteID)+"&eps="+url.QueryEscape(episode.source), referer, &serversHTML); err != nil {
		return nil, err
	}
	markers := regexp.MustCompile(`(?is)<div\b[^>]*data-type=["']([^"']+)["'][^>]*>`).FindAllStringSubmatchIndex(serversHTML, -1)
	for i, marker := range markers {
		if serversHTML[marker[2]:marker[3]] != audio {
			continue
		}
		end := len(serversHTML)
		if i+1 < len(markers) {
			end = markers[i+1][0]
		}
		groupHTML := serversHTML[marker[0]:end]
		for _, li := range regexp.MustCompile(`(?is)<li\b[^>]*data-link-id=["'][^"']+["'][^>]*>.*?</li>`).FindAllString(groupHTML, -1) {
			linkID := attr(li, "data-link-id")
			var source struct {
				skipFields
				URL string `json:"url"`
			}
			if err := p.ajax(ctx, "/ajax/sources?id="+url.QueryEscape(linkID)+"&asi=0&autoPlay=0", referer, &source); err != nil {
				continue
			}
			if stream := p.directOrEmbed(ctx, source.URL, referer); stream != nil {
				stream.SkipRanges = source.ranges()
				return stream, nil
			}
		}
	}
	return nil, fmt.Errorf("AniWaves: no direct %s HLS/MP4 source", audio)
}

func (p *ReferenceProvider) directOrEmbed(ctx context.Context, raw, referer string) *catalog.Stream {
	if !validURL(raw) {
		return nil
	}
	if isHLS(raw) || isMP4(raw) {
		return &catalog.Stream{URL: raw, MimeType: streamMime("", raw), Referer: referer}
	}
	if direct := decodeURLFragment(raw); direct != "" {
		return &catalog.Stream{URL: direct, MimeType: "application/x-mpegURL", Referer: referer}
	}
	page, err := request(ctx, p.Client, http.MethodGet, raw, nil, map[string]string{"Referer": referer})
	if err != nil {
		return nil
	}
	hls := hlsFromHTML(string(page))
	if validURL(hls) {
		u, _ := url.Parse(raw)
		return &catalog.Stream{URL: hls, MimeType: "application/x-mpegURL", Referer: u.Scheme + "://" + u.Host + "/"}
	}
	return nil
}

func decodeURLFragment(raw string) string {
	parts := strings.SplitN(raw, "#", 2)
	if len(parts) != 2 {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(parts[1])
	}
	if err == nil && isHLS(string(decoded)) {
		return string(decoded)
	}
	return ""
}

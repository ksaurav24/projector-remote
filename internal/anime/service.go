// Package anime keeps AniList identities separate from replaceable stream
// adapters. A provider outage never changes IDs in watch history or the PWA.
package anime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"projectorremote/internal/catalog"
)

// Provider resolves a canonical AniList title, episode and requested audio.
// Adapters must reject ambiguous matches and HTML/embed-only responses.
type Provider interface {
	Name() string
	Resolve(context.Context, Media, int, string) (*catalog.Stream, error)
}

type Service struct {
	PrefixSearch    *PrefixSearch
	Metadata        *Metadata
	EpisodeMetadata *EpisodeMetadata
	Providers       []Provider
	Timeout         time.Duration
	Concurrent      bool
}

func (s *Service) Home(ctx context.Context, _ string) (*catalog.Home, error) {
	return s.Metadata.Home(ctx)
}
func (s *Service) Search(ctx context.Context, _, q string) ([]catalog.SearchItem, error) {
	items, err := s.Metadata.Search(ctx, q)
	if len(items) > 0 || s.PrefixSearch == nil {
		return items, err
	}
	fallback, fallbackErr := s.PrefixSearch.Search(ctx, q)
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	if len(fallback) == 0 && err != nil {
		return nil, err
	}
	return fallback, nil
}
func parseID(raw string, parts int) (int, string, int, error) {
	p := strings.Split(raw, ":")
	if len(p) != parts {
		return 0, "", 0, fmt.Errorf("%w: invalid anime ID", catalog.ErrInvalidArgument)
	}
	id, err := strconv.Atoi(p[0])
	if err != nil || id < 1 {
		return 0, "", 0, fmt.Errorf("%w: invalid AniList ID", catalog.ErrInvalidArgument)
	}
	audio := "sub"
	ep := 1
	if parts > 1 {
		audio = p[1]
		if audio != "sub" && audio != "dub" {
			return 0, "", 0, fmt.Errorf("%w: invalid anime audio", catalog.ErrInvalidArgument)
		}
	}
	if parts > 2 {
		ep, err = strconv.Atoi(p[2])
		if err != nil || ep < 1 {
			return 0, "", 0, fmt.Errorf("%w: invalid anime episode", catalog.ErrInvalidArgument)
		}
	}
	return id, audio, ep, nil
}
func (s *Service) Title(ctx context.Context, _, raw string) (*catalog.Title, error) {
	id, _, _, err := parseID(raw, 1)
	if err != nil {
		return nil, err
	}
	m, err := s.Metadata.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// Each AniList entry is one release. Audio editions share the same release
	// ID, but have separate episode IDs so resume never silently changes audio.
	t := &catalog.Title{ID: raw, Type: "series", Title: m.name(), Desc: plain(m.Description), Poster: m.CoverImage.Large, Backdrop: m.image(), Rating: score(m.AverageScore), Langs: []catalog.Lang{}, Seasons: []catalog.Season{}, Suggest: []catalog.Poster{}}
	if m.StartDate.Year > 0 {
		t.Year = strconv.Itoa(m.StartDate.Year)
	}
	if m.Duration > 0 {
		t.Runtime = fmt.Sprintf("%d min", m.Duration)
	}
	for i, release := range s.Metadata.Releases(ctx, m) {
		id := strconv.Itoa(release.ID)
		t.Seasons = append(t.Seasons, catalog.Season{ID: id + ":sub", Number: i + 1, Label: release.name(), Episodes: release.aired(), Editions: []catalog.Edition{
			{ID: id + ":sub", Code: "sub", Label: "Subtitled"}, {ID: id + ":dub", Code: "dub", Label: "English dub"},
		}})
	}
	t.SeasonID = raw + ":sub"
	t.Episodes = s.episodePage(ctx, m, "sub", 1)
	return t, nil
}
func episodePage(m Media, audio string, page int) *catalog.EpisodePage {
	const size = 50
	out := &catalog.EpisodePage{Items: []catalog.Episode{}}
	total := m.aired()
	if total > 10000 {
		total = 10000
	}
	start := (page-1)*size + 1
	for ep := start; ep <= total && ep < start+size; ep++ {
		out.Items = append(out.Items, catalog.Episode{ID: fmt.Sprintf("%d:%s:%d", m.ID, audio, ep), Num: ep, Title: fmt.Sprintf("Episode %d", ep), Still: m.CoverImage.Large, Runtime: fmt.Sprintf("%d min", m.Duration)})
	}
	if start+size <= total {
		next := page + 1
		out.NextPage = &next
	}
	return out
}
func (s *Service) Season(ctx context.Context, _, raw string, page int) (*catalog.EpisodePage, error) {
	id, audio, _, err := parseID(raw, 2)
	if err != nil {
		return nil, err
	}
	if page < 1 || page > 201 {
		return nil, fmt.Errorf("%w: invalid episode page", catalog.ErrInvalidArgument)
	}
	m, err := s.Metadata.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.episodePage(ctx, m, audio, page), nil
}
func (s *Service) Resolve(ctx context.Context, _, raw string) (*catalog.Stream, error) {
	return s.resolve(ctx, raw, "")
}

// ResolveProvider keeps the Anivexa-compatible provider-specific watch route
// while still anchoring every lookup to AniList metadata.
func (s *Service) ResolveProvider(ctx context.Context, provider, raw string) (*catalog.Stream, error) {
	id, audio, ep, err := parseID(raw, 3)
	if err != nil {
		return nil, err
	}
	m, err := s.Metadata.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if ep > m.aired() {
		return nil, fmt.Errorf("%w: episode is not released for this AniList title", catalog.ErrInvalidArgument)
	}
	for _, p := range s.Providers {
		if p.Name() != provider {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, s.providerTimeout())
		defer cancel()
		stream, err := p.Resolve(ctx, m, ep, audio)
		if err != nil {
			return nil, err
		}
		if stream == nil || !validURL(stream.URL) || (stream.MimeType != "application/x-mpegURL" && stream.MimeType != "video/mp4") {
			return nil, fmt.Errorf("%s: no playable HLS or MP4 stream", provider)
		}
		stream.Provider = provider
		return stream, nil
	}
	return nil, fmt.Errorf("%w: unknown anime provider", catalog.ErrInvalidArgument)
}

func (s *Service) providerTimeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return 12 * time.Second
}
func (s *Service) Recover(ctx context.Context, _, raw string, failed *catalog.Stream) (*catalog.Stream, error) {
	return s.resolve(ctx, raw, failed.Provider, failed.FailedProviders...)
}
func (s *Service) resolve(ctx context.Context, raw, after string, excluded ...string) (*catalog.Stream, error) {
	id, audio, ep, err := parseID(raw, 3)
	if err != nil {
		return nil, err
	}
	m, err := s.Metadata.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if ep > m.aired() {
		return nil, fmt.Errorf("%w: episode is not released for this AniList title", catalog.ErrInvalidArgument)
	}
	if s.Concurrent {
		return s.resolveConcurrent(ctx, m, ep, audio, append(excluded, after))
	}
	timeout := s.providerTimeout()
	start := 0
	for i, p := range s.Providers {
		if p.Name() == after {
			start = i + 1
			break
		}
	}
	for _, p := range s.Providers[start:] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempt, cancel := context.WithTimeout(ctx, timeout)
		stream, err := p.Resolve(attempt, m, ep, audio)
		cancel()
		if err == nil && stream != nil && validURL(stream.URL) && (stream.MimeType == "application/x-mpegURL" || stream.MimeType == "video/mp4") {
			stream.Provider = p.Name()
			// Prefer an English external subtitle when the provider gives no
			// default. The player's saved "off" preference still takes priority.
			hasDefault := false
			for _, sub := range stream.Subtitles {
				hasDefault = hasDefault || sub.Default
			}
			if audio == "sub" && !hasDefault {
				for i, sub := range stream.Subtitles {
					lang := strings.ToLower(sub.Language)
					if lang == "en" || lang == "eng" || strings.HasPrefix(lang, "en-") || strings.EqualFold(sub.Label, "English") {
						stream.Subtitles[i].Default = true
						break
					}
				}
			}
			return stream, nil
		}
		if err == nil {
			err = errors.New("no playable HLS or MP4 stream")
		}
		log.Printf("anime: %s failed for %d episode %d (%s): %v", p.Name(), id, ep, audio, err)
	}
	return nil, fmt.Errorf("anime: no available %s source for episode %d", audio, ep)
}

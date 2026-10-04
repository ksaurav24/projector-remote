package anime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"projectorremote/internal/catalog"
)

// Independent adapters race under one deadline. A broken provider cannot hold
// a healthy source behind its timeouts. Losers stop as soon as a source wins.
func (s *Service) resolveConcurrent(ctx context.Context, m Media, ep int, audio string, failed []string) (*catalog.Stream, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 40 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		stream *catalog.Stream
		err    error
		name   string
	}
	results := make(chan result, len(s.Providers))
	count := 0
	for _, p := range s.Providers {
		if slices.Contains(failed, p.Name()) {
			continue
		}
		count++
		go func(p Provider) {
			stream, err := p.Resolve(ctx, m, ep, audio)
			if err == nil && (stream == nil || !validURL(stream.URL) || (stream.MimeType != "application/x-mpegURL" && stream.MimeType != "video/mp4")) {
				err = fmt.Errorf("no playable stream")
			}
			results <- result{stream, err, p.Name()}
		}(p)
	}
	timedOut := false
	for range count {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("anime: sources timed out for episode %d: %w", ep, ctx.Err())
		case r := <-results:
			if r.err != nil {
				timedOut = timedOut || errors.Is(r.err, context.DeadlineExceeded)
				log.Printf("anime: %s failed for %d episode %d (%s): %v", r.name, m.ID, ep, audio, r.err)
				continue
			}
			r.stream.Provider = r.name
			r.stream.FailedProviders = append([]string(nil), failed...)
			hasDefault := false
			for _, sub := range r.stream.Subtitles {
				hasDefault = hasDefault || sub.Default
			}
			if audio == "sub" && !hasDefault {
				for i, sub := range r.stream.Subtitles {
					if sub.Language == "en" || sub.Language == "eng" || strings.HasPrefix(sub.Language, "en-") || strings.EqualFold(sub.Label, "English") {
						r.stream.Subtitles[i].Default = true
						break
					}
				}
			}
			return r.stream, nil
		}
	}
	if timedOut {
		return nil, fmt.Errorf("anime: providers timed out for episode %d: %w", ep, context.DeadlineExceeded)
	}
	return nil, fmt.Errorf("anime: no available %s source for episode %d", audio, ep)
}

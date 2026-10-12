package anime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"projectorremote/internal/catalog"
)

// Malformed optional metadata must never reject a playable source.
type skipFields struct {
	Intro        json.RawMessage `json:"intro"`
	Outro        json.RawMessage `json:"outro"`
	IntroChapter json.RawMessage `json:"intro_chapter"`
	OutroChapter json.RawMessage `json:"outro_chapter"`
	IntroStart   json.RawMessage `json:"intro_start"`
	IntroEnd     json.RawMessage `json:"intro_end"`
	OutroStart   json.RawMessage `json:"outro_start"`
	OutroEnd     json.RawMessage `json:"outro_end"`
	SkipData     json.RawMessage `json:"skip_data"`
}

func skipNumber(raw json.RawMessage) float64 {
	var v float64
	if len(raw) == 0 || string(raw) == "null" {
		return math.NaN()
	}
	if json.Unmarshal(raw, &v) == nil {
		return v
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n
		}
	}
	return math.NaN()
}

func parseSkipRange(raw json.RawMessage, source string) *catalog.SkipRange {
	var pair []json.RawMessage
	if json.Unmarshal(raw, &pair) == nil && len(pair) == 2 {
		return catalog.SecondsRange(skipNumber(pair[0]), skipNumber(pair[1]), source)
	}
	var v struct {
		Start json.RawMessage `json:"start"`
		End   json.RawMessage `json:"end"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return catalog.SecondsRange(skipNumber(v.Start), skipNumber(v.End), source)
}

func (f skipFields) ranges() catalog.SkipRanges {
	r := catalog.SkipRanges{Intro: parseSkipRange(f.Intro, "provider"), Outro: parseSkipRange(f.Outro, "provider")}
	r = r.Fill(catalog.SkipRanges{Intro: parseSkipRange(f.IntroChapter, "provider"), Outro: parseSkipRange(f.OutroChapter, "provider")})
	r = r.Fill(catalog.SkipRanges{
		Intro: catalog.SecondsRange(skipNumber(f.IntroStart), skipNumber(f.IntroEnd), "provider"),
		Outro: catalog.SecondsRange(skipNumber(f.OutroStart), skipNumber(f.OutroEnd), "provider"),
	})
	var nested struct {
		Intro json.RawMessage `json:"intro"`
		Outro json.RawMessage `json:"outro"`
	}
	_ = json.Unmarshal(f.SkipData, &nested)
	return r.Fill(catalog.SkipRanges{Intro: parseSkipRange(nested.Intro, "provider"), Outro: parseSkipRange(nested.Outro, "provider")})
}

type cachedSkip struct {
	ranges catalog.SkipRanges
	until  time.Time
}
type SkipClient struct {
	mu    sync.Mutex
	cache map[string]cachedSkip
	URL   string
}

// Enrich only the winning stream after the player reports its duration.
// Network work is optional and outside playback's critical path.
func (s *Service) SkipTimes(ctx context.Context, _, raw string, stream *catalog.Stream, duration int64) catalog.SkipRanges {
	ranges := stream.SkipRanges.ForDuration(duration)
	id, audio, ep, err := parseID(raw, 3)
	if err != nil || duration <= 0 {
		return ranges
	}
	m, err := s.Metadata.Get(ctx, id)
	if err != nil {
		return ranges
	}
	if stream.Provider == "senshi" && m.IDMal > 0 {
		for _, provider := range s.Providers {
			p, ok := provider.(*ReferenceProvider)
			if !ok || p.Kind != "senshi" {
				continue
			}
			var episodes []struct {
				skipFields
				Episode wireID `json:"ep_id"`
			}
			lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := p.json(lookup, fmt.Sprintf("/episodes/%d", m.IDMal), nil, &episodes)
			cancel()
			if err == nil {
				for _, item := range episodes {
					if string(item.Episode) == strconv.Itoa(ep) {
						ranges = ranges.Fill(item.ranges().ForDuration(duration))
						break
					}
				}
			}
			break
		}
	}
	if stream.ChapterURL != "" && (ranges.Intro == nil || ranges.Outro == nil) {
		lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
		b, err := request(lookup, defaultHTTP, "GET", stream.ChapterURL, nil, map[string]string{"Referer": stream.Referer})
		cancel()
		if err == nil {
			ranges = ranges.Fill(chapterRanges(string(b)).ForDuration(duration))
		}
	}
	if s.SkipClient != nil && m.IDMal > 0 && (ranges.Intro == nil || ranges.Outro == nil) {
		ranges = ranges.Fill(s.SkipClient.get(ctx, m.IDMal, ep, audio, duration))
	}
	return ranges
}

func (c *SkipClient) get(ctx context.Context, malID, episode int, audio string, duration int64) catalog.SkipRanges {
	key := fmt.Sprintf("%d:%d:%s:%d", malID, episode, audio, duration)
	c.mu.Lock()
	entry, ok := c.cache[key]
	c.mu.Unlock()
	if ok && time.Now().Before(entry.until) {
		return entry.ranges.ForDuration(duration)
	}
	base := c.URL
	if base == "" {
		base = "https://api.aniskip.com/v2/skip-times"
	}
	query := url.Values{"types[]": {"op", "ed"}, "episodeLength": {strconv.FormatFloat(float64(duration)/1000, 'f', 3, 64)}}
	var out struct {
		Found   bool `json:"found"`
		Results []struct {
			Interval struct {
				Start float64 `json:"startTime"`
				End   float64 `json:"endTime"`
			} `json:"interval"`
			Type   string  `json:"skipType"`
			Length float64 `json:"episodeLength"`
		} `json:"results"`
	}
	lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
	err := getJSON(lookup, defaultHTTP, fmt.Sprintf("%s/%d/%d?%s", strings.TrimRight(base, "/"), malID, episode, query.Encode()), &out)
	cancel()
	ranges := catalog.SkipRanges{}
	ttl := time.Minute
	if err == nil {
		ttl = 10 * time.Minute
		if out.Found {
			for _, item := range out.Results {
				// Never stretch or shift crowd-sourced ranges between cuts.
				if item.Length <= 0 || math.Abs(item.Length-float64(duration)/1000) > 2 {
					continue
				}
				r := catalog.SecondsRange(item.Interval.Start, item.Interval.End, "aniskip")
				if item.Type == "op" && ranges.Intro == nil {
					ranges.Intro = r
				}
				if item.Type == "ed" && ranges.Outro == nil {
					ranges.Outro = r
				}
			}
			ranges = ranges.ForDuration(duration)
			if ranges.Intro != nil || ranges.Outro != nil {
				ttl = 24 * time.Hour
			}
		}
	}
	if ctx.Err() != nil {
		return ranges
	}
	c.mu.Lock()
	if c.cache == nil {
		c.cache = make(map[string]cachedSkip)
	}
	if len(c.cache) >= 128 {
		oldest := ""
		var until time.Time
		for k, v := range c.cache {
			if oldest == "" || v.until.Before(until) {
				oldest, until = k, v.until
			}
		}
		delete(c.cache, oldest)
	}
	c.cache[key] = cachedSkip{ranges, time.Now().Add(ttl)}
	c.mu.Unlock()
	return ranges
}

// Only explicit opening/ending WebVTT cues are skippable; unknown formats
// and generic chapters are ignored rather than guessed.
func chapterRanges(text string) catalog.SkipRanges {
	ranges := catalog.SkipRanges{}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	for _, block := range strings.Split(text, "\n\n") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "-->") || i+1 >= len(lines) {
				continue
			}
			pair := strings.SplitN(line, "-->", 2)
			end := strings.Fields(pair[1])
			if len(end) == 0 {
				continue
			}
			r := catalog.SecondsRange(chapterTime(strings.TrimSpace(pair[0])), chapterTime(end[0]), "provider")
			switch strings.ToLower(strings.TrimSpace(lines[i+1])) {
			case "intro", "opening", "op":
				if ranges.Intro == nil {
					ranges.Intro = r
				}
			case "outro", "ending", "ed", "credits":
				if ranges.Outro == nil {
					ranges.Outro = r
				}
			}
		}
	}
	return ranges
}

func chapterTime(s string) float64 {
	parts := strings.Split(s, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return math.NaN()
	}
	total := 0.0
	for i, part := range parts {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil || n < 0 || (i > 0 && n >= 60) {
			return math.NaN()
		}
		total = total*60 + n
	}
	return total
}

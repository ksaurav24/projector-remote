package anime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"projectorremote/internal/catalog"
)

type fakeProvider struct {
	name    string
	resolve func(context.Context, Media, int, string) (*catalog.Stream, error)
}

func (p fakeProvider) Name() string { return p.name }
func (p fakeProvider) Resolve(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
	return p.resolve(ctx, m, ep, audio)
}
func testMedia() Media {
	m := Media{ID: 1, IDMal: 1, Episodes: 26, Status: "FINISHED", Duration: 24}
	m.Title.English = "Cowboy Bebop"
	m.Title.Romaji = "Cowboy Bebop"
	m.StartDate.Year = 1998
	return m
}
func testMetadata(t *testing.T) *Metadata {
	t.Helper()
	md := NewMetadata("http://unused.invalid")
	md.remember([]Media{testMedia()})
	return md
}
func playable() *catalog.Stream {
	return &catalog.Stream{URL: "https://cdn.example/stream.m3u8", MimeType: "application/x-mpegURL"}
}

func TestFallbackOrderIdentityAndRecovery(t *testing.T) {
	var calls []string
	s := &Service{Metadata: testMetadata(t)}
	for _, name := range []string{"anineko", "anikoto", "reanime", "anizone", "animegg"} {
		s.Providers = append(s.Providers, fakeProvider{name, func(ctx context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
			calls = append(calls, name)
			if m.ID != 1 || ep != 3 || audio != "dub" {
				t.Fatalf("changed canonical request: %+v %d %s", m, ep, audio)
			}
			if name == "anineko" {
				return nil, errors.New("down")
			}
			return playable(), nil
		}})
	}
	stream, err := s.Resolve(context.Background(), "anime", "1:dub:3")
	if err != nil || stream.Provider != "anikoto" {
		t.Fatalf("resolve = %+v, %v", stream, err)
	}
	recovered, err := s.Recover(context.Background(), "anime", "1:dub:3", stream)
	if err != nil || recovered.Provider != "reanime" {
		t.Fatalf("recovery = %+v, %v", recovered, err)
	}
	if !reflect.DeepEqual(calls, []string{"anineko", "anikoto", "reanime"}) {
		t.Fatal(calls)
	}
}
func TestTimeoutCancellationAndInvalidStreams(t *testing.T) {
	calls := 0
	s := &Service{Metadata: testMetadata(t), Timeout: 5 * time.Millisecond, Providers: []Provider{
		fakeProvider{"slow", func(ctx context.Context, _ Media, _ int, _ string) (*catalog.Stream, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}},
		fakeProvider{"embed", func(context.Context, Media, int, string) (*catalog.Stream, error) {
			return &catalog.Stream{URL: "https://host.example/embed/1"}, nil
		}},
		fakeProvider{"good", func(context.Context, Media, int, string) (*catalog.Stream, error) { calls++; return playable(), nil }},
	}}
	stream, err := s.Resolve(context.Background(), "anime", "1:sub:1")
	if err != nil || stream.Provider != "good" {
		t.Fatalf("%+v %v", stream, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Resolve(ctx, "anime", "1:sub:1")
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancellation: %v, calls %d", err, calls)
	}
}
func TestExhaustedProviders(t *testing.T) {
	s := &Service{Metadata: testMetadata(t), Providers: []Provider{fakeProvider{"last", func(context.Context, Media, int, string) (*catalog.Stream, error) {
		t.Fatal("retried failed provider")
		return nil, nil
	}}}}
	if _, err := s.Recover(context.Background(), "anime", "1:sub:1", &catalog.Stream{Provider: "last"}); err == nil {
		t.Fatal("expected unavailable")
	}
	for _, id := range []string{"1", "1:all:2", "0:sub:1", "1:sub:0", "1:sub:2:extra"} {
		if _, err := s.Resolve(context.Background(), "anime", id); !errors.Is(err, catalog.ErrInvalidArgument) {
			t.Errorf("accepted %q", id)
		}
	}
	if _, err := s.Resolve(context.Background(), "anime", "1:sub:27"); !errors.Is(err, catalog.ErrInvalidArgument) {
		t.Errorf("accepted episode beyond AniList count: %v", err)
	}
}
func TestEpisodePagingAndAudioIDs(t *testing.T) {
	m := testMedia()
	m.Episodes = 120
	m.Status = "RELEASING"
	m.NextAiringEpisode = &struct {
		Episode int `json:"episode"`
	}{53}
	p := episodePage(m, "dub", 1)
	if len(p.Items) != 50 || p.NextPage == nil || *p.NextPage != 2 || p.Items[0].ID != "1:dub:1" {
		t.Fatalf("bad page %+v", p)
	}
	p = episodePage(m, "dub", 2)
	if len(p.Items) != 2 || p.NextPage != nil || p.Items[1].Num != 52 {
		t.Fatalf("aired count %+v", p)
	}
	m.Status = "NOT_YET_RELEASED"
	if len(episodePage(m, "sub", 1).Items) != 0 {
		t.Fatal("listed future episodes")
	}
}
func TestMetadataNormalizationAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Error("expected GraphQL POST")
		}
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Variables["id"] == float64(99) {
			json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "rate limited"}}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Media": testMedia()}})
	}))
	defer server.Close()
	s := &Service{Metadata: NewMetadata(server.URL)}
	title, err := s.Title(context.Background(), "anime", "1")
	if err != nil {
		t.Fatal(err)
	}
	if title.ID != "1" || title.SeasonID != "1:sub" || len(title.Seasons) != 1 || title.Seasons[0].Editions[1].ID != "1:dub" || title.Episodes.Items[0].ID != "1:sub:1" {
		t.Fatalf("normalization %+v", title)
	}
	if _, err := s.Metadata.Get(context.Background(), 99); err == nil {
		t.Fatal("ignored GraphQL errors")
	}
}
func TestStrictMatching(t *testing.T) {
	m := testMedia()
	if _, err := uniqueMatch(m, []candidate{{"movie", []string{"Cowboy Bebop: The Movie"}}}); err == nil {
		t.Fatal("matched wrong release")
	}
	if _, err := uniqueMatch(m, []candidate{{"one", []string{"Cowboy Bebop"}}, {"two", []string{"Cowboy Bebop"}}}); err == nil {
		t.Fatal("accepted ambiguous release")
	}
	if slug, err := uniqueMatch(m, []candidate{{"one", []string{"COWBOY: BEBOP"}}}); err != nil || slug != "one" {
		t.Fatal(slug, err)
	}
}

func TestAnivexaProvidersRegistered(t *testing.T) {
	service := New(Config{})
	want := []string{"reanime", "animegg", "anineko", "anikoto", "anizone", "anibd", "senshi", "kaa", "animedunya", "anidbapp", "animenosub", "animeonsen", "aniwaves"}
	if got := selectedProviderNames(service.Providers); !reflect.DeepEqual(got, want) {
		t.Fatalf("providers=%v want %v", got, want)
	}
}

// Opt-in network smoke check; unit tests remain deterministic and offline.
func TestLiveProviders(t *testing.T) {
	if os.Getenv("ANIME_LIVE_TEST") != "1" {
		t.Skip("set ANIME_LIVE_TEST=1 for upstream smoke checks")
	}
	s := New(Config{})
	success := 0
	for _, p := range s.Providers {
		if p.Name() == "reanime" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		stream, err := p.Resolve(ctx, testMedia(), 1, "sub")
		cancel()
		if err != nil {
			t.Logf("%s: %v", p.Name(), err)
			continue
		}
		if stream != nil && validURL(stream.URL) && stream.MimeType != "" {
			reqCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
			req, _ := http.NewRequestWithContext(reqCtx, "GET", stream.URL, nil)
			req.Header.Set("User-Agent", userAgent)
			req.Header.Set("Referer", stream.Referer)
			req.Header.Set("Range", "bytes=0-2047")
			for k, v := range stream.Headers {
				req.Header.Set(k, v)
			}
			response, fetchErr := defaultHTTP.Do(req)
			if fetchErr != nil {
				done()
				t.Logf("%s media fetch: %v", p.Name(), fetchErr)
				continue
			}
			prefix, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
			response.Body.Close()
			done()
			if response.StatusCode != 200 && response.StatusCode != 206 {
				t.Logf("%s media HTTP %d", p.Name(), response.StatusCode)
				continue
			}
			if stream.MimeType == "application/x-mpegURL" && !bytes.Contains(prefix, []byte("#EXTM3U")) {
				t.Logf("%s returned no HLS manifest", p.Name())
				continue
			}
			if stream.MimeType == "video/mp4" && !bytes.Contains(prefix, []byte("ftyp")) {
				t.Logf("%s returned no MP4 header", p.Name())
				continue
			}
			success++
			t.Logf("%s resolved and fetched %s with %d subtitles", p.Name(), stream.MimeType, len(stream.Subtitles))
		}
	}
	if success == 0 {
		t.Fatal("no live provider resolved the smoke title")
	}
}
func TestResponseSizeBound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat("x", (8<<20)+1))) }))
	defer server.Close()
	if _, err := request(context.Background(), server.Client(), "GET", server.URL, nil, nil); err == nil {
		t.Fatal("accepted oversized upstream")
	}
}

func TestLiveMetadata(t *testing.T) {
	if os.Getenv("ANIME_LIVE_TEST") != "1" {
		t.Skip("set ANIME_LIVE_TEST=1 for upstream smoke checks")
	}
	md := NewMetadata("https://graphql.anilist.co")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h, err := md.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Rows) == 0 || len(h.Slider) == 0 {
		t.Fatal("empty home")
	}
	found, err := md.Search(ctx, "Cowboy Bebop")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("empty search")
	}
	m, err := md.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != 1 || m.Episodes != 26 {
		t.Fatalf("unexpected metadata %+v", m)
	}
}

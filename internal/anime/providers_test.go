package anime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAniNekoChoosesRequestedAudioAndResolvesEmbed(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/browser":
			fmt.Fprint(w, `<a class="nv-anime-thumb" href="/watch/bebop"><h3>Cowboy Bebop</h3></a>`)
		case "/watch/bebop/ep-3":
			fmt.Fprintf(w, `<div class="nv-server-grid" data-id="sub"><button data-video="%s/sub"></button></div><div class="nv-server-grid" data-id="dub"><button data-video="%s/dub"></button></div>`, server.URL, server.URL)
		case "/sub":
			t.Error("requested sub while playing dub")
			fmt.Fprint(w, `<html>embed only</html>`)
		case "/dub":
			if r.Header.Get("Referer") != server.URL+"/" {
				t.Error("missing embed referer")
			}
			fmt.Fprint(w, `const src = "https://cdn.example/master.m3u8?a=1&amp;b=2"`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := &Scraper{Kind: "anineko", Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 3, "dub")
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://cdn.example/master.m3u8?a=1&b=2" || stream.Referer != server.URL+"/" {
		t.Fatalf("%+v", stream)
	}
}
func TestAnimeGGUsesListedEpisodeAndMP4(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/":
			fmt.Fprint(w, `<a class="mse" href="/series/bebop"><h2>Cowboy Bebop</h2><p>Episodes: 26</p></a>`)
		case "/series/bebop":
			fmt.Fprint(w, `<li><a class="anm_det_pop" href="/bebop-episode-3"><strong>Episode 3</strong></a><span class="btn-subbed">Sub</span></li>`)
		case "/bebop-episode-3":
			fmt.Fprint(w, `<a data-toggle="tab" data-id="123" data-version="subbed">Sub</a><a data-toggle="tab" data-id="999" data-version="dubbed">Dub</a>`)
		case "/embed/123":
			fmt.Fprint(w, `var videoSources = [{file: '/video/123.mp4', label: '720p'}];`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := &Scraper{Kind: "animegg", Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 3, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != server.URL+"/video/123.mp4" || stream.MimeType != "video/mp4" {
		t.Fatalf("%+v", stream)
	}
	if _, err = p.Resolve(context.Background(), testMedia(), 3, "dub"); err == nil {
		t.Fatal("played sub as dub")
	}
}
func TestAniZoneJSONEscapesSubtitlesAndDubRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/anime":
			fmt.Fprint(w, `items: JSON.parse('[{\u0022slug\u0022:\u0022bebop\u0022,\u0022main_title\u0022:\u0022Cowboy Bebop\u0022,\u0022title_list\u0022:{\u00228\u0022:\u0022\\u30ab\\u30a6\u0022},\u0022start_year\u0022:1998}]')`)
		case "/anime/bebop/3":
			fmt.Fprint(w, `vidstackPlayer(JSON.parse('{\u0022src\u0022:\u0022https:\\/\\/cdn.example\\/master.m3u8\u0022,\u0022subtitles\u0022:[{\u0022file\u0022:\u0022https://cdn.example/en.vtt\u0022,\u0022title\u0022:\u0022English\u0022,\u0022language\u0022:\u0022en\u0022,\u0022default\u0022:true},{\u0022file\u0022:\u0022javascript:alert(1)\u0022,\u0022title\u0022:\u0022Invalid\u0022}]}'))`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := &Scraper{Kind: "anizone", Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 3, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://cdn.example/master.m3u8" || len(stream.Subtitles) != 1 || stream.Subtitles[0].Language != "en" || !stream.Subtitles[0].Default {
		t.Fatalf("%+v", stream)
	}
	if _, err = p.Resolve(context.Background(), testMedia(), 3, "dub"); err == nil {
		t.Fatal("claimed dub support")
	}
}

func TestScraperRejectsConflictingExactMatchesFromDifferentQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("keyword") {
		case "Cowboy Bebop":
			fmt.Fprint(w, `<a href="/watch/one"><h3>Cowboy Bebop</h3></a>`)
		case "Cowboy Bebop (1998)":
			fmt.Fprint(w, `<a href="/watch/two"><h3>Cowboy Bebop (1998)</h3></a>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	m := testMedia()
	m.Title.Romaji = "Cowboy Bebop (1998)"
	p := &Scraper{Kind: "anineko", Base: server.URL, Client: server.Client()}
	if _, err := p.match(context.Background(), m); err == nil || !strings.Contains(err.Error(), "unambiguous") {
		t.Fatalf("conflicting matches accepted: %v", err)
	}
}
func TestAniKotoAPIFlow(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		var results any
		switch r.URL.Path {
		case "/search":
			results = map[string]any{"data": []any{map[string]any{"slug": "wrong", "title": "Cowboy Bebop Movie"}, map[string]any{"slug": "bebop/ep-1", "title": "Cowboy Bebop"}}}
		case "/episodes/bebop":
			results = map[string]any{"episodes": []any{map[string]any{"episode_no": 3, "server_ids": "opaque+/=", "mal_id": "1"}}}
		case "/servers":
			if r.URL.Query().Get("ids") != "opaque+/=" {
				t.Error("changed opaque server IDs")
			}
			results = []any{map[string]any{"type": "sub", "link_id": "skip"}, map[string]any{"type": "dub", "link_id": 17}, map[string]any{"type": "dub", "link_id": 18}}
		case "/stream/resolve":
			if r.URL.Query().Get("slug") != "bebop" {
				t.Error("missing session slug")
			}
			if r.URL.Query().Get("id") == "17" {
				results = map[string]any{"url": "https://example.com/embed/17", "type": "embed"}
			} else {
				results = map[string]any{"url": "https://cdn.example/master.m3u8", "type": "hls", "embedUrl": "https://player.example/embed/18", "subtitles": []any{map[string]any{"src": "https://cdn.example/en.vtt", "srclang": "en", "label": "English"}}}
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "results": results})
	}))
	defer server.Close()
	p := &AniKoto{Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 3, "dub")
	if err != nil {
		t.Fatal(err)
	}
	if stream.Referer != "https://player.example/" || len(stream.Subtitles) != 1 {
		t.Fatalf("%+v", stream)
	}
	if len(seen) != 5 {
		t.Fatal(seen)
	}
}

func TestAniKotoRejectsConflictingExactMatchesAcrossTitleQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var results any
		switch r.URL.Path {
		case "/search":
			switch r.URL.Query().Get("keyword") {
			case "Cowboy Bebop":
				results = map[string]any{"data": []any{map[string]any{"slug": "first", "title": "Cowboy Bebop"}}}
			case "Cowboy Bebop (1998)":
				results = map[string]any{"data": []any{map[string]any{"slug": "second", "title": "Cowboy Bebop (1998)"}}}
			}
		default:
			t.Errorf("ambiguous match proceeded to %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "results": results})
	}))
	defer server.Close()
	m := testMedia()
	m.Title.Romaji = "Cowboy Bebop (1998)"
	p := &AniKoto{Base: server.URL, Client: server.Client()}
	if _, err := p.Resolve(context.Background(), m, 1, "sub"); err == nil || !strings.Contains(err.Error(), "exact match") {
		t.Fatalf("conflicting matches accepted: %v", err)
	}
}

func TestAniKotoSkipsWhenNoCompatibleAPIIsConfigured(t *testing.T) {
	if _, err := (&AniKoto{}).Resolve(context.Background(), testMedia(), 1, "sub"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unconfigured adapter error = %v", err)
	}
}

func TestReAnimeCanonicalRequestAndRejectsEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anime/1/3/dub" {
			t.Error(r.URL.Path)
		}
		fmt.Fprint(w, `{"audio":"dub","streams":[{"url":"https://player.example/embed/1","type":"embed"},{"url":"https://cdn.example/master.m3u8","type":"hls"}],"subtitles":[{"url":"https://cdn.example/en.vtt","label":"English","lang":"en"}]}`)
	}))
	defer server.Close()
	p := &ReAnime{Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 3, "dub")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stream.URL, "master.m3u8") || len(stream.Subtitles) != 1 {
		t.Fatalf("%+v", stream)
	}
}

func TestKaguyaSeasonPunctuationIsSignificant(t *testing.T) {
	first := Media{ID: 101921}
	first.Title.Romaji = "Kaguya-sama wa Kokurasetai: Tensaitachi no Renai Zunousen"
	second := Media{ID: 112641}
	second.Title.Romaji = "Kaguya-sama wa Kokurasetai?: Tensaitachi no Renai Zunousen"
	candidates := []candidate{
		{"season1", []string{"Kaguya-sama wa Kokurasetai: Tensai-tachi no Renai Zunousen"}},
		{"season2", []string{"Kaguya-sama wa Kokurasetai?: Tensai-tachi no Renai Zunousen"}},
	}
	for _, tt := range []struct {
		m    Media
		want string
	}{{first, "season1"}, {second, "season2"}} {
		got, err := uniqueMatch(tt.m, candidates)
		if err != nil || got != tt.want {
			t.Fatalf("release %d = %q %v", tt.m.ID, got, err)
		}
	}
}

package anime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"projectorremote/internal/catalog"
)

func TestReferenceAniBDResolvesDirectHLS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2.php":
			fmt.Fprint(w, `[{"server_name":"sub","server_data":[{"name":"1","link":"provider-ep"}]}]`)
		case "/apilink.php":
			fmt.Fprint(w, `[{"server":"primary","link":"`+"http://"+r.Host+`/player"}]`)
		case "/player":
			if r.Header.Get("Referer") != "http://"+r.Host+"/" {
				t.Errorf("player referer = %q", r.Header.Get("Referer"))
			}
			fmt.Fprint(w, `videoUrl: "https://cdn.example/master.m3u8"`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := &ReferenceProvider{Kind: "anibd", Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 1, "sub")
	if err != nil || stream.URL != "https://cdn.example/master.m3u8" || stream.MimeType != "application/x-mpegURL" {
		t.Fatalf("stream=%+v err=%v", stream, err)
	}
}

func TestReferenceAnimeDunyaResolvesSubtitlesAndRejectsDub(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/en/play/1/2" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `<script>const state = {"stream":{"source":"https://cdn.example/2.m3u8","subtitles":[{"src":"https://cdn.example/en.vtt","label":"English","srclang":"en","default":true}]}};</script>`)
	}))
	defer server.Close()
	p := &ReferenceProvider{Kind: "animedunya", Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 2, "sub")
	if err != nil || stream.URL != "https://cdn.example/2.m3u8" || len(stream.Subtitles) != 1 || !stream.Subtitles[0].Default {
		t.Fatalf("stream=%+v err=%v", stream, err)
	}
	if _, err := p.Resolve(context.Background(), testMedia(), 2, "dub"); err == nil {
		t.Fatal("accepted unsupported dub")
	}
}

func TestReferenceSenshiMatchesAudioAndReturnsTracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/episode-embeds/1/4":
			fmt.Fprint(w, `[{"status":"sub","remote_source_id":"sub-id"},{"status":"dub","remote_source_id":"dub-id"}]`)
		case "/_v1/sources":
			if r.URL.Query().Get("id") != "dub-id" {
				t.Errorf("wrong source id %q", r.URL.Query().Get("id"))
			}
			fmt.Fprint(w, `[{"source":{"src":"https://cdn.example/dub.m3u8","audio":"dub"},"tracks":[{"vtt_url":"https://cdn.example/en.vtt","label":"English"}]}]`)
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := &ReferenceProvider{Kind: "senshi", Base: server.URL, Secondary: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 4, "dub")
	if err != nil || stream == nil || stream.URL != "https://cdn.example/dub.m3u8" || len(stream.Subtitles) != 1 {
		t.Fatalf("stream=%+v err=%v", stream, err)
	}
}

func TestReferenceKAAMatchesReleaseAndResolvesHLS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fsearch":
			json.NewEncoder(w).Encode(map[string]any{"result": []any{map[string]any{"slug": "cowboy-bebop", "title": "Cowboy Bebop", "title_en": "Cowboy Bebop", "type": "tv", "year": 1998, "locales": []string{"ja-JP"}}}})
		case "/api/show/cowboy-bebop":
			fmt.Fprint(w, `{"type":"tv","locales":["ja-JP"]}`)
		case "/api/show/cowboy-bebop/episodes":
			fmt.Fprint(w, `{"result":[{"episode_number":1,"slug":"abc123","title":"Session 1"}],"pages":[]}`)
		case "/api/show/cowboy-bebop/episode/ep-1-abc123":
			fmt.Fprint(w, `{"servers":[{"src":"https://server.example/play?id=stream-id","name":"primary"}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := &ReferenceProvider{Kind: "kaa", Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 1, "sub")
	if err != nil || stream.URL != "https://hls.krussdomi.com/manifest/stream-id/master.m3u8" || stream.MimeType != "application/x-mpegURL" {
		t.Fatalf("stream=%+v err=%v", stream, err)
	}
}

func TestAnivexaAniKotoDirectSiteFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/filter":
			fmt.Fprint(w, `<a class="name d-title" href="https://anikototv.to/watch/bebop" data-jp="カウボーイビバップ">Cowboy Bebop</a>`)
		case "/watch/bebop":
			fmt.Fprint(w, `<div data-id="23"></div>`)
		case "/ajax/episode/list/23":
			json.NewEncoder(w).Encode(map[string]string{"result": `<a data-id="1" data-num="1" data-ids="srv" data-sub="1"></a>`})
		case "/ajax/server/list":
			json.NewEncoder(w).Encode(map[string]string{"result": `<div class="type" data-type="sub"><ul><li data-link-id="direct">Primary</li></ul></div>`})
		case "/ajax/server":
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]string{"url": "https://cdn.example/anikoto.m3u8"}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := &ReferenceProvider{Kind: "anikoto", Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 1, "sub")
	if err != nil || stream.URL != "https://cdn.example/anikoto.m3u8" {
		t.Fatalf("stream=%+v err=%v", stream, err)
	}
}

func TestAnivexaAniWavesAjaxEpisodeAndServerFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/filter":
			fmt.Fprint(w, `<a class="name d-title" href="/watch/cowboy-bebop-123" data-jp="カウボーイビバップ">Cowboy Bebop</a>`)
		case "/ajax/episode/list/123":
			json.NewEncoder(w).Encode(map[string]string{"status": "200", "result": `<a data-num="1" data-slug="ep-1" data-ids="id1" data-sub="1"></a>`})
		case "/ajax/server/list":
			json.NewEncoder(w).Encode(map[string]string{"status": "200", "result": `<div data-type="sub"><ul><li data-link-id="link1">Primary</li></ul></div>`})
		case "/ajax/sources":
			json.NewEncoder(w).Encode(map[string]any{"status": "200", "result": map[string]string{"url": "https://cdn.example/aniwaves.m3u8"}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := &ReferenceProvider{Kind: "aniwaves", Base: server.URL, Client: server.Client()}
	stream, err := p.Resolve(context.Background(), testMedia(), 1, "sub")
	if err != nil || stream.URL != "https://cdn.example/aniwaves.m3u8" {
		t.Fatalf("stream=%+v err=%v", stream, err)
	}
}

func TestAnivexaWatchCompatibilityRoute(t *testing.T) {
	service := &Service{Metadata: testMetadata(t), Providers: []Provider{fakeProvider{"anibd", func(context.Context, Media, int, string) (*catalog.Stream, error) {
		return playable(), nil
	}}}}
	h := CompatHandler(service)
	r := httptest.NewRequest(http.MethodGet, "/watch/anibd/1/sub/anibd-1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		AniListID int    `json:"anilistId"`
		Episode   int    `json:"episode"`
		Audio     string `json:"audio"`
		Streams   []struct {
			URL  string `json:"url"`
			Type string `json:"type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.AniListID != 1 || out.Episode != 1 || out.Audio != "sub" || len(out.Streams) != 1 || out.Streams[0].Type != "hls" {
		t.Fatalf("unexpected compatibility response: %+v", out)
	}
}

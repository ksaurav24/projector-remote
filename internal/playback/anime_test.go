package playback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"projectorremote/internal/anime"
	"projectorremote/internal/catalog"
)

type animeProvider struct {
	name string
	fail bool
}

func (p animeProvider) Name() string { return p.name }
func (p animeProvider) Resolve(_ context.Context, _ anime.Media, _ int, _ string) (*catalog.Stream, error) {
	if p.fail {
		return nil, errors.New("provider down")
	}
	return &catalog.Stream{URL: "https://cdn.example/" + p.name + ".m3u8", MimeType: "application/x-mpegURL", Headers: map[string]string{"Origin": "https://source.example"}, Subtitles: []catalog.Subtitle{{URL: "https://cdn.example/en.vtt", Language: "en", MimeType: "text/vtt"}}}, nil
}
func animeRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Media": map[string]any{"id": 1, "title": map[string]string{"english": "Cowboy Bebop"}, "episodes": 2, "status": "FINISHED", "duration": 24}}})
	}))
	t.Cleanup(upstream.Close)
	r.m.d.Catalog = &anime.Service{Metadata: anime.NewMetadata(upstream.URL), Providers: []anime.Provider{animeProvider{"first", true}, animeProvider{"second", false}, animeProvider{"third", false}, animeProvider{"fourth", false}}}
	r.connect(t, "")
	return r
}
func TestAnimeFallbackRecoveryRetainsPositionAndContract(t *testing.T) {
	r := animeRig(t)
	if err := r.m.Play(context.Background(), "anime", "1", "1:dub", "1:dub:1"); err != nil {
		t.Fatal(err)
	}
	load := r.player.last("load")
	if load["contentId"] != "1:dub:1" || load["url"] != "https://cdn.example/second.m3u8" || load["mimeType"] != "application/x-mpegURL" || load["subtitles"] == nil || load["headers"] == nil {
		t.Fatalf("load contract %+v", load)
	}
	if _, exists := load["provider"]; exists {
		t.Fatal("provider leaked to player")
	}
	r.playerSays(t, map[string]any{"type": "state", "contentId": "1:dub:1", "status": "playing", "pos": int64(20000), "dur": int64(1000000)})
	r.playerSays(t, map[string]any{"type": "error", "contentId": "1:dub:1", "http": 503, "pos": int64(21000)})
	load = r.player.last("load")
	if load["url"] != "https://cdn.example/third.m3u8" || load["startPos"] != float64(21000) || load["contentId"] != "1:dub:1" {
		t.Fatalf("lost recovery state: %+v", load)
	}
	// Unlike expiring NetMirror tokens, repeated anime CDN failures should try
	// the next provider even within the normal retry window.
	r.playerSays(t, map[string]any{"type": "error", "contentId": "1:dub:1", "http": 403, "pos": int64(21000)})
	if r.player.last("load")["url"] != "https://cdn.example/fourth.m3u8" {
		t.Fatal("retry window blocked fallback")
	}
	r.playerSays(t, map[string]any{"type": "state", "contentId": "1:dub:1", "status": "paused", "pos": int64(30000), "dur": int64(1000000)})
	r.m.Stop()
	if err := r.m.Play(context.Background(), "anime", "1", "", ""); err != nil {
		t.Fatal(err)
	}
	load = r.player.last("load")
	if load["contentId"] != "1:dub:1" || load["startPos"] != float64(30000) {
		t.Fatalf("resume changed audio or position: %+v", load)
	}
}
func TestAnimeNextStaysInAudioEditionAndFinaleStops(t *testing.T) {
	r := animeRig(t)
	if err := r.m.Play(context.Background(), "anime", "1", "1:sub", "1:sub:1"); err != nil {
		t.Fatal(err)
	}
	if r.m.Snapshot().NextLabel == "" {
		t.Fatal("missing next episode")
	}
	if err := r.m.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.m.Snapshot(); got.ContentID != "1:sub:2" || got.NextLabel != "" {
		t.Fatalf("finale switches edition: %+v", got)
	}
	r.playerSays(t, map[string]any{"type": "ended", "contentId": "1:sub:2"})
	if r.m.Snapshot().Countdown != nil {
		t.Fatal("autoplay restarts in dub")
	}
}

func TestAnimeFailedEpisodeRetryKeepsRequestedIdentity(t *testing.T) {
	r := animeRig(t)
	service := r.m.d.Catalog.(*anime.Service)
	providers := service.Providers
	service.Providers = []anime.Provider{animeProvider{"down", true}}
	if err := r.m.Play(context.Background(), "anime", "1", "1:sub", "1:sub:2"); err == nil {
		t.Fatal("expected source failure")
	}
	failed := r.m.Snapshot()
	if failed.ContentID != "1:sub:2" || failed.SeasonID != "1:sub" || failed.EpTitle != "Episode 2" {
		t.Fatalf("lost retry identity: %+v", failed)
	}
	service.Providers = providers
	if err := r.m.Play(context.Background(), failed.OTT, failed.TitleID, failed.SeasonID, failed.ContentID); err != nil {
		t.Fatal(err)
	}
	if load := r.player.last("load"); load["contentId"] != "1:sub:2" {
		t.Fatalf("wrong episode loaded: %+v", load)
	}
	// Reports from the previous episode cannot overwrite the new selection.
	r.playerSays(t, map[string]any{"type": "state", "contentId": "1:sub:1", "status": "playing", "pos": 9999})
	if got := r.m.Snapshot(); got.ContentID != "1:sub:2" || got.Pos == 9999 {
		t.Fatalf("stale state accepted: %+v", got)
	}
}

func TestSourceTimeoutHasActionableMessage(t *testing.T) {
	if got := userMessage(fmt.Errorf("provider: %w", context.DeadlineExceeded)); got != "The source providers took too long to respond. Try again." {
		t.Fatal(got)
	}
}

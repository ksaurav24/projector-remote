package anime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEpisodeMetadataUsesReleaseLocalNumbers(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("anilist_id") != "1" {
			t.Error("wrong release")
		}
		w.Write([]byte(`{"mappings":{"anilist_id":1},"episodes":{"1":{"title":{"en":"Asteroid Blues"},"episodeNumber":14,"summary":"Correct summary","overview":"Wrong TVDB summary","image":"https://wrong.example/still.jpg","length":25},"2":{"title":{"en":"Stray Dog Strut"},"episodeNumber":2,"image":"https://correct.example/still.jpg"}}}`))
	}))
	defer server.Close()
	s := Service{Metadata: testMetadata(t), EpisodeMetadata: NewEpisodeMetadata(server.URL)}
	for _, audio := range []string{"sub", "dub"} {
		p, err := s.Season(context.Background(), "anime", "1:"+audio, 1)
		if err != nil {
			t.Fatal(err)
		}
		if p.Items[0].Title != "Asteroid Blues" || p.Items[0].Desc != "Correct summary" || p.Items[0].Still == "https://wrong.example/still.jpg" || p.Items[0].ID != "1:"+audio+":1" {
			t.Fatalf("wrong mapping: %+v", p.Items[0])
		}
		if p.Items[1].Still != "https://correct.example/still.jpg" || p.Items[2].Title != "Episode 3" {
			t.Fatal(p.Items)
		}
	}
	if calls != 1 {
		t.Fatalf("cache misses: %d", calls)
	}
}
func TestEpisodeMetadataFailureLeavesEpisodesPlayable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	s := Service{Metadata: testMetadata(t), EpisodeMetadata: NewEpisodeMetadata(server.URL)}
	p, err := s.Season(context.Background(), "anime", "1:dub", 1)
	if err != nil || len(p.Items) != 26 || p.Items[0].ID != "1:dub:1" {
		t.Fatalf("%+v %v", p, err)
	}
}
func TestRelatedSeasonsExcludeSpinoffsAndPreserveSelectedRelease(t *testing.T) {
	var a, b Media
	json.Unmarshal([]byte(`{"id":1,"format":"TV","episodes":2,"startDate":{"year":2020},"title":{"english":"First"},"relations":{"edges":[{"relationType":"SEQUEL","node":{"id":2,"format":"TV","type":"ANIME"}},{"relationType":"SPIN_OFF","node":{"id":3,"format":"TV","type":"ANIME"}}]}}`), &a)
	json.Unmarshal([]byte(`{"id":2,"format":"TV","episodes":2,"startDate":{"year":2021},"title":{"english":"Second"},"relations":{"edges":[{"relationType":"PREQUEL","node":{"id":1,"format":"TV","type":"ANIME"}}]}}`), &b)
	md := NewMetadata("http://unused.invalid")
	md.remember([]Media{a, b})
	s := Service{Metadata: md}
	title, err := s.Title(context.Background(), "anime", "2")
	if err != nil {
		t.Fatal(err)
	}
	if len(title.Seasons) != 2 || title.Seasons[0].ID != "1:sub" || title.Seasons[1].Editions[1].ID != "2:dub" || title.SeasonID != "2:sub" || title.Episodes.Items[0].ID != "2:sub:1" {
		t.Fatalf("%+v", title)
	}
}

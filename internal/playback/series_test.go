package playback

import (
	"context"
	"fmt"
	"testing"

	"projectorremote/internal/catalog"
	"projectorremote/internal/netmirror"
)

// fakeCatalog serves one movie ("movie") and one series ("show"):
// Season 1 has three episodes split over two pages, Season 2 has one.
type fakeCatalog struct {
	seasonCalls []string
	resolveErr  error
	resolves    []string
}

func ptr(n int) *int { return &n }

var showPages = map[string]*netmirror.EpisodePage{
	"s1/1": {Items: []netmirror.Episode{{ID: "e1", Num: 1, Title: "One"}, {ID: "e2", Num: 2, Title: "Two"}}, NextPage: ptr(2)},
	"s1/2": {Items: []netmirror.Episode{{ID: "e3", Num: 3, Title: "Three"}}},
	"s2/1": {Items: []netmirror.Episode{{ID: "f1", Num: 1, Title: "Four"}}},
}

func (f *fakeCatalog) Title(_ context.Context, ott, id string) (*netmirror.Title, error) {
	switch id {
	case "movie":
		return &netmirror.Title{ID: "movie", Type: "movie", Title: "A Movie", Poster: "movie.jpg", Seasons: []netmirror.Season{}}, nil
	case "show":
		// NetMirror opens a series on its latest season.
		return &netmirror.Title{
			ID: "show", Type: "series", Title: "A Show", Poster: "show.jpg",
			Seasons:  []netmirror.Season{{ID: "s1", Number: 1}, {ID: "s2", Number: 2}},
			SeasonID: "s2", Episodes: showPages["s2/1"],
		}, nil
	}
	return nil, netmirror.ErrNotFound
}

func (f *fakeCatalog) Season(_ context.Context, ott, seasonID string, page int) (*netmirror.EpisodePage, error) {
	key := fmt.Sprintf("%s/%d", seasonID, page)
	f.seasonCalls = append(f.seasonCalls, key)
	if p, ok := showPages[key]; ok {
		return p, nil
	}
	return &netmirror.EpisodePage{Items: []netmirror.Episode{}}, nil
}

func (f *fakeCatalog) Resolve(_ context.Context, ott, id string) (*netmirror.Stream, error) {
	f.resolves = append(f.resolves, id)
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return &netmirror.Stream{URL: "https://cdn/" + id + ".m3u8?in=tok", Referer: "https://net52.cc"}, nil
}

func show(t *testing.T, cat *fakeCatalog) *netmirror.Title {
	t.Helper()
	title, err := cat.Title(context.Background(), "nf", "show")
	if err != nil {
		t.Fatal(err)
	}
	return title
}

func TestLocate_DefaultsToFirstEpisodeOfFirstSeason(t *testing.T) {
	cat := &fakeCatalog{}
	c, err := locate(context.Background(), cat, "nf", show(t, cat), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.current().ID != "e1" || c.label() != "S1 E1" {
		t.Fatalf("current %+v label %q", c.current(), c.label())
	}
}

func TestLocate_LoadsPagesUntilFound(t *testing.T) {
	cat := &fakeCatalog{}
	c, err := locate(context.Background(), cat, "nf", show(t, cat), "s1", "e3")
	if err != nil {
		t.Fatal(err)
	}
	if c.current().ID != "e3" || c.label() != "S1 E3" {
		t.Fatalf("current %+v label %q", c.current(), c.label())
	}
}

func TestLocate_UsesEpisodesAlreadyInTheTitle(t *testing.T) {
	cat := &fakeCatalog{}
	c, err := locate(context.Background(), cat, "nf", show(t, cat), "s2", "f1")
	if err != nil {
		t.Fatal(err)
	}
	if c.current().ID != "f1" || len(cat.seasonCalls) != 0 {
		t.Fatalf("current %+v, season calls %v", c.current(), cat.seasonCalls)
	}
}

func TestLocate_UnknownEpisodeIsError(t *testing.T) {
	cat := &fakeCatalog{}
	if _, err := locate(context.Background(), cat, "nf", show(t, cat), "s1", "nope"); err == nil {
		t.Fatal("locate succeeded for an unknown episode")
	}
}

func TestNext(t *testing.T) {
	cases := []struct {
		season, episode    string
		wantSeason, wantEp string
		wantLabel          string
		wantOK             bool
	}{
		{"s1", "e1", "s1", "e2", "S1 E2", true}, // same page
		{"s1", "e2", "s1", "e3", "S1 E3", true}, // next page
		{"s1", "e3", "s2", "f1", "S2 E1", true}, // next season
		{"s2", "f1", "", "", "", false},         // finale
	}
	for _, tc := range cases {
		cat := &fakeCatalog{}
		c, err := locate(context.Background(), cat, "nf", show(t, cat), tc.season, tc.episode)
		if err != nil {
			t.Fatal(err)
		}
		season, ep, ok, err := c.next(context.Background(), cat, "nf")
		if err != nil || ok != tc.wantOK || season.ID != tc.wantSeason || ep.ID != tc.wantEp {
			t.Errorf("%s: next = %q %q %v %v, want %q %q %v", tc.episode, season.ID, ep.ID, ok, err, tc.wantSeason, tc.wantEp, tc.wantOK)
		}
		if ok && epLabel(season.Number, ep.Num) != tc.wantLabel {
			t.Errorf("%s: next label = %q, want %q", tc.episode, epLabel(season.Number, ep.Num), tc.wantLabel)
		}
	}
}

func TestAnimeNextSeasonPreservesAudio(t *testing.T) {
	cat := &fakeCatalog{}
	title := &netmirror.Title{ID: "anime", SeasonID: "s1", Seasons: []netmirror.Season{
		{ID: "sub1", Number: 1, Editions: []catalog.Edition{{ID: "sub1", Code: "sub"}, {ID: "s1", Code: "dub"}}},
		{ID: "sub2", Number: 2, Editions: []catalog.Edition{{ID: "sub2", Code: "sub"}, {ID: "s2", Code: "dub"}}},
	}, Episodes: showPages["s1/1"]}
	c, err := locate(context.Background(), cat, "anime", title, "s1", "e3")
	if err != nil {
		t.Fatal(err)
	}
	season, ep, ok, err := c.next(context.Background(), cat, "anime")
	if err != nil || !ok || season.ID != "s2" || ep.ID != "f1" {
		t.Fatalf("%+v %+v %v %v", season, ep, ok, err)
	}
	title.SeasonID = "s2"
	title.Episodes = showPages["s2/1"]
	c, err = locate(context.Background(), cat, "anime", title, "", "")
	if err != nil || c.season().ID != "s2" {
		t.Fatalf("later release default: %+v %v", c, err)
	}
}

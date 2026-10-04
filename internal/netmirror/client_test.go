package netmirror

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fixtureFor maps an upstream request to a response captured from the
// real API on 2026-09-28 (see testdata/). Unknown titles fall through to
// post_unknown_nf.json, which is what the real API returns for them.
func fixtureFor(r *http.Request) string {
	q := r.URL.Query()
	switch r.URL.Path {
	case "/newtv/main.php":
		return "home_" + r.Header.Get("ott") + ".json"
	case "/newtv/search.php":
		if q.Get("s") == "stranger" {
			return "search_hit_nf.json"
		}
		return "search_miss_nf.json"
	case "/newtv/post.php":
		switch q.Get("id") {
		case "82813021":
			return "post_movie_nf.json"
		case "81762865":
			return "post_series_nf.json"
		case "70136120":
			return "post_series_multi_nf.json"
		case "0M1G8W35R6LZYZ25OYBKKX3PBF":
			return "post_movie_pv.json"
		case "0O0Y3RM5NIGN9SMVL6098UHNBR":
			return "post_series_pv.json"
		case "1271643838":
			return "post_series_hs.json"
		}
		return "post_unknown_nf.json"
	case "/newtv/episodes.php":
		switch q.Get("id") + "/" + q.Get("page") {
		case "81762866/1":
			return "episodes_nf.json"
		case "70242419/2":
			return "episodes_page2_nf.json"
		case "70242419/3":
			return "episodes_page3_nf.json"
		case "70023522/1":
			return "episodes_s1_nf.json"
		}
	case "/newtv/player.php":
		return "player_notoken_nf.json"
	case "/newtv/otp.php":
		switch r.Header.Get("otp") {
		case "":
			return "otp_prompt.json"
		case "111111":
			return "otp_ok.json"
		}
		return "otp_bad.json"
	}
	return ""
}

type fakeUpstream struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.reqs = append(f.reqs, r)
		f.mu.Unlock()

		// A real token makes player.php bake it into video_link, the
		// shape observed from the app's own traffic.
		if r.URL.Path == "/newtv/player.php" && r.Header.Get("usertoken") != "" {
			id := r.URL.Query().Get("id")
			fmt.Fprintf(w, `{"status":"ok","video_link":"https://tv.imgcdn.kim/newtv/hls/nf/%s.m3u8?in=%s::t","referer":"https://net52.cc","title":"T","ep":"2026","ep_title":"MOVIE"}`, id, r.Header.Get("usertoken"))
			return
		}
		name := fixtureFor(r)
		if name == "" {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Errorf("fixture %s: %v", name, err)
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeUpstream) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func newTestClient(t *testing.T, token string) (*Client, *fakeUpstream) {
	t.Helper()
	up := newFakeUpstream(t)
	c := New(staticToken(token))
	c.BaseURL = up.URL + "/newtv/"
	return c, up
}

func TestValidOTT(t *testing.T) {
	for _, s := range []string{"nf", "pv", "hs"} {
		if !ValidOTT(s) {
			t.Errorf("ValidOTT(%q) = false", s)
		}
	}
	for _, s := range []string{"", "NF", "dp", "nf "} {
		if ValidOTT(s) {
			t.Errorf("ValidOTT(%q) = true", s)
		}
	}
}

func TestHome_SendsAppHeaders(t *testing.T) {
	c, up := newTestClient(t, "")
	if _, err := c.Home(context.Background(), "pv"); err != nil {
		t.Fatal(err)
	}
	r := up.last()
	if got := r.Header.Get("x-requested-with"); got != "NetmirrorNewTV v1.0" {
		t.Errorf("x-requested-with = %q", got)
	}
	if got := r.Header.Get("ott"); got != "pv" {
		t.Errorf("ott header = %q", got)
	}
	if got := r.Header.Get("User-Agent"); !strings.HasSuffix(got, " /OS.GatuNewTV v1.0") {
		t.Errorf("User-Agent = %q", got)
	}
}

func TestHome_NetflixRowsUseLayoutTemplates(t *testing.T) {
	c, _ := newTestClient(t, "")
	h, err := c.Home(context.Background(), "nf")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Rows) != 27 {
		t.Fatalf("rows = %d, want 27", len(h.Rows))
	}
	first := h.Rows[0]
	if first.Title != "Exciting Movies" || first.Layout != "h" || len(first.Items) != 20 {
		t.Errorf("first row = %q %q %d items", first.Title, first.Layout, len(first.Items))
	}
	if got, want := first.Items[0], (Poster{ID: "81768525", Poster: "https://imgcdn.kim/poster/341/81768525.jpg"}); got != want {
		t.Errorf("first item = %+v, want %+v", got, want)
	}
	top := h.Rows[2]
	if top.Layout != "v" || !strings.HasPrefix(top.Items[0].Poster, "https://imgcdn.kim/poster/v/") {
		t.Errorf("top-10 row = layout %q poster %q", top.Layout, top.Items[0].Poster)
	}
}

func TestHome_HotstarTallRowsUseItsOwnTemplate(t *testing.T) {
	c, _ := newTestClient(t, "")
	h, err := c.Home(context.Background(), "hs")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := h.Rows[0].Items[0].Poster, "https://imgcdn.kim/hs/v/166/1260016021.jpg"; got != want {
		t.Errorf("poster = %q, want %q", got, want)
	}
}

func TestHome_Slider(t *testing.T) {
	c, _ := newTestClient(t, "")
	h, err := c.Home(context.Background(), "nf")
	if err != nil {
		t.Fatal(err)
	}
	s := h.Slider[0]
	if s.ID != "82813021" || s.Rating != "A 18+" || !strings.HasPrefix(s.Img, "https://") ||
		!strings.HasPrefix(s.Logo, "https://") || !strings.HasPrefix(s.Desc, "Driven by personal tragedy") {
		t.Errorf("slide = %+v", s)
	}
}

func TestSearch_ReturnsTitledPosters(t *testing.T) {
	c, up := newTestClient(t, "")
	items, err := c.Search(context.Background(), "nf", "stranger")
	if err != nil {
		t.Fatal(err)
	}
	if up.last().URL.Query().Get("s") != "stranger" {
		t.Errorf("query = %q", up.last().URL.RawQuery)
	}
	want := SearchItem{ID: "80187302", Title: "Stranger", Poster: "https://imgcdn.kim/poster/341/80187302.jpg"}
	if len(items) == 0 || items[0] != want {
		t.Fatalf("items[0] = %+v, want %+v", items, want)
	}
}

func TestSearch_NoResultIsEmptyNotError(t *testing.T) {
	c, _ := newTestClient(t, "")
	items, err := c.Search(context.Background(), "nf", "zzqq")
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want empty non-nil slice", items)
	}
}

func TestTitle_Movie(t *testing.T) {
	c, _ := newTestClient(t, "")
	m, err := c.Title(context.Background(), "nf", "82813021")
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "movie" || m.Title != "Dhurandhar The Revenge (Raw & Undekha) (Uncensored)" ||
		m.Year != "2026" || m.Runtime != "3h 52m" || m.Quality != "HD" || m.Rating != "U/A 18+ [A]" {
		t.Errorf("movie = %+v", m)
	}
	if m.Backdrop != "https://imgcdn.kim/poster/h/82813021.jpg" || m.Poster != "https://imgcdn.kim/poster/341/82813021.jpg" {
		t.Errorf("backdrop %q poster %q", m.Backdrop, m.Poster)
	}
	if m.Seasons == nil || len(m.Seasons) != 0 || m.Episodes != nil {
		t.Errorf("seasons = %#v, episodes = %#v", m.Seasons, m.Episodes)
	}
	if len(m.Langs) != 5 || m.Langs[0] != (Lang{Code: "hin", Label: "Hindi"}) {
		t.Errorf("langs = %+v", m.Langs)
	}
	if len(m.Suggest) != 12 || m.Suggest[0] != (Poster{ID: "82156122", Poster: "https://imgcdn.kim/poster/341/82156122.jpg"}) {
		t.Errorf("suggest = %+v", m.Suggest)
	}
}

func TestTitle_PrimeMovieWithoutSeasonKey(t *testing.T) {
	c, _ := newTestClient(t, "")
	m, err := c.Title(context.Background(), "pv", "0M1G8W35R6LZYZ25OYBKKX3PBF")
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "movie" || m.Title != "Obsession" || m.Episodes != nil || len(m.Seasons) != 0 {
		t.Errorf("movie = %+v", m)
	}
}

func TestTitle_NetflixSeries(t *testing.T) {
	c, _ := newTestClient(t, "")
	s, err := c.Title(context.Background(), "nf", "81762865")
	if err != nil {
		t.Fatal(err)
	}
	if s.Type != "series" || s.Title != "Shaque: Trust No One" {
		t.Errorf("series = %+v", s)
	}
	wantSeason := Season{ID: "81762866", Number: 1, Label: "Season 1", Episodes: 7}
	if len(s.Seasons) != 1 || !reflect.DeepEqual(s.Seasons[0], wantSeason) || s.SeasonID != "81762866" {
		t.Fatalf("seasons = %+v, seasonId %q", s.Seasons, s.SeasonID)
	}
	if s.Episodes == nil || len(s.Episodes.Items) != 7 || s.Episodes.NextPage != nil {
		t.Fatalf("episodes = %+v", s.Episodes)
	}
	ep := s.Episodes.Items[0]
	if ep.ID != "81783844" || ep.Num != 1 || ep.Title != "Episode 1" || ep.Runtime != "38m" ||
		ep.Still != "https://imgcdn.kim/epimg/150/81783844.jpg" || !strings.HasPrefix(ep.Desc, "After their newborn") {
		t.Errorf("episode = %+v", ep)
	}
}

func TestTitle_MultiSeasonFirstPageHasNextPage(t *testing.T) {
	c, _ := newTestClient(t, "")
	s, err := c.Title(context.Background(), "nf", "70136120")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Seasons) != 9 || s.Seasons[8].Number != 9 || s.Seasons[8].Episodes != 23 || s.SeasonID != "70242419" {
		t.Fatalf("seasons = %+v, seasonId %q", s.Seasons, s.SeasonID)
	}
	if len(s.Episodes.Items) != 10 || s.Episodes.NextPage == nil || *s.Episodes.NextPage != 2 {
		t.Fatalf("episodes = %d items, nextPage %v", len(s.Episodes.Items), s.Episodes.NextPage)
	}
}

func TestTitle_PrimeAndHotstarEpisodeRuntime(t *testing.T) {
	c, _ := newTestClient(t, "")
	pv, err := c.Title(context.Background(), "pv", "0O0Y3RM5NIGN9SMVL6098UHNBR")
	if err != nil {
		t.Fatal(err)
	}
	if ep := pv.Episodes.Items[0]; ep.Runtime != "38m" || ep.Still != "https://imgcdn.kim/pvepimg/341/0OBPABJMG6CQGU80CFDAK0EDLT.jpg" {
		t.Errorf("pv episode = %+v", ep)
	}
	hs, err := c.Title(context.Background(), "hs", "1271643838")
	if err != nil {
		t.Fatal(err)
	}
	if ep := hs.Episodes.Items[0]; ep.Runtime != "36m" || ep.Num != 1 || ep.Title != "Punishment Posting" {
		t.Errorf("hs episode = %+v", ep)
	}
}

func TestTitle_UnknownIDIsNotFound(t *testing.T) {
	c, _ := newTestClient(t, "")
	_, err := c.Title(context.Background(), "nf", "999999999999")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSeason_Pages(t *testing.T) {
	c, _ := newTestClient(t, "")
	// Title first, as every real caller does: it teaches the client
	// this OTT's episode-still template, which episodes.php omits.
	if _, err := c.Title(context.Background(), "nf", "70136120"); err != nil {
		t.Fatal(err)
	}
	p2, err := c.Season(context.Background(), "nf", "70242419", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Items) != 10 || p2.Items[0].Num != 11 || p2.NextPage == nil || *p2.NextPage != 3 {
		t.Fatalf("page 2 = %d items, first %+v, next %v", len(p2.Items), p2.Items[0], p2.NextPage)
	}
	if !strings.HasPrefix(p2.Items[0].Still, "https://imgcdn.kim/epimg/150/") {
		t.Errorf("still = %q", p2.Items[0].Still)
	}
	p3, err := c.Season(context.Background(), "nf", "70242419", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(p3.Items) != 3 || p3.NextPage != nil {
		t.Fatalf("page 3 = %d items, next %v", len(p3.Items), p3.NextPage)
	}
}

func TestResolve_WithoutTokenIsErrNoToken(t *testing.T) {
	c, _ := newTestClient(t, "")
	_, err := c.Resolve(context.Background(), "nf", "82813021")
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
}

func TestResolve_SendsTokenAndReturnsTokenizedLink(t *testing.T) {
	c, up := newTestClient(t, "aa::bb::1790000000::ni")
	s, err := c.Resolve(context.Background(), "nf", "82813021")
	if err != nil {
		t.Fatal(err)
	}
	if got := up.last().Header.Get("usertoken"); got != "aa::bb::1790000000::ni" {
		t.Errorf("usertoken header = %q", got)
	}
	if s.URL != "https://tv.imgcdn.kim/newtv/hls/nf/82813021.m3u8?in=aa::bb::1790000000::ni::t" || s.Referer != "https://net52.cc" {
		t.Errorf("stream = %+v", s)
	}
}

func TestLoginImage(t *testing.T) {
	c, up := newTestClient(t, "")
	img, err := c.LoginImage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if img != "https://tv.imgcdn.kim/otp2.jpg" {
		t.Errorf("img = %q", img)
	}
	if got := up.last().Header.Get("x-requested-with"); got != "NetmirrorNewTV v1.0" {
		t.Errorf("x-requested-with = %q", got)
	}
}

func TestLogin_SendsOTPHeaderAndReturnsToken(t *testing.T) {
	c, up := newTestClient(t, "")
	tok, err := c.Login(context.Background(), "111111")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "4a712e8fc016dc1cdfd9b74cf7f6846e::b4c231765d8e731782cf663fd1fbe777::1790701931::ni" {
		t.Errorf("token = %q", tok)
	}
	if got := up.last().Header.Get("otp"); got != "111111" {
		t.Errorf("otp header = %q", got)
	}
}

func TestLogin_WrongOTPIsErrBadOTP(t *testing.T) {
	c, _ := newTestClient(t, "")
	if _, err := c.Login(context.Background(), "123456"); !errors.Is(err, ErrBadOTP) {
		t.Fatalf("err = %v, want ErrBadOTP", err)
	}
}

func TestUpstreamErrorStatus(t *testing.T) {
	c, _ := newTestClient(t, "")
	_, err := c.Season(context.Background(), "nf", "no-such-fixture", 1)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want a plain upstream error", err)
	}
}

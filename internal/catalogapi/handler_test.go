package catalogapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"projectorremote/internal/catalog"
	"projectorremote/internal/history"
	"projectorremote/internal/netmirror"
)

type fakeCatalog struct {
	err         error
	title       *netmirror.Title
	seasonCalls []string
	lastOTT     string
	lastQuery   string
}

func (f *fakeCatalog) Home(_ context.Context, ott string) (*netmirror.Home, error) {
	f.lastOTT = ott
	if f.err != nil {
		return nil, f.err
	}
	return &netmirror.Home{Slider: []netmirror.Slide{}, Rows: []netmirror.Row{{Title: "Row", Layout: "h", Items: []netmirror.Poster{{ID: "1", Poster: "p1"}}}}}, nil
}

func (f *fakeCatalog) Search(_ context.Context, ott, q string) ([]netmirror.SearchItem, error) {
	f.lastOTT, f.lastQuery = ott, q
	return []netmirror.SearchItem{{ID: "1", Title: "Dark", Poster: "p1"}}, f.err
}

func (f *fakeCatalog) Title(_ context.Context, ott, id string) (*netmirror.Title, error) {
	f.lastOTT = ott
	if f.err != nil {
		return nil, f.err
	}
	t := *f.title
	return &t, nil
}

func (f *fakeCatalog) LoginImage(context.Context) (string, error) {
	return "https://tv.imgcdn.kim/otp2.jpg", f.err
}

func (f *fakeCatalog) Login(_ context.Context, otp string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if otp != "111111" {
		return "", netmirror.ErrBadOTP
	}
	return "tok", nil
}

type fakeTokens struct{ saved string }

func (f *fakeTokens) Save(token string) error {
	f.saved = token
	return nil
}

func (f *fakeCatalog) Season(_ context.Context, ott, seasonID string, page int) (*netmirror.EpisodePage, error) {
	f.seasonCalls = append(f.seasonCalls, seasonID)
	if f.err != nil {
		return nil, f.err
	}
	next := page + 1
	return &netmirror.EpisodePage{Items: []netmirror.Episode{{ID: seasonID + "-e1", Num: 1}}, NextPage: &next}, nil
}

type fakeHistory struct {
	resume    *history.Resume
	positions map[string]history.Position
}

func (f *fakeHistory) Position(ott, id string) (history.Position, bool) {
	p, ok := f.positions[ott+":"+id]
	return p, ok
}

func (f *fakeHistory) Continue() []history.ContinueItem {
	return []history.ContinueItem{{OTT: "nf", TitleID: "t1", EpisodeID: "e1", Pos: 5}}
}

func (f *fakeHistory) Resume(ott, titleID string) *history.Resume { return f.resume }

func series() *netmirror.Title {
	return &netmirror.Title{
		ID: "show", Type: "series", Title: "Show",
		Seasons:  []netmirror.Season{{ID: "s1", Number: 1}, {ID: "s2", Number: 2}, {ID: "s3", Number: 3}},
		SeasonID: "s3",
		Episodes: &netmirror.EpisodePage{Items: []netmirror.Episode{{ID: "s3-e1", Num: 1}}},
	}
}

func do(t *testing.T, h http.Handler, method, target string, out any) int {
	t.Helper()
	return doBody(t, h, method, target, "", out)
}

func doBody(t *testing.T, h http.Handler, method, target, body string, out any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: body %q: %v", method, target, rec.Body.String(), err)
		}
	}
	return rec.Code
}

type errBody struct {
	Error string `json:"error"`
}

func TestHome(t *testing.T) {
	cat := &fakeCatalog{}
	var body netmirror.Home
	code := do(t, New(cat, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/home?ott=pv", &body)
	if code != 200 || cat.lastOTT != "pv" || len(body.Rows) != 1 || body.Rows[0].Items[0].ID != "1" {
		t.Fatalf("code %d ott %q body %+v", code, cat.lastOTT, body)
	}
}

func TestBadOTTIs400(t *testing.T) {
	var body errBody
	code := do(t, New(&fakeCatalog{}, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/home?ott=xx", &body)
	if code != 400 || body.Error == "" {
		t.Fatalf("code %d body %+v", code, body)
	}
}

func TestUpstreamFailureIs502(t *testing.T) {
	var body errBody
	code := do(t, New(&fakeCatalog{err: errors.New("boom")}, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/home?ott=nf", &body)
	if code != 502 || body.Error == "" {
		t.Fatalf("code %d body %+v", code, body)
	}
}

func TestInvalidCatalogArgumentIs400(t *testing.T) {
	var body errBody
	cat := &fakeCatalog{err: catalog.ErrInvalidArgument}
	code := do(t, New(cat, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/home?ott=anime", &body)
	if code != http.StatusBadRequest || body.Error == "" {
		t.Fatalf("code %d body %+v", code, body)
	}
}

func TestSearch(t *testing.T) {
	cat := &fakeCatalog{}
	h := New(cat, &fakeHistory{}, &fakeTokens{})
	if code := do(t, h, "GET", "/api/catalog/search?ott=nf&q=", &errBody{}); code != 400 {
		t.Fatalf("empty q: code %d, want 400", code)
	}
	var body struct {
		Items []netmirror.SearchItem `json:"items"`
	}
	code := do(t, h, "GET", "/api/catalog/search?ott=nf&q=dark", &body)
	if code != 200 || cat.lastQuery != "dark" || len(body.Items) != 1 || body.Items[0].Title != "Dark" {
		t.Fatalf("code %d q %q body %+v", code, cat.lastQuery, body)
	}
}

func TestTitle_NotFoundIs404(t *testing.T) {
	cat := &fakeCatalog{err: netmirror.ErrNotFound}
	if code := do(t, New(cat, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/title?ott=nf&id=x", &errBody{}); code != 404 {
		t.Fatalf("code %d, want 404", code)
	}
}

func TestTitle_MissingIDIs400(t *testing.T) {
	if code := do(t, New(&fakeCatalog{}, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/title?ott=nf", &errBody{}); code != 400 {
		t.Fatalf("code %d, want 400", code)
	}
}

type titleBody struct {
	Progress map[string]history.Position `json:"progress"`
	ID       string                      `json:"id"`
	SeasonID string                      `json:"seasonId"`
	Episodes *netmirror.EpisodePage      `json:"episodes"`
	Resume   *history.Resume             `json:"resume"`
}

func TestTitle_MovieHasNullResume(t *testing.T) {
	cat := &fakeCatalog{title: &netmirror.Title{ID: "m", Type: "movie", Seasons: []netmirror.Season{}}}
	var body titleBody
	code := do(t, New(cat, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/title?ott=nf&id=m", &body)
	if code != 200 || body.ID != "m" || body.Resume != nil || len(cat.seasonCalls) != 0 {
		t.Fatalf("code %d body %+v seasonCalls %v", code, body, cat.seasonCalls)
	}
}

func TestTitle_UnstartedSeriesOpensOnSeasonOne(t *testing.T) {
	cat := &fakeCatalog{title: series()}
	var body titleBody
	code := do(t, New(cat, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/title?ott=nf&id=show", &body)
	if code != 200 || body.SeasonID != "s1" || body.Episodes.Items[0].ID != "s1-e1" {
		t.Fatalf("code %d body %+v", code, body)
	}
	if len(cat.seasonCalls) != 1 || cat.seasonCalls[0] != "s1" {
		t.Fatalf("seasonCalls = %v", cat.seasonCalls)
	}
}

func TestTitle_StartedSeriesOpensOnResumeSeason(t *testing.T) {
	cat := &fakeCatalog{title: series()}
	hist := &fakeHistory{resume: &history.Resume{EpisodeID: "s2-e4", SeasonID: "s2", Pos: 1000, Dur: 2000}}
	var body titleBody
	code := do(t, New(cat, hist, &fakeTokens{}), "GET", "/api/catalog/title?ott=nf&id=show", &body)
	if code != 200 || body.SeasonID != "s2" || body.Resume == nil || body.Resume.EpisodeID != "s2-e4" {
		t.Fatalf("code %d body %+v", code, body)
	}
}

func TestTitle_NoExtraCallWhenSelectedSeasonIsTheTarget(t *testing.T) {
	cat := &fakeCatalog{title: series()}
	hist := &fakeHistory{resume: &history.Resume{EpisodeID: "s3-e1", SeasonID: "s3"}}
	var body titleBody
	do(t, New(cat, hist, &fakeTokens{}), "GET", "/api/catalog/title?ott=nf&id=show", &body)
	if len(cat.seasonCalls) != 0 || body.Episodes.Items[0].ID != "s3-e1" {
		t.Fatalf("seasonCalls %v body %+v", cat.seasonCalls, body)
	}
}

func TestSeason(t *testing.T) {
	cat := &fakeCatalog{}
	h := New(cat, &fakeHistory{}, &fakeTokens{})
	var page netmirror.EpisodePage
	code := do(t, h, "GET", "/api/catalog/season?ott=nf&id=s2&page=3", &page)
	if code != 200 || len(page.Items) != 1 || page.NextPage == nil || *page.NextPage != 4 {
		t.Fatalf("code %d page %+v", code, page)
	}
	if code := do(t, h, "GET", "/api/catalog/season?ott=nf&id=s2&page=0", &errBody{}); code != 400 {
		t.Fatalf("page=0: code %d, want 400", code)
	}
	if code := do(t, h, "GET", "/api/catalog/season?ott=nf&id=s2", &page); code != 200 || *page.NextPage != 2 {
		t.Fatalf("default page: code %d next %v", code, page.NextPage)
	}
}

func TestContinue(t *testing.T) {
	var body struct {
		Items []history.ContinueItem `json:"items"`
	}
	code := do(t, New(&fakeCatalog{}, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/continue", &body)
	if code != 200 || len(body.Items) != 1 || body.Items[0].TitleID != "t1" {
		t.Fatalf("code %d body %+v", code, body)
	}
}

func TestTitleAndSeason_IncludeProgressForStartedEpisodes(t *testing.T) {
	cat := &fakeCatalog{title: series()}
	hist := &fakeHistory{positions: map[string]history.Position{
		"nf:s1-e1": {Pos: 300, Dur: 1000},
		"nf:other": {Pos: 1, Dur: 2},
	}}
	h := New(cat, hist, &fakeTokens{})

	var body titleBody
	do(t, h, "GET", "/api/catalog/title?ott=nf&id=show", &body)
	if len(body.Progress) != 1 || body.Progress["s1-e1"].Pos != 300 {
		t.Fatalf("title progress = %+v", body.Progress)
	}

	var page struct {
		Progress map[string]history.Position `json:"progress"`
	}
	do(t, h, "GET", "/api/catalog/season?ott=nf&id=s1", &page)
	if len(page.Progress) != 1 || page.Progress["s1-e1"].Dur != 1000 {
		t.Fatalf("season progress = %+v", page.Progress)
	}
}

func TestTitle_MovieProgress(t *testing.T) {
	cat := &fakeCatalog{title: &netmirror.Title{ID: "m", Type: "movie", Seasons: []netmirror.Season{}}}
	hist := &fakeHistory{positions: map[string]history.Position{"nf:m": {Pos: 5, Dur: 10}}}
	var body titleBody
	do(t, New(cat, hist, &fakeTokens{}), "GET", "/api/catalog/title?ott=nf&id=m", &body)
	if body.Progress["m"].Pos != 5 {
		t.Fatalf("progress = %+v", body.Progress)
	}
}

func TestLoginImage(t *testing.T) {
	var body struct{ Image string }
	code := do(t, New(&fakeCatalog{}, &fakeHistory{}, &fakeTokens{}), "GET", "/api/catalog/login", &body)
	if code != 200 || body.Image != "https://tv.imgcdn.kim/otp2.jpg" {
		t.Fatalf("code %d, body %+v", code, body)
	}
}

func TestLogin_SavesToken(t *testing.T) {
	tokens := &fakeTokens{}
	code := doBody(t, New(&fakeCatalog{}, &fakeHistory{}, tokens), "POST", "/api/catalog/login", `{"otp":"111111"}`, nil)
	if code != http.StatusNoContent || tokens.saved != "tok" {
		t.Fatalf("code %d, saved %q", code, tokens.saved)
	}
}

func TestLogin_WrongOTP(t *testing.T) {
	tokens := &fakeTokens{}
	var body errBody
	code := doBody(t, New(&fakeCatalog{}, &fakeHistory{}, tokens), "POST", "/api/catalog/login", `{"otp":"000000"}`, &body)
	if code != http.StatusUnauthorized || body.Error == "" || tokens.saved != "" {
		t.Fatalf("code %d, body %+v, saved %q", code, body, tokens.saved)
	}
}

func TestAnimeUsesExistingHTTPContract(t *testing.T) {
	cat := &fakeCatalog{}
	h := New(cat, &fakeHistory{}, &fakeTokens{})
	for _, path := range []string{"/api/catalog/home?ott=anime", "/api/catalog/search?ott=anime&q=Cowboy%20Bebop", "/api/catalog/season?ott=anime&id=1%3Adub&page=2"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if cat.lastOTT != "anime" || cat.lastQuery != "Cowboy Bebop" || cat.seasonCalls[0] != "1:dub" {
		t.Fatalf("lost canonical query: %+v", cat)
	}
}

func (f *fakeHistory) Bookmarks() []history.Bookmark            { return []history.Bookmark{} }
func (f *fakeHistory) SetBookmark(history.Bookmark, bool) error { return nil }

func TestBookmarkHTTPRoundTrip(t *testing.T) {
	store := history.New(t.TempDir() + "/state.json")
	h := New(&fakeCatalog{}, store, &fakeTokens{})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	path := "/api/catalog/bookmarks?ott=anime&id=1"
	if w := call("PUT", path, `{"title":"Cowboy Bebop","poster":"https://example.com/p.jpg"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := call("GET", "/api/catalog/bookmarks", "")
	var result struct {
		Items []history.Bookmark `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Items) != 1 || result.Items[0].OTT != "anime" {
		t.Fatal(w.Body.String(), err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("mutable bookmarks cached")
	}
	if w := call("DELETE", path, ""); w.Code != 200 || len(store.Bookmarks()) != 0 {
		t.Fatal(w.Body.String())
	}
	for _, bad := range []string{"/api/catalog/bookmarks?ott=unknown&id=1", "/api/catalog/bookmarks?ott=anime"} {
		if w := call("PUT", bad, `{"title":"test"}`); w.Code != 400 {
			t.Fatal("accepted bad request", w.Code)
		}
	}
	if w := call("PUT", path, `{}`); w.Code != 400 {
		t.Fatal("accepted empty title")
	}
	failed := New(&fakeCatalog{}, history.New(t.TempDir()+"/missing/state.json"), &fakeTokens{})
	w = httptest.NewRecorder()
	failed.ServeHTTP(w, httptest.NewRequest("PUT", path, strings.NewReader(`{"title":"test"}`)))
	if w.Code != 500 {
		t.Fatal("disk failure reported success", w.Code)
	}
}

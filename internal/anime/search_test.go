package anime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPartialSearchKeepsAniListIdentity(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":{"Page":{"media":[]}}}`) }))
	defer primary.Close()
	calls := 0
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query().Get("filter[text]")
		if q != "kagu" && q != "kaguy" {
			t.Errorf("unexpected query %q", q)
		}
		fmt.Fprint(w, `{"data":[{"attributes":{"canonicalTitle":"Kaguya-sama","titles":{"en":"Kaguya-sama: Love is War"}},"relationships":{"mappings":{"data":[{"id":"mal"},{"id":"al"}]}}},{"attributes":{"canonicalTitle":"Unmapped result"},"relationships":{"mappings":{"data":[{"id":"mal"}]}}},{"attributes":{"canonicalTitle":"Duplicate"},"relationships":{"mappings":{"data":[{"id":"al"}]}}}],"included":[{"id":"mal","type":"mappings","attributes":{"externalSite":"myanimelist/anime","externalId":"37999"}},{"id":"al","type":"mappings","attributes":{"externalSite":"anilist/anime","externalId":"101921"}}]}`)
	}))
	defer fallback.Close()
	s := Service{Metadata: NewMetadata(primary.URL), PrefixSearch: &PrefixSearch{URL: fallback.URL}}
	for _, q := range []string{"kagu", "kaguy", "kagu"} {
		items, err := s.Search(context.Background(), "anime", q)
		if err != nil || len(items) != 1 || items[0].ID != "101921" || items[0].Title != "Kaguya-sama: Love is War" {
			t.Fatalf("query %s: %+v %v", q, items, err)
		}
	}
	if calls != 2 {
		t.Fatalf("cache did not reuse prefix: %d", calls)
	}
}
func TestSearchFailureDoesNotPretendThereAreNoMatches(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer down.Close()
	s := Service{Metadata: NewMetadata(down.URL), PrefixSearch: &PrefixSearch{URL: down.URL}}
	if _, err := s.Search(context.Background(), "anime", "kagu"); err == nil {
		t.Fatal("outage reported as empty search")
	}
}

package anime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"projectorremote/internal/catalog"
	"testing"
	"time"
)

func TestConcurrentResolutionDoesNotWaitForBrokenProvider(t *testing.T) {
	stopped := make(chan struct{})
	s := Service{Metadata: testMetadata(t), Concurrent: true, Timeout: time.Second, Providers: []Provider{
		fakeProvider{"stuck", func(ctx context.Context, _ Media, _ int, _ string) (*catalog.Stream, error) {
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		}},
		fakeProvider{"healthy", func(_ context.Context, m Media, ep int, audio string) (*catalog.Stream, error) {
			if m.ID != 1 || ep != 19 || audio != "sub" {
				return nil, fmt.Errorf("changed requested episode")
			}
			return playable(), nil
		}},
	}}
	stream, err := s.Resolve(context.Background(), "anime", "1:sub:19")
	if err != nil || stream.Provider != "healthy" {
		t.Fatalf("%+v %v", stream, err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("loser was not cancelled")
	}
}
func TestTransientGETRetryAndMatchCache(t *testing.T) {
	searches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/anime" {
			searches++
			if searches == 1 {
				w.WriteHeader(503)
				return
			}
			fmt.Fprint(w, `items: JSON.parse('[{"slug":"bebop","main_title":"Cowboy Bebop","start_year":1998}]')`)
			return
		}
		fmt.Fprintf(w, `vidstackPlayer(JSON.parse('{"src":"https://cdn.example%s.m3u8"}'))`, r.URL.Path)
	}))
	defer server.Close()
	s := Scraper{Kind: "anizone", Base: server.URL, Client: server.Client()}
	first, err := s.Resolve(context.Background(), testMedia(), 2, "sub")
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Resolve(context.Background(), testMedia(), 3, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if searches != 2 || first.URL != "https://cdn.example/anime/bebop/2.m3u8" || next.URL != "https://cdn.example/anime/bebop/3.m3u8" {
		t.Fatalf("searches %d streams %+v %+v", searches, first, next)
	}
}

func TestProviderTimeoutIsNotReportedAsMissingSource(t *testing.T) {
	s := Service{Metadata: testMetadata(t), Concurrent: true, Providers: []Provider{fakeProvider{"slow", func(context.Context, Media, int, string) (*catalog.Stream, error) {
		return nil, fmt.Errorf("fetch: %w", context.DeadlineExceeded)
	}}}}
	_, err := s.Resolve(context.Background(), "anime", "1:sub:1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout was lost: %v", err)
	}
}

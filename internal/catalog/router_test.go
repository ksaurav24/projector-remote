package catalog

import (
	"context"
	"testing"
)

type sourceStub struct{ calls []string }

func (s *sourceStub) Home(context.Context, string) (*Home, error) {
	s.calls = append(s.calls, "home")
	return &Home{}, nil
}
func (s *sourceStub) Search(context.Context, string, string) ([]SearchItem, error) {
	s.calls = append(s.calls, "search")
	return nil, nil
}
func (s *sourceStub) Title(context.Context, string, string) (*Title, error) {
	s.calls = append(s.calls, "title")
	return &Title{}, nil
}
func (s *sourceStub) Season(context.Context, string, string, int) (*EpisodePage, error) {
	s.calls = append(s.calls, "season")
	return &EpisodePage{}, nil
}
func (s *sourceStub) Resolve(context.Context, string, string) (*Stream, error) {
	s.calls = append(s.calls, "resolve")
	return &Stream{}, nil
}
func (s *sourceStub) LoginImage(context.Context) (string, error) {
	s.calls = append(s.calls, "loginImage")
	return "image", nil
}
func (s *sourceStub) Login(context.Context, string) (string, error) {
	s.calls = append(s.calls, "login")
	return "token", nil
}

type recoveringSource struct {
	sourceStub
	failed *Stream
}

func (s *recoveringSource) Recover(_ context.Context, _, _ string, failed *Stream) (*Stream, error) {
	s.failed = failed
	s.calls = append(s.calls, "recover")
	return &Stream{Provider: "fallback"}, nil
}
func TestRouterKeepsCatalogueAndRecoveryBoundaries(t *testing.T) {
	nm := &sourceStub{}
	anime := &recoveringSource{}
	r := &Router{Default: nm, Anime: anime}
	ctx := context.Background()
	for _, ott := range []string{"nf", "pv", "hs", "anime"} {
		r.Home(ctx, ott)
		r.Search(ctx, ott, "q")
		r.Title(ctx, ott, "id")
		r.Season(ctx, ott, "id", 1)
		r.Resolve(ctx, ott, "id")
	}
	if len(nm.calls) != 15 || len(anime.calls) != 5 {
		t.Fatalf("routing: %v / %v", nm.calls, anime.calls)
	}
	r.LoginImage(ctx)
	r.Login(ctx, "123")
	if len(nm.calls) != 17 || len(anime.calls) != 5 {
		t.Fatal("login routed to anime")
	}
	failed := &Stream{Provider: "primary"}
	got, err := r.Recover(ctx, "anime", "id", failed)
	if err != nil || got.Provider != "fallback" || anime.failed != failed {
		t.Fatal("lost recovery provider")
	}
	r.Recover(ctx, "nf", "id", failed)
	if nm.calls[len(nm.calls)-1] != "resolve" {
		t.Fatal("NetMirror recovery no longer resolves a fresh token")
	}
}

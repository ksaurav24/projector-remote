// Package localmedia is the projector's side of the laptop media library:
// it finds the laptop's media server on the tailnet and presents its
// library as the "local" catalogue. The laptop does all the work (indexing,
// transcoding, subtitles); this side only relays small JSON responses and
// artwork, and hands the player the laptop's HLS URLs to fetch directly.
package localmedia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"projectorremote/internal/catalog"
	"projectorremote/internal/mediastream"
)

// ErrOffline means the laptop's media server isn't reachable.
var ErrOffline = catalog.ErrOffline

type Config struct {
	// Server is a fixed media server base URL; empty discovers one over
	// Tailscale.
	Server string
	Port   int
	// Tailscale CLI and the tailscaled socket it talks to.
	Tailscale, Socket string
	Caps              mediastream.Caps
}

type Source struct {
	cfg    Config
	client *http.Client
	// slow is for resolving: a file's first play reads its keyframes and
	// subtitles on the laptop.
	slow *http.Client
	caps string

	mu   sync.Mutex
	base string // "" while offline
	name string
	kick chan struct{}
}

func New(cfg Config) *Source {
	raw, _ := json.Marshal(cfg.Caps)
	return &Source{cfg: cfg, caps: string(raw), client: &http.Client{Timeout: 20 * time.Second},
		slow: &http.Client{Timeout: 3 * time.Minute}, kick: make(chan struct{}, 1)}
}

// Available reports whether a media server is up.
func (s *Source) Available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base != ""
}

func (s *Source) current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base
}

// Run keeps track of the media server until ctx ends: every 30 seconds
// while it's offline, and a lighter check every minute once found.
func (s *Source) Run(ctx context.Context) {
	for {
		wait := 30 * time.Second
		if base := s.current(); base != "" {
			if s.alive(ctx, base) {
				wait = time.Minute
			} else {
				s.setBase("", "")
				continue
			}
		} else if base, name := s.discover(ctx); base != "" {
			s.setBase(base, name)
			wait = time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-s.kick:
		}
	}
}

func (s *Source) setBase(base, name string) {
	s.mu.Lock()
	changed := s.base != base
	s.base, s.name = base, name
	s.mu.Unlock()
	if changed {
		if base == "" {
			log.Printf("localmedia: media server went offline")
		} else {
			log.Printf("localmedia: media server %q at %s", name, base)
		}
	}
}

// offline drops a server that stopped answering and looks again soon.
func (s *Source) offline() {
	s.setBase("", "")
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

type info struct {
	Service string `json:"service"`
	Name    string `json:"name"`
}

func (s *Source) probe(ctx context.Context, base string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/info", nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var in info
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&in) != nil || in.Service != "projector-media" {
		return "", false
	}
	return in.Name, true
}

func (s *Source) alive(ctx context.Context, base string) bool {
	_, ok := s.probe(ctx, base)
	return ok
}

// discover asks tailscaled for the online peers and checks each for a
// media server, all at once.
func (s *Source) discover(ctx context.Context) (string, string) {
	if s.cfg.Server != "" {
		name, ok := s.probe(ctx, strings.TrimRight(s.cfg.Server, "/"))
		if ok {
			return strings.TrimRight(s.cfg.Server, "/"), name
		}
		return "", ""
	}
	peers, err := s.peers(ctx)
	if err != nil {
		log.Printf("localmedia: tailscale status: %v", err)
		return "", ""
	}
	type hit struct{ base, name string }
	found := make(chan hit, len(peers))
	var wg sync.WaitGroup
	for _, ip := range peers {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			base := fmt.Sprintf("http://%s:%d", ip, s.cfg.Port)
			if name, ok := s.probe(ctx, base); ok {
				found <- hit{base, name}
			}
		}(ip)
	}
	wg.Wait()
	close(found)
	for h := range found {
		return h.base, h.name
	}
	return "", ""
}

func (s *Source) peers(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{}
	if s.cfg.Socket != "" {
		args = append(args, "--socket="+s.cfg.Socket)
	}
	out, err := exec.CommandContext(ctx, s.cfg.Tailscale, append(args, "status", "--json")...).Output()
	if err != nil {
		return nil, err
	}
	var st struct {
		Peer map[string]struct {
			Online       bool     `json:"Online"`
			OS           string   `json:"OS"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, err
	}
	var ips []string
	for _, p := range st.Peer {
		// Phones don't run the media server.
		if !p.Online || p.OS == "iOS" || p.OS == "android" {
			continue
		}
		for _, ip := range p.TailscaleIPs {
			if !strings.Contains(ip, ":") {
				ips = append(ips, ip)
			}
		}
	}
	return ips, nil
}

// get fetches a JSON path from the media server into out.
func (s *Source) get(ctx context.Context, path string, q url.Values, out any) error {
	return s.fetch(ctx, s.client, path, q, out)
}

func (s *Source) fetch(ctx context.Context, client *http.Client, path string, q url.Values, out any) error {
	base := s.current()
	if base == "" {
		return ErrOffline
	}
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			s.offline()
		}
		return fmt.Errorf("%w: %v", ErrOffline, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return catalog.ErrNotFound
	case resp.StatusCode != http.StatusOK:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("localmedia: %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Artwork the laptop serves is relayed through ArtPrefix, since phones on
// the LAN can't reach the laptop.
const ArtPrefix = "/api/local/art/"

func art(u string) string {
	if strings.HasPrefix(u, "/art/") {
		return ArtPrefix + strings.TrimPrefix(u, "/art/")
	}
	return u
}

func (s *Source) Home(ctx context.Context, _ string) (*catalog.Home, error) {
	var h catalog.Home
	if err := s.get(ctx, "/api/home", nil, &h); err != nil {
		return nil, err
	}
	for i := range h.Slider {
		h.Slider[i].Img = art(h.Slider[i].Img)
	}
	for i := range h.Rows {
		for k := range h.Rows[i].Items {
			h.Rows[i].Items[k].Poster = art(h.Rows[i].Items[k].Poster)
		}
	}
	return &h, nil
}

func (s *Source) Search(ctx context.Context, _, q string) ([]catalog.SearchItem, error) {
	var r struct {
		Items []catalog.SearchItem `json:"items"`
	}
	if err := s.get(ctx, "/api/search", url.Values{"q": {q}}, &r); err != nil {
		return nil, err
	}
	for i := range r.Items {
		r.Items[i].Poster = art(r.Items[i].Poster)
	}
	return r.Items, nil
}

func (s *Source) Title(ctx context.Context, _, id string) (*catalog.Title, error) {
	var t catalog.Title
	if err := s.get(ctx, "/api/title", url.Values{"id": {id}}, &t); err != nil {
		return nil, err
	}
	t.Poster, t.Backdrop = art(t.Poster), art(t.Backdrop)
	for i := range t.Suggest {
		t.Suggest[i].Poster = art(t.Suggest[i].Poster)
	}
	if t.Episodes != nil {
		fixStills(t.Episodes)
	}
	return &t, nil
}

func fixStills(p *catalog.EpisodePage) {
	for i := range p.Items {
		p.Items[i].Still = art(p.Items[i].Still)
	}
}

func (s *Source) Season(ctx context.Context, _, id string, page int) (*catalog.EpisodePage, error) {
	var p catalog.EpisodePage
	if err := s.get(ctx, "/api/season", url.Values{"id": {id}, "page": {fmt.Sprint(page)}}, &p); err != nil {
		return nil, err
	}
	fixStills(&p)
	return &p, nil
}

// Resolve asks the laptop to prepare a stream.
func (s *Source) Resolve(ctx context.Context, _, id string) (*catalog.Stream, error) {
	var r struct {
		URL       string             `json:"url"`
		MimeType  string             `json:"mimeType"`
		Subtitles []catalog.Subtitle `json:"subtitles"`
	}
	if err := s.fetch(ctx, s.slow, "/api/resolve", url.Values{"id": {id}, "caps": {s.caps}}, &r); err != nil {
		return nil, err
	}
	return &catalog.Stream{URL: r.URL, MimeType: r.MimeType, Subtitles: r.Subtitles, Provider: "local"}, nil
}

// ArtHandler relays artwork from the media server.
func (s *Source) ArtHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := s.current()
		if base == "" {
			http.Error(w, "media server offline", http.StatusServiceUnavailable)
			return
		}
		target, _ := url.Parse(base)
		proxy := &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
				pr.Out.URL.Path = "/art/" + strings.TrimPrefix(pr.In.URL.Path, ArtPrefix)
				pr.Out.URL.RawPath = "/art/" + strings.TrimPrefix(pr.In.URL.EscapedPath(), ArtPrefix)
				pr.Out.Host = target.Host
			},
		}
		proxy.ServeHTTP(w, r)
	})
}

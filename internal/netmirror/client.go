// Package netmirror is a client for the NetMirror TV app's backend
// (tv.imgcdn.kim/newtv). See docs/netmirror-api.md for how the API was
// reverse-engineered and what each route does.
package netmirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL = "https://tv.imgcdn.kim/newtv/"
	// StreamReferer is required on HLS playlist and segment requests.
	StreamReferer = "https://net52.cc"

	requestedWith = "NetmirrorNewTV v1.0"
	userAgent     = "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0 /OS.GatuNewTV v1.0"
)

var (
	ErrNotFound = errors.New("netmirror: not found")
	// ErrNoToken means NetMirror would not hand out a playable stream:
	// there is no usertoken, or the one sent was rejected (the server
	// answers status "otp" and leaves the token off video_link).
	ErrNoToken = errors.New("netmirror: no valid usertoken")
	// ErrBadOTP means otp.php turned down the code Login was given.
	ErrBadOTP = errors.New("netmirror: invalid OTP")
)

// TokenSource supplies the usertoken NetMirror issues after its OTP
// step. Called once per Resolve, so an implementation can read it fresh
// each time.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// ValidOTT reports whether s is one of the platform catalogs NetMirror
// serves: nf (Netflix), pv (Prime Video), hs (Hotstar).
func ValidOTT(s string) bool {
	return s == "nf" || s == "pv" || s == "hs"
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
	tokens  TokenSource

	// stills maps OTT -> episode-still URL template. episodes.php omits
	// image templates, so Season borrows the one post.php last returned
	// for that OTT (a Title call always precedes a Season call in
	// practice: the detail screen and playback both start there).
	mu     sync.Mutex
	stills map[string]string
}

func New(tokens TokenSource) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
		tokens:  tokens,
		stills:  make(map[string]string),
	}
}

func (c *Client) get(ctx context.Context, ott, route string, query url.Values, header http.Header, out any) error {
	u := c.BaseURL + route
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	for k, vs := range header {
		req.Header[k] = vs
	}
	req.Header.Set("x-requested-with", requestedWith)
	req.Header.Set("User-Agent", userAgent)
	if ott != "" {
		req.Header.Set("ott", ott)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("netmirror: %s: %w", route, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("netmirror: %s: HTTP %d", route, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("netmirror: %s: decode: %w", route, err)
	}
	return nil
}

func (c *Client) Home(ctx context.Context, ott string) (*Home, error) {
	var raw rawHome
	if err := c.get(ctx, ott, "main.php", nil, nil, &raw); err != nil {
		return nil, err
	}
	wide := string(raw.ImgCDNH)
	if wide == "" {
		wide = string(raw.ImgCDN)
	}

	h := &Home{Slider: []Slide{}, Rows: []Row{}}
	for _, s := range raw.Slider {
		h.Slider = append(h.Slider, Slide{
			ID:     string(s.ID),
			Img:    string(s.Img),
			Logo:   string(s.NameLogo),
			Desc:   string(s.Desc),
			Rating: string(s.UA),
		})
	}
	for _, p := range raw.Post {
		layout, tmpl := "h", wide
		if p.Row == "v" {
			layout, tmpl = "v", string(raw.ImgCDNV)
		}
		row := Row{Title: string(p.Cate), Layout: layout, Items: []Poster{}}
		for _, id := range strings.Split(string(p.IDs), ",") {
			if id = strings.TrimSpace(id); id != "" {
				row.Items = append(row.Items, Poster{ID: id, Poster: fillTemplate(tmpl, id)})
			}
		}
		if len(row.Items) > 0 {
			h.Rows = append(h.Rows, row)
		}
	}
	return h, nil
}

func (c *Client) Search(ctx context.Context, ott, q string) ([]SearchItem, error) {
	var raw rawSearch
	if err := c.get(ctx, ott, "search.php", url.Values{"s": {q}}, nil, &raw); err != nil {
		return nil, err
	}
	items := []SearchItem{}
	for _, r := range raw.SearchResult {
		items = append(items, SearchItem{
			ID:     string(r.ID),
			Title:  string(r.T),
			Poster: fillTemplate(string(raw.ImgCDN), string(r.ID)),
		})
	}
	return items, nil
}

func (c *Client) Title(ctx context.Context, ott, id string) (*Title, error) {
	var raw rawPost
	if err := c.get(ctx, ott, "post.php", url.Values{"id": {id}}, nil, &raw); err != nil {
		return nil, err
	}
	var typ string
	switch raw.Type {
	case "m":
		typ = "movie"
	case "t":
		typ = "series"
	default:
		// Unknown IDs still come back "ok", just with null title/type.
		return nil, fmt.Errorf("%w: %s title %s", ErrNotFound, ott, id)
	}

	stillTmpl := string(raw.EpPoster)
	c.mu.Lock()
	c.stills[ott] = stillTmpl
	c.mu.Unlock()

	mainID := string(raw.MainID)
	if mainID == "" {
		mainID = id
	}
	t := &Title{
		ID:       mainID,
		Type:     typ,
		Title:    string(raw.Title),
		Year:     string(raw.Year),
		Rating:   string(raw.UA),
		Runtime:  string(raw.Runtime),
		Quality:  string(raw.HDSD),
		Desc:     string(raw.Desc),
		Backdrop: fillTemplate(string(raw.MainPoster), mainID),
		Poster:   fillTemplate(string(raw.MorelikePoster), mainID),
		Langs:    []Lang{},
		Seasons:  []Season{},
		Suggest:  []Poster{},
	}
	for _, l := range raw.Lang {
		t.Langs = append(t.Langs, Lang{Code: string(l.S), Label: string(l.L)})
	}
	for _, s := range raw.Suggest {
		t.Suggest = append(t.Suggest, Poster{ID: string(s.ID), Poster: fillTemplate(string(raw.MorelikePoster), string(s.ID))})
	}
	if typ == "series" {
		for i, s := range raw.Season {
			label, num, eps := parseSeasonLabel(string(s.S), i)
			t.Seasons = append(t.Seasons, Season{ID: string(s.ID), Number: num, Label: label, Episodes: eps})
			if s.Selected {
				t.SeasonID = string(s.ID)
			}
		}
		if t.SeasonID == "" && len(t.Seasons) > 0 {
			t.SeasonID = t.Seasons[0].ID
		}
		t.Episodes = raw.rawEpisodes.page(stillTmpl)
	}
	return t, nil
}

// Season returns one page (1-based) of a season's episodes.
func (c *Client) Season(ctx context.Context, ott, seasonID string, page int) (*EpisodePage, error) {
	var raw rawEpisodes
	q := url.Values{"id": {seasonID}, "page": {strconv.Itoa(page)}}
	if err := c.get(ctx, ott, "episodes.php", q, nil, &raw); err != nil {
		return nil, err
	}
	c.mu.Lock()
	tmpl := c.stills[ott]
	c.mu.Unlock()
	return raw.page(tmpl), nil
}

func (r *rawEpisodes) page(stillTmpl string) *EpisodePage {
	p := &EpisodePage{Items: []Episode{}}
	for _, e := range r.Episodes {
		if e == nil {
			continue
		}
		p.Items = append(p.Items, Episode{
			ID:      string(e.ID),
			Num:     int(e.Ep),
			Title:   string(e.T),
			Desc:    string(e.EpDesc),
			Runtime: episodeRuntime(e.Info),
			Still:   fillTemplate(stillTmpl, string(e.ID)),
		})
	}
	if r.NextPageShow == 1 {
		next := int(r.NextPage)
		p.NextPage = &next
	}
	return p
}

// Resolve returns the tokenized HLS master playlist for a movie or
// episode ID. The caller must play it from this machine's public IP,
// since NetMirror signs the segment URLs to the IP that resolved them.
func (c *Client) Resolve(ctx context.Context, ott, id string) (*Stream, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoToken, err)
	}
	if token == "" {
		return nil, ErrNoToken
	}
	var raw rawPlayer
	h := http.Header{"usertoken": {token}}
	if err := c.get(ctx, ott, "player.php", url.Values{"id": {id}}, h, &raw); err != nil {
		return nil, err
	}
	link := string(raw.VideoLink)
	if raw.Status == "otp" || !strings.Contains(link, "in=") {
		return nil, ErrNoToken
	}
	ref := string(raw.Referer)
	if ref == "" {
		ref = StreamReferer
	}
	return &Stream{URL: link, Referer: ref}, nil
}

// LoginImage returns the image NetMirror's OTP screen shows. It spells
// out the current OTP (it has read "111111 is OTP for Login"), so the
// phone shows it next to the code field.
func (c *Client) LoginImage(ctx context.Context) (string, error) {
	var raw rawOTP
	if err := c.get(ctx, "", "otp.php", nil, nil, &raw); err != nil {
		return "", err
	}
	return string(raw.PubMsgImg.Img), nil
}

// Login trades an OTP for a usertoken, the step the app's OTP screen
// does. Call it from the machine that will play: the token is issued
// to the caller's public IP like the streams it unlocks.
func (c *Client) Login(ctx context.Context, otp string) (string, error) {
	var raw rawOTP
	if err := c.get(ctx, "", "otp.php", nil, http.Header{"otp": {otp}}, &raw); err != nil {
		return "", err
	}
	tok := string(raw.UserToken)
	if raw.Status != "ok" || tok == "" {
		return "", fmt.Errorf("%w: %s", ErrBadOTP, raw.ErrorMsg)
	}
	return tok, nil
}

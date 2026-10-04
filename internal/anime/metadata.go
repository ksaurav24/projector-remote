package anime

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"projectorremote/internal/catalog"
)

type Media struct {
	ID    int `json:"id"`
	IDMal int `json:"idMal"`
	Title struct {
		English string `json:"english"`
		Romaji  string `json:"romaji"`
		Native  string `json:"native"`
	} `json:"title"`
	Synonyms    []string `json:"synonyms"`
	Description string   `json:"description"`
	CoverImage  struct {
		Large string `json:"large"`
	} `json:"coverImage"`
	BannerImage  string `json:"bannerImage"`
	Format       string `json:"format"`
	Status       string `json:"status"`
	Episodes     int    `json:"episodes"`
	Duration     int    `json:"duration"`
	AverageScore int    `json:"averageScore"`
	StartDate    struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	} `json:"startDate"`
	Relations struct {
		Edges []struct {
			RelationType string `json:"relationType"`
			Node         struct {
				ID     int    `json:"id"`
				Type   string `json:"type"`
				Format string `json:"format"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"relations"`
	NextAiringEpisode *struct {
		Episode int `json:"episode"`
	} `json:"nextAiringEpisode"`
}

func (m Media) name() string {
	if m.Title.English != "" {
		return m.Title.English
	}
	return m.Title.Romaji
}
func (m Media) titles() []string {
	return append([]string{m.Title.Romaji, m.Title.English, m.Title.Native}, m.Synonyms...)
}
func (m Media) image() string {
	if m.BannerImage != "" {
		return m.BannerImage
	}
	return m.CoverImage.Large
}
func (m Media) aired() int {
	if m.Status == "NOT_YET_RELEASED" {
		return 0
	}
	if m.NextAiringEpisode != nil {
		return m.NextAiringEpisode.Episode - 1
	}
	return m.Episodes
}

var tags = regexp.MustCompile(`<[^>]*>`)

func plain(s string) string {
	return strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(s, " ")))
}

const fields = `id idMal title { english romaji native } synonyms description(asHtml:false) coverImage { large } bannerImage format status episodes duration averageScore startDate { year month day } relations { edges { relationType node { id type format } } } nextAiringEpisode { episode }`

type cachedMedia struct {
	media Media
	until time.Time
}
type Metadata struct {
	URL    string
	Client *http.Client
	mu     sync.Mutex
	cache  map[int]cachedMedia
}

func NewMetadata(rawURL string) *Metadata {
	return &Metadata{URL: rawURL, Client: defaultHTTP, cache: map[int]cachedMedia{}}
}
func (m *Metadata) query(ctx context.Context, q string, variables any, out any) error {
	b, e := request(ctx, m.Client, "POST", m.URL, map[string]any{"query": q, "variables": variables}, nil)
	if e != nil {
		return e
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if e = json.Unmarshal(b, &envelope); e != nil {
		return e
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("AniList: %s", envelope.Errors[0].Message)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("AniList returned no data")
	}
	return json.Unmarshal(envelope.Data, out)
}
func (m *Metadata) remember(items []Media) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cache) > 1000 {
		m.cache = map[int]cachedMedia{}
	}
	for _, item := range items {
		m.cache[item.ID] = cachedMedia{item, time.Now().Add(5 * time.Minute)}
	}
}
func (m *Metadata) Get(ctx context.Context, id int) (Media, error) {
	m.mu.Lock()
	cached, ok := m.cache[id]
	m.mu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return cached.media, nil
	}
	var out struct {
		Media *Media `json:"Media"`
	}
	err := m.query(ctx, `query($id:Int!){Media(id:$id,type:ANIME){`+fields+`}}`, map[string]any{"id": id}, &out)
	if err != nil {
		return Media{}, err
	}
	if out.Media == nil {
		return Media{}, catalog.ErrNotFound
	}
	m.remember([]Media{*out.Media})
	return *out.Media, nil
}
func (m *Metadata) Search(ctx context.Context, q string) ([]catalog.SearchItem, error) {
	var out struct {
		Page struct {
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	err := m.query(ctx, `query($q:String!){Page(page:1,perPage:30){media(search:$q,type:ANIME,isAdult:false,sort:SEARCH_MATCH){`+fields+`}}}`, map[string]any{"q": q}, &out)
	if err != nil {
		return nil, err
	}
	m.remember(out.Page.Media)
	items := []catalog.SearchItem{}
	for _, v := range out.Page.Media {
		items = append(items, catalog.SearchItem{ID: strconv.Itoa(v.ID), Title: v.name(), Poster: v.CoverImage.Large})
	}
	return items, nil
}
func (m *Metadata) Home(ctx context.Context) (*catalog.Home, error) {
	var out struct {
		Trending struct {
			Media []Media `json:"media"`
		} `json:"trending"`
		Popular struct {
			Media []Media `json:"media"`
		} `json:"popular"`
		Airing struct {
			Media []Media `json:"media"`
		} `json:"airing"`
	}
	q := `query {trending:Page(perPage:18){media(type:ANIME,isAdult:false,sort:TRENDING_DESC){` + fields + `}} popular:Page(perPage:18){media(type:ANIME,isAdult:false,sort:POPULARITY_DESC){` + fields + `}} airing:Page(perPage:18){media(type:ANIME,isAdult:false,status:RELEASING,sort:POPULARITY_DESC){` + fields + `}}}`
	if e := m.query(ctx, q, nil, &out); e != nil {
		return nil, e
	}
	h := &catalog.Home{Slider: []catalog.Slide{}, Rows: []catalog.Row{}}
	for i, row := range []struct {
		name  string
		items []Media
	}{{"Trending anime", out.Trending.Media}, {"Airing now", out.Airing.Media}, {"All-time favourites", out.Popular.Media}} {
		m.remember(row.items)
		r := catalog.Row{Title: row.name, Layout: "v", Items: []catalog.Poster{}}
		for j, v := range row.items {
			r.Items = append(r.Items, catalog.Poster{ID: strconv.Itoa(v.ID), Poster: v.CoverImage.Large, Title: v.name()})
			if i == 0 && j < 5 {
				h.Slider = append(h.Slider, catalog.Slide{ID: strconv.Itoa(v.ID), Title: v.name(), Img: v.image(), Desc: plain(v.Description), Rating: score(v.AverageScore)})
			}
		}
		h.Rows = append(h.Rows, r)
	}
	return h, nil
}
func score(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%.1f / 10", float64(n)/10)
}

// Releases follows only direct television/ONA sequels and prequels. Alternate
// adaptations, films, specials and spin-offs are separate catalogue titles.
func (m *Metadata) Releases(ctx context.Context, current Media) []Media {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	items := []Media{current}
	seen := map[int]bool{current.ID: true}
	serial := func(f string) bool { return f == "TV" || f == "TV_SHORT" || f == "ONA" }
	if !serial(current.Format) {
		return items
	}
	for i := 0; i < len(items) && len(items) < 20; i++ {
		for _, edge := range items[i].Relations.Edges {
			if (edge.RelationType != "PREQUEL" && edge.RelationType != "SEQUEL") || edge.Node.Type != "ANIME" || !serial(edge.Node.Format) || seen[edge.Node.ID] {
				continue
			}
			seen[edge.Node.ID] = true
			if next, err := m.Get(ctx, edge.Node.ID); err == nil {
				items = append(items, next)
			}
			if len(items) >= 20 || ctx.Err() != nil {
				break
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].StartDate, items[j].StartDate
		if a.Year == 0 {
			return false
		}
		if b.Year == 0 {
			return true
		}
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		if a.Month != b.Month {
			return a.Month < b.Month
		}
		if a.Day != b.Day {
			return a.Day < b.Day
		}
		return items[i].ID < items[j].ID
	})
	return items
}

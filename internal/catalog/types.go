package catalog

import "errors"

// Provider-neutral shapes shared by the HTTP catalogue and playback manager.
var (
	ErrNotFound        = errors.New("catalog: not found")
	ErrInvalidArgument = errors.New("catalog: invalid argument")
)

type Home struct {
	Slider []Slide `json:"slider"`
	Rows   []Row   `json:"rows"`
}

type Slide struct {
	ID     string `json:"id"`
	Img    string `json:"img"`
	Logo   string `json:"logo"`
	Desc   string `json:"desc"`
	Rating string `json:"rating"`
	Title  string `json:"title,omitempty"`
}

type Row struct {
	Title string `json:"title"`
	// Layout is "h" (wide 16:9 cards) or "v" (tall posters, used by the
	// Top 10 rows).
	Layout string   `json:"layout"`
	Items  []Poster `json:"items"`
}

type Poster struct {
	Title  string `json:"title,omitempty"`
	ID     string `json:"id"`
	Poster string `json:"poster"`
}

type SearchItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Poster string `json:"poster"`
}

type Title struct {
	ID string `json:"id"`
	// Type is "movie" or "series".
	Type     string `json:"type"`
	Title    string `json:"title"`
	Year     string `json:"year"`
	Rating   string `json:"rating"`
	Runtime  string `json:"runtime"`
	Quality  string `json:"quality"`
	Desc     string `json:"desc"`
	Backdrop string `json:"backdrop"`
	// Poster is the title's own card image, for places like the
	// continue-watching row that show a title without its backdrop.
	Poster  string   `json:"poster"`
	Langs   []Lang   `json:"langs"`
	Seasons []Season `json:"seasons"`
	// SeasonID names the season Episodes belongs to. Empty for movies.
	SeasonID string       `json:"seasonId"`
	Episodes *EpisodePage `json:"episodes"`
	Suggest  []Poster     `json:"suggest"`
}

type Lang struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type Edition struct {
	ID    string `json:"id"`
	Code  string `json:"code"`
	Label string `json:"label"`
}

type Season struct {
	Editions []Edition `json:"editions,omitempty"`
	ID       string    `json:"id"`
	Number   int       `json:"number"`
	Label    string    `json:"label"`
	Episodes int       `json:"episodes"`
}

type Episode struct {
	ID      string `json:"id"`
	Num     int    `json:"num"`
	Title   string `json:"title"`
	Desc    string `json:"desc"`
	Runtime string `json:"runtime"`
	Still   string `json:"still"`
}

type EpisodePage struct {
	Items []Episode `json:"items"`
	// NextPage is the page number to request next, or nil when this is
	// the season's last page.
	NextPage *int `json:"nextPage"`
}

// Stream describes playable media and its transport requirements. Only the
// server knows which provider resolved it; clients receive media fields.
type Stream struct {
	URL       string
	Referer   string
	Headers   map[string]string
	MimeType  string
	Subtitles []Subtitle
	// Provider is server-side routing metadata; clients never select providers.
	Provider        string
	FailedProviders []string
}

type Subtitle struct {
	URL      string `json:"url"`
	Label    string `json:"label"`
	Language string `json:"language"`
	MimeType string `json:"mimeType"`
	Default  bool   `json:"default"`
}

func ValidOTT(ott string) bool {
	return ott == "nf" || ott == "pv" || ott == "hs" || ott == "anime"
}

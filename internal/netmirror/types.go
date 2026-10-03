package netmirror

// The cleaned-up shapes this package hands out. NetMirror's own JSON
// (see wire.go) is stringly typed and differs per OTT; everything here
// is normalized so callers never see those quirks.

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
}

type Row struct {
	Title string `json:"title"`
	// Layout is "h" (wide 16:9 cards) or "v" (tall posters, used by the
	// Top 10 rows).
	Layout string   `json:"layout"`
	Items  []Poster `json:"items"`
}

type Poster struct {
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

type Season struct {
	ID       string `json:"id"`
	Number   int    `json:"number"`
	Label    string `json:"label"`
	Episodes int    `json:"episodes"`
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

// Stream is a resolved, tokenized HLS master playlist. It must be
// fetched from the same public IP that resolved it (see
// docs/netmirror-api.md §4), with Referer set.
type Stream struct {
	URL     string
	Referer string
}

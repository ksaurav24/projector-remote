package medialib

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// NFO is the useful part of a Kodi/Jellyfin .nfo sidecar (tvshow.nfo,
// movie.nfo, or <video>.nfo for an episode).
type NFO struct {
	Title   string   `json:"title,omitempty"`
	Plot    string   `json:"plot,omitempty"`
	Year    int      `json:"year,omitempty"`
	Rating  float64  `json:"rating,omitempty"`
	Runtime int      `json:"runtime,omitempty"`
	TMDBID  int      `json:"tmdbId,omitempty"`
	Genres  []string `json:"genres,omitempty"`
}

type nfoXML struct {
	Title     string   `xml:"title"`
	Plot      string   `xml:"plot"`
	Outline   string   `xml:"outline"`
	Year      string   `xml:"year"`
	Premiered string   `xml:"premiered"`
	Aired     string   `xml:"aired"`
	Rating    string   `xml:"rating"`
	Runtime   string   `xml:"runtime"`
	TMDBID    string   `xml:"tmdbid"`
	Genres    []string `xml:"genre"`
	UniqueIDs []struct {
		Type  string `xml:"type,attr"`
		Value string `xml:",chardata"`
	} `xml:"uniqueid"`
	Ratings struct {
		Rating []struct {
			Value string `xml:"value"`
		} `xml:"rating"`
	} `xml:"ratings"`
}

// Some .nfo files are just a URL ("https://www.themoviedb.org/movie/603").
var reTMDBURL = regexp.MustCompile(`themoviedb\.org/(?:movie|tv)/(\d+)`)

// ReadNFO parses an .nfo file.
func ReadNFO(path string) (*NFO, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n := &NFO{}
	if m := reTMDBURL.FindSubmatch(raw); m != nil {
		n.TMDBID, _ = strconv.Atoi(string(m[1]))
	}
	start := bytes.IndexByte(raw, '<')
	if start < 0 {
		return n, nil
	}
	var x nfoXML
	dec := xml.NewDecoder(bytes.NewReader(raw[start:]))
	dec.Strict = false
	// Kodi writes UTF-8 but often declares other charsets; read as is.
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := dec.Decode(&x); err != nil {
		return n, nil
	}
	n.Title = strings.TrimSpace(x.Title)
	n.Plot = strings.TrimSpace(x.Plot)
	if n.Plot == "" {
		n.Plot = strings.TrimSpace(x.Outline)
	}
	n.Year, _ = strconv.Atoi(strings.TrimSpace(x.Year))
	for _, d := range []string{x.Premiered, x.Aired} {
		if n.Year == 0 && len(d) >= 4 {
			n.Year, _ = strconv.Atoi(d[:4])
		}
	}
	n.Rating, _ = strconv.ParseFloat(strings.TrimSpace(x.Rating), 64)
	if n.Rating == 0 && len(x.Ratings.Rating) > 0 {
		n.Rating, _ = strconv.ParseFloat(strings.TrimSpace(x.Ratings.Rating[0].Value), 64)
	}
	n.Runtime, _ = strconv.Atoi(strings.TrimSpace(x.Runtime))
	if id, err := strconv.Atoi(strings.TrimSpace(x.TMDBID)); err == nil {
		n.TMDBID = id
	}
	for _, u := range x.UniqueIDs {
		if strings.EqualFold(u.Type, "tmdb") {
			if id, err := strconv.Atoi(strings.TrimSpace(u.Value)); err == nil {
				n.TMDBID = id
			}
		}
	}
	for _, g := range x.Genres {
		if g = strings.TrimSpace(g); g != "" {
			n.Genres = append(n.Genres, g)
		}
	}
	return n, nil
}

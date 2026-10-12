package medialib

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Catalog is an immutable snapshot of the library, grouped into shows and
// movies. A new one is built whenever the index changes.
type Catalog struct {
	Shows  []*Show
	Movies []*Movie
	// Recent lists every title, newest file first.
	Recent []Title

	shows    map[string]*Show
	movies   map[string]*Movie
	seasons  map[string]*Season
	episodes map[string]*Episode
}

// Title is a show or a movie.
type Title interface {
	TitleID() string
	DisplayName() string
	added() int64
}

type Show struct {
	ID   string
	Key  string
	Name string
	Year int
	// Dir is the show's folder (relative), or "" for loose episode files.
	Dir     string
	Seasons []*Season
	NFO     *NFO
	Art     Art
	Added   int64
}

type Season struct {
	ID       string
	Show     *Show
	Num      int
	Episodes []*Episode
	Poster   string
}

type Episode struct {
	ID     string
	Show   *Show
	Season *Season
	Nums   []int
	File   *File
	// Title is the name parsed from the file, if it carried one.
	Title string
}

type Movie struct {
	ID    string
	Key   string
	Name  string
	Year  int
	File  *File
	NFO   *NFO
	Art   Art
	Added int64
}

// Art holds sidecar image paths (relative to the library root).
type Art struct {
	Poster   string
	Backdrop string
}

func (s *Show) TitleID() string      { return s.ID }
func (s *Show) DisplayName() string  { return s.Name }
func (s *Show) added() int64         { return s.Added }
func (m *Movie) TitleID() string     { return m.ID }
func (m *Movie) DisplayName() string { return m.Name }
func (m *Movie) added() int64        { return m.Added }

// Num is the episode's first number; multi-episode files cover more.
func (e *Episode) Num() int { return e.Nums[0] }

// Label is "S1 E3" or "S1 E3-E4".
func (e *Episode) Label() string {
	l := fmt.Sprintf("S%d E%d", e.Season.Num, e.Nums[0])
	if len(e.Nums) > 1 {
		l += fmt.Sprintf("-E%d", e.Nums[len(e.Nums)-1])
	}
	return l
}

func (c *Catalog) Show(id string) *Show       { return c.shows[id] }
func (c *Catalog) Movie(id string) *Movie     { return c.movies[id] }
func (c *Catalog) Season(id string) *Season   { return c.seasons[id] }
func (c *Catalog) Episode(id string) *Episode { return c.episodes[id] }

// Playable returns the file for a movie or episode ID.
func (c *Catalog) Playable(id string) *File {
	if m := c.movies[id]; m != nil {
		return m.File
	}
	if e := c.episodes[id]; e != nil {
		return e.File
	}
	return nil
}

func buildCatalog(root string, files []*File) *Catalog {
	c := &Catalog{shows: map[string]*Show{}, movies: map[string]*Movie{}, seasons: map[string]*Season{}, episodes: map[string]*Episode{}}
	sort.Slice(files, func(i, j int) bool { return naturalLess(files[i].Rel, files[j].Rel) })

	type pendingEp struct {
		f *File
		p Parsed
	}
	showEps := map[string][]pendingEp{}
	showDirs := map[string]string{}
	for _, f := range files {
		p := ParsePath(f.Rel)
		switch p.Kind {
		case KindEpisode:
			key := normalizeKey(p.Show)
			if key == "" {
				continue
			}
			s := c.shows[key]
			if s == nil {
				s = &Show{ID: "tv-" + shortHash(key), Key: key, Name: p.Show}
				c.shows[key] = s
			}
			if p.Year != 0 && s.Year == 0 {
				s.Year = p.Year
			}
			if f.ModTime > s.Added {
				s.Added = f.ModTime
			}
			if _, ok := showDirs[key]; !ok {
				showDirs[key] = showDir(f.Rel, p)
			}
			showEps[key] = append(showEps[key], pendingEp{f, p})
		case KindMovie:
			key := normalizeKey(p.Show)
			if key == "" {
				continue
			}
			mk := key + "|" + strconv.Itoa(p.Year)
			if old := c.movies[mk]; old != nil && !betterCopy(f, old.File) {
				continue
			}
			c.movies[mk] = &Movie{ID: "mv-" + shortHash(mk), Key: key, Name: p.Show, Year: p.Year, File: f, Added: f.ModTime}
		}
	}

	for key, eps := range showEps {
		s := c.shows[key]
		s.Dir = showDirs[key]
		seasons := map[int]*Season{}
		bySlot := map[[2]int]*Episode{}
		var unnumbered []pendingEp
		for _, pe := range eps {
			num := pe.p.Season
			if num < 0 {
				num = 1
			}
			if len(pe.p.Episodes) == 0 {
				unnumbered = append(unnumbered, pe)
				continue
			}
			addEpisode(s, seasons, bySlot, num, pe.p.Episodes, pe.f, pe.p.EpTitle)
		}
		// Episodes without numbers follow the numbered ones in name order.
		for _, pe := range unnumbered {
			num := pe.p.Season
			if num < 0 {
				num = 1
			}
			next := 1
			if se := seasons[num]; se != nil {
				for _, e := range se.Episodes {
					next = max(next, e.Nums[len(e.Nums)-1]+1)
				}
			}
			addEpisode(s, seasons, bySlot, num, []int{next}, pe.f, pe.p.EpTitle)
		}
		for _, n := range sortedKeys(seasons) {
			se := seasons[n]
			sort.Slice(se.Episodes, func(i, j int) bool { return se.Episodes[i].Nums[0] < se.Episodes[j].Nums[0] })
			s.Seasons = append(s.Seasons, se)
		}
		// Specials (season 0) go last, the way every player lists them.
		if len(s.Seasons) > 1 && s.Seasons[0].Num == 0 {
			s.Seasons = append(s.Seasons[1:], s.Seasons[0])
		}
		for _, se := range s.Seasons {
			c.seasons[se.ID] = se
			for _, e := range se.Episodes {
				c.episodes[e.ID] = e
			}
		}
		if s.Dir != "" {
			s.NFO, _ = ReadNFO(filepath.Join(root, s.Dir, "tvshow.nfo"))
			s.Art = findArt(root, s.Dir, "")
			for _, se := range s.Seasons {
				se.Poster = seasonPoster(root, s.Dir, se.Num)
			}
		}
		c.Shows = append(c.Shows, s)
	}
	for _, m := range c.movies {
		dir := pathDir(m.File.Rel)
		m.NFO = m.File.NFO
		if m.NFO == nil && dir != "" {
			m.NFO, _ = ReadNFO(filepath.Join(root, dir, "movie.nfo"))
		}
		stem := strings.TrimSuffix(path.Base(m.File.Rel), path.Ext(m.File.Rel))
		m.Art = findArt(root, dir, stem)
		c.Movies = append(c.Movies, m)
	}

	// Re-key by public ID.
	shows, movies := map[string]*Show{}, map[string]*Movie{}
	for _, s := range c.Shows {
		shows[s.ID] = s
	}
	for _, m := range c.Movies {
		movies[m.ID] = m
	}
	c.shows, c.movies = shows, movies

	sort.Slice(c.Shows, func(i, j int) bool { return sortName(c.Shows[i].Name) < sortName(c.Shows[j].Name) })
	sort.Slice(c.Movies, func(i, j int) bool { return sortName(c.Movies[i].Name) < sortName(c.Movies[j].Name) })
	for _, s := range c.Shows {
		c.Recent = append(c.Recent, s)
	}
	for _, m := range c.Movies {
		c.Recent = append(c.Recent, m)
	}
	sort.SliceStable(c.Recent, func(i, j int) bool { return c.Recent[i].added() > c.Recent[j].added() })
	return c
}

func addEpisode(s *Show, seasons map[int]*Season, bySlot map[[2]int]*Episode, num int, nums []int, f *File, title string) {
	se := seasons[num]
	if se == nil {
		se = &Season{ID: s.ID + ":" + strconv.Itoa(num), Show: s, Num: num}
		seasons[num] = se
	}
	slot := [2]int{num, nums[0]}
	if old := bySlot[slot]; old != nil {
		// Two copies of one episode: keep the better one.
		if betterCopy(f, old.File) {
			old.File, old.Nums, old.Title = f, nums, title
		}
		return
	}
	e := &Episode{ID: fmt.Sprintf("%s:%d:%d", s.ID, num, nums[0]), Show: s, Season: se, Nums: nums, File: f, Title: title}
	bySlot[slot] = e
	se.Episodes = append(se.Episodes, e)
}

// betterCopy prefers higher resolution, then the larger file.
func betterCopy(a, b *File) bool {
	ha, hb := a.Probe.Video.Height, b.Probe.Video.Height
	if ha != hb {
		return ha > hb
	}
	return a.Size > b.Size
}

// showDir is the folder that holds a show (where tvshow.nfo and its
// artwork live), or "" when episodes sit loose in a shared folder.
func showDir(rel string, p Parsed) string {
	dirs := strings.Split(pathDir(rel), "/")
	if len(dirs) == 1 && dirs[0] == "" {
		return ""
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if _, ok := seasonFromDir(dirs[i]); ok {
			if i == 0 {
				return ""
			}
			return strings.Join(dirs[:i], "/")
		}
		if _, _, ok := seasonPack(dirs[i]); ok {
			if i > 0 && normalizeKey(dirs[i-1]) == normalizeKey(p.Show) {
				return strings.Join(dirs[:i], "/")
			}
			return strings.Join(dirs[:i+1], "/")
		}
	}
	if categoryDirs[normalizeKey(dirs[len(dirs)-1])] {
		return ""
	}
	return strings.Join(dirs, "/")
}

var posterNames = []string{"poster", "folder", "cover", "show", "movie", "default"}
var backdropNames = []string{"fanart", "backdrop", "background", "art", "landscape"}
var imageExts = []string{".jpg", ".jpeg", ".png", ".webp"}

// findArt looks for poster and backdrop images in dir, either generic
// ("poster.jpg") or named after the video ("Movie-poster.jpg"). Generic
// names only count in a folder that belongs to one title.
func findArt(root, dir, stem string) Art {
	var a Art
	look := func(names []string) string {
		for _, n := range names {
			for _, ext := range imageExts {
				candidates := []string{}
				if stem != "" {
					candidates = append(candidates, stem+"-"+n+ext, stem+"."+n+ext)
				}
				if stem == "" || ownsFolder(root, dir) {
					candidates = append(candidates, n+ext)
				}
				for _, c := range candidates {
					rel := path.Join(dir, c)
					if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
						return rel
					}
				}
			}
		}
		return ""
	}
	a.Poster = look(posterNames)
	a.Backdrop = look(backdropNames)
	return a
}

// ownsFolder reports whether dir holds a single video (so "poster.jpg"
// there is that video's).
func ownsFolder(root, dir string) bool {
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		return false
	}
	return onlyVideoIn(entries)
}

func seasonPoster(root, showDir string, n int) string {
	names := []string{fmt.Sprintf("season%02d-poster", n), fmt.Sprintf("season%d-poster", n), fmt.Sprintf("Season %d", n)}
	if n == 0 {
		names = append(names, "season-specials-poster")
	}
	for _, name := range names {
		for _, ext := range imageExts {
			rel := path.Join(showDir, name+ext)
			if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
				return rel
			}
		}
	}
	return ""
}

// sortName orders "The Office" under O.
func sortName(s string) string {
	l := strings.ToLower(s)
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(l, a) && len(l) > len(a) {
			return l[len(a):]
		}
	}
	return l
}

// naturalLess orders "Episode 2" before "Episode 10".
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		ca, cb := a[0], b[0]
		if isDigit(ca) && isDigit(cb) {
			na, ra := leadingDigits(a)
			nb, rb := leadingDigits(b)
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		la, lb := lower(ca), lower(cb)
		if la != lb {
			return la < lb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

func leadingDigits(s string) (int, string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	n, _ := strconv.Atoi(s[:min(i, 18)])
	return n, s[i:]
}

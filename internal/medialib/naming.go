// Package medialib indexes a folder of local video files into movies and
// series, the way people actually name them: scene releases
// ("Show.Name.S01E02.1080p.WEB-DL.x264-GRP.mkv"), anime fansubs
// ("[Group] Show - 05 (1080p).mkv"), Plex/Kodi layouts
// ("Show/Season 1/01 - Pilot.mkv") and the misspelled or oddly cased
// variants of all of them.
package medialib

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Kind is what a path turned out to be.
type Kind int

const (
	KindSkip Kind = iota
	KindMovie
	KindEpisode
)

// Parsed is what a file's path says about it, before any metadata lookup.
type Parsed struct {
	Kind Kind
	// Show is the movie or series name as it should be displayed.
	Show string
	Year int
	// Season is -1 when nothing in the path names one (the index then
	// assumes 1). 0 is Specials.
	Season int
	// Episodes holds every episode number the file covers ("S01E01E02"
	// covers two); empty when an episode file carries no number, which the
	// index fills in from the file's sort order.
	Episodes []int
	EpTitle  string
}

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".wmv": true,
	".flv": true, ".webm": true, ".ts": true, ".m2ts": true, ".mts": true, ".mpg": true,
	".mpeg": true, ".vob": true, ".ogv": true, ".3gp": true, ".divx": true, ".rmvb": true,
	".asf": true, ".f4v": true,
}

// IsVideo reports whether name has a video file extension.
func IsVideo(name string) bool { return videoExts[strings.ToLower(filepath.Ext(name))] }

// Folders whose contents are bonus material, not episodes or movies.
var extrasDirs = map[string]bool{
	"extras": true, "extra": true, "featurettes": true, "featurette": true, "behind the scenes": true,
	"deleted scenes": true, "interviews": true, "scenes": true, "shorts": true, "trailers": true,
	"trailer": true, "sample": true, "samples": true, "bonus": true, "other": true, "making of": true,
	"ncop": true, "nced": true, "menus": true, "menu": true, "cds": true, "scans": true,
}

// Top-level folders people sort a library into. They name a category,
// never a title.
var categoryDirs = map[string]bool{
	"tv": true, "tv shows": true, "tvshows": true, "shows": true, "series": true, "tv series": true,
	"movies": true, "movie": true, "films": true, "film": true, "anime": true, "cartoons": true,
	"kids": true, "documentaries": true, "documentary": true, "videos": true, "media": true,
	"downloads": true, "complete": true, "completed": true, "new": true,
}

// Release tags that end the meaningful part of a name. Everything from the
// first one on is quality, source or group noise.
var junkWords = map[string]bool{
	"2160p": true, "1080p": true, "1080i": true, "720p": true, "576p": true, "480p": true, "4k": true,
	"uhd": true, "fhd": true, "hd": true, "sd": true, "bluray": true, "blu": true, "bdrip": true,
	"brrip": true, "bdremux": true, "remux": true, "webrip": true, "webdl": true, "web": true,
	"hdtv": true, "hdrip": true, "dvdrip": true, "dvd": true, "dvdscr": true, "hdcam": true,
	"x264": true, "x265": true, "h264": true, "h265": true, "hevc": true, "avc": true, "av1": true,
	"xvid": true, "divx": true, "10bit": true, "8bit": true, "hdr": true, "hdr10": true, "dv": true,
	"dovi": true, "sdr": true, "dts": true, "ac3": true, "aac": true, "eac3": true, "ddp": true,
	"dd": true, "truehd": true, "atmos": true, "flac": true, "opus": true, "proper": true,
	"repack": true, "extended": true, "unrated": true, "uncut": true, "remastered": true,
	"multi": true, "dual": true, "subbed": true, "dubbed": true, "nf": true, "amzn": true,
	"dsnp": true, "hmax": true, "atvp": true, "hulu": true, "pcok": true, "imax": true,
	"internal": true, "limited": true, "complete": true, "batch": true, "esub": true, "esubs": true,
	"hindi": true, "english": true, "eng": true,
}

var (
	// S01E02, s1e2, S01.E02, S01 E02, S01E01E02, S01E01-E03, S01E01-03, S01E01&E02
	reSxE = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(\d{1,3})[ ._-]*e(\d{1,4})((?:[ ._-]*(?:-|&|e)[ ._-]*e?\d{1,4})*)`)
	// 1x02 (but never a resolution like 1920x1080)
	reNxN = regexp.MustCompile(`(?i)(?:^|[^0-9a-z])(\d{1,2})x(\d{1,3})(?:[^0-9]|$)`)
	// Season 1 Episode 2, Season.1.Ep.2
	reSeasonEp = regexp.MustCompile(`(?i)season[ ._-]*(\d{1,3})[ ._-]*(?:episode|ep)[ ._-]*(\d{1,4})`)
	// Episode 12, Ep12, Ep.12, E12, #12
	reEpOnly = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:episode|ep|e|#)[ ._]*(\d{1,4})(?:v\d)?(?:[^0-9]|$)`)
	// "[Group] Show - 12 (1080p)" or "Show - 12v2"
	reDashNum = regexp.MustCompile(`\s[-–]\s+(\d{1,4})(?:v\d)?(?:\s|\(|\[|$|\.)`)
	// A file named just by its number: "01", "01 - Pilot", "1. Pilot"
	reLeadingNum = regexp.MustCompile(`^(\d{1,4})(?:v\d)?(?:\s*[-–.)_]\s*|\s+|$)`)
	// Season folder names: Season 1, Season01, S01, S1, Series 2, Staffel 3,
	// Saison 1, Temporada 2, Stagione 1. Misspellings of "season" are
	// handled separately.
	reSeasonDir = regexp.MustCompile(`(?i)^(?:season|series|staffel|saison|temporada|stagione|seizoen|sezon|s)[ ._-]*0*(\d{1,3})$`)
	// A season pack folder: "Breaking.Bad.S01.1080p.BluRay", "Show Season 2"
	reSeasonPack = regexp.MustCompile(`(?i)(?:^|[ ._-])(?:s|season[ ._-]*)0*(\d{1,3})(?:[ ._-]|$)`)
	reYear       = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)\d{2})(?:[^0-9]|$)`)
	reBracketed  = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	reParens     = regexp.MustCompile(`\(([^)]*)\)`)
	reSpaces     = regexp.MustCompile(`\s+`)
	reResolution = regexp.MustCompile(`(?i)\d{3,4}x\d{3,4}`)
)

// ParsePath works out what rel (a slash-separated path under the library
// root) is.
func ParsePath(rel string) Parsed {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	file := parts[len(parts)-1]
	dirs := parts[:len(parts)-1]
	if !IsVideo(file) || isHidden(file) {
		return Parsed{Kind: KindSkip}
	}
	for _, d := range dirs {
		if isHidden(d) || extrasDirs[strings.ToLower(strings.TrimSpace(d))] {
			return Parsed{Kind: KindSkip}
		}
	}
	stem := strings.TrimSuffix(file, filepath.Ext(file))
	if isSample(stem) {
		return Parsed{Kind: KindSkip}
	}

	// Drop category folders ("TV Shows/", "Movies/") from the front.
	for len(dirs) > 0 && categoryDirs[normalizeKey(dirs[0])] {
		dirs = dirs[1:]
	}

	// The nearest folder that names a season.
	season, seasonIx, packShow := -1, -1, ""
	for i := len(dirs) - 1; i >= 0; i-- {
		if n, ok := seasonFromDir(dirs[i]); ok {
			season, seasonIx = n, i
			break
		}
		if n, prefix, ok := seasonPack(dirs[i]); ok {
			season, seasonIx, packShow = n, i, prefix
			break
		}
	}

	p := Parsed{Season: -1}
	ep := findEpisode(stem, season >= 0)
	if ep.found || seasonIx >= 0 {
		p.Kind = KindEpisode
		p.Season = season
		if ep.season >= 0 {
			p.Season = ep.season
		}
		p.Episodes = ep.episodes
		// "102 - Title.mkv" inside "Season 1" is episode 2.
		if len(p.Episodes) == 1 && season > 0 && ep.season < 0 && p.Episodes[0] > 100 && p.Episodes[0]/100 == season {
			p.Episodes = []int{p.Episodes[0] % 100}
		}
		p.EpTitle = episodeTitle(ep.after)

		// The show is the folder above the season folder, else the folder
		// holding a loose episode, else whatever precedes the episode
		// number in the file name.
		var showDir string
		switch {
		case seasonIx > 0:
			showDir = dirs[seasonIx-1]
		case seasonIx < 0 && len(dirs) > 0:
			showDir = dirs[len(dirs)-1]
		}
		switch {
		case showDir != "":
			p.Show, p.Year = titleAndYear(showDir)
		case packShow != "":
			p.Show, p.Year = titleAndYear(packShow)
		}
		if p.Show == "" {
			p.Show, p.Year = titleAndYear(ep.before)
		}
		if p.Show == "" {
			p.Show, p.Year = titleAndYear(stem)
		}
		return p
	}

	// A movie. Its folder usually carries the cleaner name
	// ("Inception (2010)/inception.2010.1080p.mkv"), unless it's a
	// collection folder holding several films.
	p.Kind = KindMovie
	ft, fy := titleAndYear(stem)
	p.Show, p.Year = ft, fy
	if len(dirs) > 0 {
		dt, dy := titleAndYear(dirs[len(dirs)-1])
		if dt != "" && (dy != 0 || normalizeKey(dt) == normalizeKey(ft) || ft == "") {
			p.Show = dt
			if dy != 0 {
				p.Year = dy
			}
		}
	}
	return p
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || strings.HasPrefix(name, "$") ||
		strings.EqualFold(name, "lost+found")
}

var reSample = regexp.MustCompile(`(?i)(?:^|[^a-z])(sample|trailer|ncop\d*|nced\d*)(?:[^a-z]|$)`)

func isSample(stem string) bool { return reSample.MatchString(stem) }

// seasonFromDir recognizes a folder that is only a season ("Season 2",
// "S02", "Specials", "Seson 2").
func seasonFromDir(name string) (int, bool) {
	n := strings.TrimSpace(name)
	l := strings.ToLower(n)
	if l == "specials" || l == "special" || l == "season 0" || l == "sp" || l == "ova" || l == "ovas" {
		return 0, true
	}
	if m := reSeasonDir.FindStringSubmatch(n); m != nil {
		v, _ := strconv.Atoi(m[1])
		return v, true
	}
	// Misspelled "season": the word before the number is within two edits
	// of it ("Seson 1", "Saeson 1", "Seaon 1", "Sesaon 1").
	fields := strings.FieldsFunc(l, func(r rune) bool { return r == ' ' || r == '.' || r == '_' || r == '-' })
	if len(fields) == 2 {
		if v, err := strconv.Atoi(fields[1]); err == nil && len(fields[0]) >= 4 && levenshtein(fields[0], "season") <= 2 {
			return v, true
		}
	}
	// "Season1"
	if i := strings.IndexFunc(l, unicode.IsDigit); i >= 4 {
		if v, err := strconv.Atoi(l[i:]); err == nil && levenshtein(l[:i], "season") <= 2 {
			return v, true
		}
	}
	return 0, false
}

// seasonPack recognizes "Show.Name.S01.1080p" or "Show Name Season 2":
// a season number after some title text, without an episode number.
func seasonPack(name string) (int, string, bool) {
	if reSxE.MatchString(name) {
		return 0, "", false
	}
	loc := reSeasonPack.FindStringSubmatchIndex(name)
	if loc == nil || loc[0] == 0 {
		return 0, "", false
	}
	v, _ := strconv.Atoi(name[loc[2]:loc[3]])
	prefix := name[:loc[0]]
	if cleanTitle(prefix) == "" {
		return 0, "", false
	}
	return v, prefix, true
}

type epMatch struct {
	found    bool
	season   int
	episodes []int
	before   string
	after    string
}

func findEpisode(stem string, inSeason bool) epMatch {
	// Brackets hold release groups, CRCs and resolutions; strip them
	// first so "[1080p]" or "[A1B2C3D4]" can't look like episode numbers.
	// Keep the positions consistent by blanking rather than deleting.
	s := reBracketed.ReplaceAllStringFunc(stem, func(m string) string { return strings.Repeat(" ", len(m)) })
	s = reResolution.ReplaceAllStringFunc(s, func(m string) string { return strings.Repeat(" ", len(m)) })

	if m := reSxE.FindStringSubmatchIndex(s); m != nil {
		season, _ := strconv.Atoi(s[m[2]:m[3]])
		first, _ := strconv.Atoi(s[m[4]:m[5]])
		eps := []int{first}
		if m[6] >= 0 {
			eps = append(eps, expandRange(first, s[m[6]:m[7]])...)
		}
		return epMatch{found: true, season: season, episodes: eps, before: stem[:m[0]], after: stem[m[1]:]}
	}
	if m := reSeasonEp.FindStringSubmatchIndex(s); m != nil {
		season, _ := strconv.Atoi(s[m[2]:m[3]])
		e, _ := strconv.Atoi(s[m[4]:m[5]])
		return epMatch{found: true, season: season, episodes: []int{e}, before: stem[:m[0]], after: stem[m[1]:]}
	}
	if m := reNxN.FindStringSubmatchIndex(s); m != nil {
		season, _ := strconv.Atoi(s[m[2]:m[3]])
		e, _ := strconv.Atoi(s[m[4]:m[5]])
		return epMatch{found: true, season: season, episodes: []int{e}, before: stem[:m[0]], after: stem[m[5]:]}
	}
	if m := reDashNum.FindStringSubmatchIndex(s); m != nil && !looksLikeYear(s[m[2]:m[3]]) {
		e, _ := strconv.Atoi(s[m[2]:m[3]])
		return epMatch{found: true, season: -1, episodes: []int{e}, before: stem[:m[0]], after: stem[m[3]:]}
	}
	if m := reEpOnly.FindStringSubmatchIndex(s); m != nil {
		e, _ := strconv.Atoi(s[m[2]:m[3]])
		return epMatch{found: true, season: -1, episodes: []int{e}, before: stem[:m[0]], after: stem[m[3]:]}
	}
	if inSeason {
		trimmed := strings.TrimLeft(s, " ")
		off := len(s) - len(trimmed)
		if m := reLeadingNum.FindStringSubmatchIndex(trimmed); m != nil {
			e, _ := strconv.Atoi(trimmed[m[2]:m[3]])
			return epMatch{found: true, season: -1, episodes: []int{e}, after: stem[off+m[1]:]}
		}
		// The last lone number: "Show 07 720p" style, once junk is gone.
		clean := cutJunk(spaced(s))
		f := strings.Fields(clean)
		for i := len(f) - 1; i >= 0; i-- {
			if v, err := strconv.Atoi(strings.TrimSuffix(f[i], "v2")); err == nil && v < 2000 {
				return epMatch{found: true, season: -1, episodes: []int{v}, before: strings.Join(f[:i], " "), after: strings.Join(f[i+1:], " ")}
			}
		}
		return epMatch{season: -1, after: stem}
	}
	return epMatch{season: -1}
}

func looksLikeYear(s string) bool {
	v, _ := strconv.Atoi(s)
	return len(s) == 4 && v >= 1900 && v <= 2099
}

var reRangeNums = regexp.MustCompile(`(-|&|e|E)?[ ._]*[eE]?(\d{1,4})`)

// expandRange turns the tail of "S01E01-E03" ("-E03") into [2 3], and
// "E02E03" into [2 3].
func expandRange(first int, tail string) []int {
	var out []int
	last := first
	for _, m := range reRangeNums.FindAllStringSubmatch(tail, -1) {
		v, _ := strconv.Atoi(m[2])
		if v <= last || v-last > 50 {
			continue
		}
		if m[1] == "-" {
			for n := last + 1; n <= v; n++ {
				out = append(out, n)
			}
		} else {
			out = append(out, v)
		}
		last = v
	}
	return out
}

// titleAndYear cleans a folder or file name into a display title and its
// release year (0 if none).
func titleAndYear(s string) (string, int) {
	year := 0
	s = reBracketed.ReplaceAllStringFunc(s, func(m string) string {
		if inner := strings.TrimSpace(m[1 : len(m)-1]); looksLikeYear(inner) && year == 0 {
			year, _ = strconv.Atoi(inner)
			return " \x00 "
		}
		return " "
	})
	// A year in parentheses is the most reliable: "Dune (2021)".
	s = reParens.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimSpace(m[1 : len(m)-1])
		if looksLikeYear(inner) {
			if year == 0 {
				year, _ = strconv.Atoi(inner)
			}
			return " \x00 "
		}
		return " "
	})
	s = spaced(s)
	if year == 0 {
		// A bare year ends the title, but a title can start with one
		// ("2001 A Space Odyssey 1968"), so only a year after some text counts.
		for _, m := range reYear.FindAllStringSubmatchIndex(s, -1) {
			if strings.TrimSpace(s[:m[2]]) == "" {
				continue
			}
			year, _ = strconv.Atoi(s[m[2]:m[3]])
			s = s[:m[2]] + " \x00 " + s[m[3]:]
			break
		}
	}
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return cleanTitle(s), year
}

// cleanTitle turns separator-heavy release text into a readable title.
func cleanTitle(s string) string {
	s = reBracketed.ReplaceAllString(s, " ")
	s = reParens.ReplaceAllString(s, " ")
	s = cutJunk(spaced(s))
	s = strings.Trim(s, " -–_.,:;")
	s = reSpaces.ReplaceAllString(s, " ")
	if s == "" {
		return ""
	}
	// ALLCAPS or all-lowercase names get title case; mixed case is kept
	// as the author wrote it ("iCarly", "MASH").
	if s == strings.ToLower(s) || (s == strings.ToUpper(s) && len(s) > 4) {
		s = titleCase(s)
	}
	return s
}

// spaced replaces dot and underscore separators with spaces, keeping
// dotted acronyms ("S.H.I.E.L.D.") and decimals ("5.1") intact.
func spaced(s string) string {
	if !strings.ContainsAny(s, "._") {
		return s
	}
	r := []rune(s)
	out := make([]rune, 0, len(r))
	for i, c := range r {
		if c == '_' {
			out = append(out, ' ')
			continue
		}
		if c == '.' {
			single := func(j int) bool {
				return j >= 0 && j < len(r) && unicode.IsLetter(r[j]) &&
					(j == 0 || r[j-1] == '.' || r[j-1] == ' ') && (j == len(r)-1 || r[j+1] == '.' || r[j+1] == ' ')
			}
			prevLetter, nextSingle := single(i-1), single(i+1)
			digits := i > 0 && i+1 < len(r) && unicode.IsDigit(r[i-1]) && unicode.IsDigit(r[i+1])
			if (prevLetter && nextSingle) || digits {
				out = append(out, c)
				continue
			}
			out = append(out, ' ')
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

// episodeTitle cleans the text after an episode number. Unlike a show
// name, it can be nothing but release tags ("S01E01.1080p.WEB").
func episodeTitle(s string) string {
	f := strings.Fields(reSpaces.ReplaceAllString(spaced(reBracketed.ReplaceAllString(s, " ")), " "))
	if len(f) > 0 && cutJunk("x "+f[0]) == "x" {
		return ""
	}
	return cleanTitle(s)
}

// cutJunk drops everything from the first release tag on.
func cutJunk(s string) string {
	f := strings.Fields(s)
	for i, w := range f {
		lw := strings.ToLower(strings.Trim(w, "-[]()"))
		lw = strings.ReplaceAll(lw, "-", "")
		if i > 0 && (junkWords[lw] || strings.HasPrefix(lw, "ddp") || strings.HasPrefix(lw, "dts") ||
			strings.HasPrefix(lw, "aac") || strings.HasPrefix(lw, "h264") || strings.HasPrefix(lw, "x264") ||
			strings.HasPrefix(lw, "x265") || strings.HasPrefix(lw, "webdl")) {
			return strings.Join(f[:i], " ")
		}
	}
	return s
}

var smallWords = map[string]bool{"a": true, "an": true, "the": true, "and": true, "of": true, "in": true, "on": true, "at": true, "to": true, "for": true, "or": true, "no": true, "vs": true}

func titleCase(s string) string {
	f := strings.Fields(strings.ToLower(s))
	for i, w := range f {
		if i > 0 && smallWords[w] {
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		f[i] = string(r)
	}
	return strings.Join(f, " ")
}

// normalizeKey is the identity used to group files that name the same
// show differently: case, punctuation, "&" vs "and" and a leading "The"
// don't matter.
func normalizeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "&", " and ")
	var b strings.Builder
	space := false
	for _, r := range foldAccents(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		case r == '\'' || r == '’':
			// "Grey's" and "Greys" are the same show.
		default:
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimPrefix(out, "the ")
	return out
}

var accentFold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o", "ø", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ñ", "n", "ç", "c", "ß", "ss", "ō", "o", "ū", "u",
)

func foldAccents(s string) string { return accentFold.Replace(s) }

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

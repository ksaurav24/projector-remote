package netmirror

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// NetMirror's JSON as it arrives. Fields are declared loosely: the same
// field is a string on one OTT, a number or null on another, and whole
// keys come and go (a Prime movie has no "season" key at all, a Netflix
// one has "episodes":[null]).

type rawHome struct {
	ImgCDN  looseString `json:"imgcdn"`
	ImgCDNH looseString `json:"imgcdn_h"`
	ImgCDNV looseString `json:"imgcdn_v"`
	Slider  []struct {
		ID       looseString `json:"id"`
		Img      looseString `json:"img"`
		Desc     looseString `json:"desc"`
		UA       looseString `json:"ua"`
		NameLogo looseString `json:"namelogo"`
	} `json:"slider"`
	Post []struct {
		IDs  looseString `json:"ids"`
		Cate looseString `json:"cate"`
		Row  looseString `json:"row"`
	} `json:"post"`
}

type rawSearch struct {
	ImgCDN       looseString `json:"imgcdn"`
	SearchResult []struct {
		ID looseString `json:"id"`
		T  looseString `json:"t"`
	} `json:"searchResult"`
}

type rawPost struct {
	MainID         looseString `json:"main_id"`
	Title          looseString `json:"title"`
	Type           looseString `json:"type"`
	Year           looseString `json:"year"`
	UA             looseString `json:"ua"`
	Runtime        looseString `json:"runtime"`
	HDSD           looseString `json:"hdsd"`
	Desc           looseString `json:"desc"`
	MainPoster     looseString `json:"main_poster"`
	EpPoster       looseString `json:"ep_poster"`
	MorelikePoster looseString `json:"morelike_poster"`
	Lang           []struct {
		L looseString `json:"l"`
		S looseString `json:"s"`
	} `json:"lang"`
	Season []struct {
		S        looseString `json:"s"`
		ID       looseString `json:"id"`
		Selected bool        `json:"selected"`
	} `json:"season"`
	rawEpisodes
	Suggest []struct {
		ID looseString `json:"id"`
	} `json:"suggest"`
}

// rawEpisodes is the paged episode list shared by post.php and
// episodes.php.
type rawEpisodes struct {
	Episodes []*struct {
		ID     looseString   `json:"id"`
		T      looseString   `json:"t"`
		Ep     looseInt      `json:"ep"`
		EpDesc looseString   `json:"ep_desc"`
		Info   []looseString `json:"info"`
	} `json:"episodes"`
	NextPageShow looseInt `json:"nextPageShow"`
	NextPage     looseInt `json:"nextPage"`
}

type rawPlayer struct {
	Status    looseString `json:"status"`
	VideoLink looseString `json:"video_link"`
	Referer   looseString `json:"referer"`
}

type rawOTP struct {
	Status    looseString `json:"status"`
	UserToken looseString `json:"usertoken"`
	ErrorMsg  looseString `json:"error_msg"`
	PubMsgImg struct {
		Img looseString `json:"img"`
	} `json:"pub_msg_img"`
}

// idPlaceholder is what NetMirror's image-URL templates use in place of
// the content ID.
const idPlaceholder = "------------------"

func fillTemplate(tmpl, id string) string {
	if tmpl == "" || id == "" {
		return ""
	}
	return strings.Replace(tmpl, idPlaceholder, id, 1)
}

var (
	// "Season 1 (7 EP)"
	seasonLabelRe = regexp.MustCompile(`^(.*?)\s*\((\d+)\s*EP\)\s*$`)
	seasonNumRe   = regexp.MustCompile(`(\d+)`)
	// "38m", "1h 5m", "2h"
	runtimeRe = regexp.MustCompile(`^(\d+h\s*)?(\d+m)?$`)
)

func parseSeasonLabel(s string, index int) (label string, number, episodes int) {
	label = s
	if m := seasonLabelRe.FindStringSubmatch(s); m != nil {
		label = m[1]
		episodes, _ = strconv.Atoi(m[2])
	}
	number = index + 1
	if m := seasonNumRe.FindString(label); m != "" {
		number, _ = strconv.Atoi(m)
	}
	return label, number, episodes
}

// episodeRuntime picks the runtime out of an episode's "info" list. Its
// other entries vary by OTT (Netflix: ["E1","S1","38m"], Prime:
// ["Jun 23, 2026","U/A 16 ","38m"], Hotstar: ["E1:S1","3 Jul 2026","36m"]),
// but the runtime is always last.
func episodeRuntime(info []looseString) string {
	if len(info) == 0 {
		return ""
	}
	last := string(info[len(info)-1])
	if last != "" && runtimeRe.MatchString(last) {
		return last
	}
	return ""
}

// looseString accepts a JSON string, number, bool, or null, and trims
// surrounding whitespace (titles arrive as " Dhurandhar ...").
type looseString string

func (s *looseString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*s = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = looseString(strings.TrimSpace(v))
		return nil
	}
	*s = looseString(strings.TrimSpace(string(b)))
	return nil
}

// looseInt accepts a JSON number, a numeric string, or null (as 0).
type looseInt int

func (n *looseInt) UnmarshalJSON(b []byte) error {
	var s looseString
	if err := s.UnmarshalJSON(b); err != nil {
		return err
	}
	if s == "" {
		*n = 0
		return nil
	}
	v, err := strconv.Atoi(string(s))
	if err != nil {
		return err
	}
	*n = looseInt(v)
	return nil
}

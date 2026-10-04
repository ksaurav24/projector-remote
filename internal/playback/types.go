package playback

import "time"

// NowPlaying is the whole playback session as the phones see it. It is
// always sent as a full snapshot (never a diff), so a phone that
// connects late or reconnects is immediately in sync.
type NowPlaying struct {
	OTT       string `json:"ott"`
	TitleID   string `json:"titleId"`
	ContentID string `json:"contentId"`
	SeasonID  string `json:"seasonId"`
	Title     string `json:"title"`
	// EpLabel ("S1 E3") and EpTitle are empty for movies.
	EpLabel string `json:"epLabel"`
	EpTitle string `json:"epTitle"`
	Poster  string `json:"poster"`
	// NextLabel names the next episode, or is empty when there is none.
	NextLabel string `json:"nextLabel"`
	// Status is "loading", "playing", "paused", "buffering", "ended",
	// or "error".
	Status    string     `json:"status"`
	Pos       int64      `json:"pos"`
	Dur       int64      `json:"dur"`
	Tracks    Tracks     `json:"tracks"`
	Selected  Selected   `json:"selected"`
	Countdown *Countdown `json:"countdown"`
	Error     string     `json:"error"`
	// NeedsLogin marks an error NetMirror's OTP login fixes, so the
	// phone offers it.
	NeedsLogin bool `json:"needsLogin"`
}

// Tracks come from the player, which reads them from the HLS manifest.
// IDs are the player's own ("<group>:<track>") and only hold until the
// next load.
type Tracks struct {
	Audio []Track      `json:"audio"`
	Text  []Track      `json:"text"`
	Video []VideoTrack `json:"video"`
}

type Track struct {
	ID    string `json:"id"`
	Lang  string `json:"lang"`
	Label string `json:"label"`
}

type VideoTrack struct {
	ID      string `json:"id"`
	Height  int    `json:"height"`
	Bitrate int    `json:"bitrate"`
}

// Selected holds the active track IDs; Text may be "off" and Video
// "auto".
type Selected struct {
	Audio string `json:"audio"`
	Text  string `json:"text"`
	Video string `json:"video"`
}

type Countdown struct {
	Seconds int    `json:"seconds"`
	Label   string `json:"label"`
}

func emptyTracks() Tracks {
	return Tracks{Audio: []Track{}, Text: []Track{}, Video: []VideoTrack{}}
}

// playerMsg is anything the player app sends.
type playerMsg struct {
	Type      string   `json:"type"`
	ContentID string   `json:"contentId"`
	Status    string   `json:"status"`
	Pos       int64    `json:"pos"`
	Dur       int64    `json:"dur"`
	Tracks    *Tracks  `json:"tracks"`
	Selected  Selected `json:"selected"`
	HTTP      int      `json:"http"`
	Message   string   `json:"message"`
}

// Clock is time as the manager sees it; tests substitute a fake one.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

type Timer interface {
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time                            { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

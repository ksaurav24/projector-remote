package wsserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"projectorremote/internal/deviceio"
	"projectorremote/internal/playback"
)

// DeviceController is the subset of deviceio.Controller's behavior
// wsserver depends on. Satisfied by *deviceio.Controller in production.
type DeviceController interface {
	KeyEvent(code string) error
	Text(s string) error
	LaunchApp(pkg string) error
	PointerMove(dx, dy int) error
	PointerButton(down bool) error
	Apps() []string
	RefreshApps(ctx context.Context) ([]string, error)
	Connected() bool
	Volume() (int, error)
	SetVolume(level int) error
	ProjectorSettings() (deviceio.ProjectorSettings, error)
	SetZoom(percent int) error
	SetKeystone(axis string, angle int) error
	NudgeCorner(corner, direction string) error
	SetCorner(corner string, x, y int, final bool) error
	ResetKeystone() error
	SetProjectionMode(mode string) error
	SetProjectionFeature(key string, enabled bool) error
	StartFocus(direction string) error
	StopFocus() error
}

// Playback is the subset of *playback.Manager's behavior wsserver
// depends on.
type Playback interface {
	Play(ctx context.Context, ott, titleID, seasonID, episodeID string) error
	SetPaused(paused bool) error
	SeekTo(toMs int64) error
	Skip(byMs int64) error
	SelectTrack(kind, id string) error
	Next(ctx context.Context) error
	Stop()
	CancelAutoplay()
	AutoplayNow(ctx context.Context) error
	Snapshot() *playback.NowPlaying
}

type incoming struct {
	Type  string `json:"type"`
	Dx    int    `json:"dx"`
	Dy    int    `json:"dy"`
	Code  string `json:"code"`
	Value string `json:"value"`
	Pkg   string `json:"pkg"`
	Level int    `json:"level"`

	Percent   int    `json:"percent"`
	Axis      string `json:"axis"`
	Angle     int    `json:"angle"`
	Corner    string `json:"corner"`
	Direction string `json:"direction"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Final     bool   `json:"final"`
	Mode      string `json:"mode"`
	Key       string `json:"key"`
	Enabled   bool   `json:"enabled"`

	OTT       string `json:"ott"`
	TitleID   string `json:"titleId"`
	SeasonID  string `json:"seasonId"`
	EpisodeID string `json:"episodeId"`
	Paused    bool   `json:"paused"`
	To        int64  `json:"to"`
	By        int64  `json:"by"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
}

// Dispatch parses one raw client WS message and invokes the matching
// method on ctrl or pb. Returns an error for malformed JSON, an unknown
// message type, or a rejected command; callers should log and drop the
// message rather than closing the connection.
//
// Commands that go out to NetMirror (play, next, autoplay_now) run in
// the background so they don't hold up this phone's later messages
// (a pause pressed while a title is still resolving); their outcome
// reaches the phones through now_playing, not this return value.
func Dispatch(ctx context.Context, ctrl DeviceController, pb Playback, raw []byte) error {
	var msg incoming
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("wsserver: invalid json: %w", err)
	}
	switch msg.Type {
	case "move":
		return ctrl.PointerMove(msg.Dx, msg.Dy)
	case "pointerdown":
		return ctrl.PointerButton(true)
	case "pointerup":
		return ctrl.PointerButton(false)
	case "key":
		return ctrl.KeyEvent(msg.Code)
	case "text":
		return ctrl.Text(msg.Value)
	case "launch":
		return ctrl.LaunchApp(msg.Pkg)
	case "refresh_apps":
		_, err := ctrl.RefreshApps(ctx)
		return err
	case "set_volume":
		return ctrl.SetVolume(msg.Level)
	case "set_zoom":
		return ctrl.SetZoom(msg.Percent)
	case "set_keystone":
		return ctrl.SetKeystone(msg.Axis, msg.Angle)
	case "nudge_corner":
		return ctrl.NudgeCorner(msg.Corner, msg.Direction)
	case "set_corner":
		return ctrl.SetCorner(msg.Corner, msg.X, msg.Y, msg.Final)
	case "reset_keystone":
		return ctrl.ResetKeystone()
	case "set_projection_mode":
		return ctrl.SetProjectionMode(msg.Mode)
	case "set_projection_feature":
		return ctrl.SetProjectionFeature(msg.Key, msg.Enabled)
	case "focus_start":
		return ctrl.StartFocus(msg.Direction)
	case "focus_stop":
		return ctrl.StopFocus()
	case "play":
		background("play", func() error { return pb.Play(ctx, msg.OTT, msg.TitleID, msg.SeasonID, msg.EpisodeID) })
		return nil
	case "next":
		background("next", func() error { return pb.Next(ctx) })
		return nil
	case "autoplay_now":
		background("autoplay_now", func() error { return pb.AutoplayNow(ctx) })
		return nil
	case "set_paused":
		return pb.SetPaused(msg.Paused)
	case "seek":
		return pb.SeekTo(msg.To)
	case "skip":
		return pb.Skip(msg.By)
	case "select_track":
		return pb.SelectTrack(msg.Kind, msg.ID)
	case "stop":
		pb.Stop()
		return nil
	case "cancel_autoplay":
		pb.CancelAutoplay()
		return nil
	default:
		return fmt.Errorf("wsserver: unknown message type %q", msg.Type)
	}
}

func background(name string, f func() error) {
	go func() {
		if err := f(); err != nil {
			log.Printf("wsserver: %s: %v", name, err)
		}
	}()
}

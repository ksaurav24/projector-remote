package playback

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"projectorremote/internal/history"
	"projectorremote/internal/netmirror"
)

// fakeClock fires timers synchronously from Advance.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	f       func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	was := !t.stopped
	t.stopped = true
	return was
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && !t.at.After(target) && (next == nil || t.at.Before(next.at)) {
				next = t
			}
		}
		if next == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		next.stopped = true
		c.now = next.at
		c.mu.Unlock()
		next.f()
	}
}

// fakePlayer records what the manager sends to the player app.
type fakePlayer struct {
	mu   sync.Mutex
	sent []map[string]any
}

func (p *fakePlayer) Send(v any) error {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	p.mu.Lock()
	p.sent = append(p.sent, m)
	p.mu.Unlock()
	return nil
}

func (p *fakePlayer) Close() error { return nil }

func (p *fakePlayer) ofType(typ string) []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []map[string]any
	for _, m := range p.sent {
		if m["type"] == typ {
			out = append(out, m)
		}
	}
	return out
}

func (p *fakePlayer) last(typ string) map[string]any {
	all := p.ofType(typ)
	if len(all) == 0 {
		return nil
	}
	return all[len(all)-1]
}

type rig struct {
	m        *Manager
	cat      *fakeCatalog
	hist     *history.Store
	clock    *fakeClock
	player   *fakePlayer
	launches int
	snaps    []*NowPlaying
}

func newRig(t *testing.T) *rig {
	t.Helper()
	hist, err := history.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{cat: &fakeCatalog{}, hist: hist, clock: &fakeClock{now: time.Unix(1790000000, 0)}, player: &fakePlayer{}}
	r.m = New(Deps{
		Catalog:  r.cat,
		History:  hist,
		Launch:   func() error { r.launches++; return nil },
		Clock:    r.clock,
		OnChange: func(np *NowPlaying) { r.snaps = append(r.snaps, np) },
	})
	return r
}

// connect attaches the fake player and says hello, as the app does on
// every (re)connect.
func (r *rig) connect(t *testing.T, playing string) {
	t.Helper()
	r.m.attach(r.player)
	r.playerSays(t, map[string]any{"type": "hello", "version": 1, "contentId": playing})
}

func (r *rig) playerSays(t *testing.T, msg map[string]any) {
	t.Helper()
	b, _ := json.Marshal(msg)
	r.m.handlePlayer(r.player, b)
}

func (r *rig) play(t *testing.T, ott, titleID, seasonID, episodeID string) {
	t.Helper()
	_ = r.m.Play(context.Background(), ott, titleID, seasonID, episodeID)
}

func TestPlay_MovieLaunchesPlayerThenLoadsOnHello(t *testing.T) {
	r := newRig(t)
	r.play(t, "nf", "movie", "", "")

	if r.launches != 1 {
		t.Fatalf("launches = %d, want 1", r.launches)
	}
	if r.player.last("load") != nil {
		t.Fatal("load sent before the player connected")
	}
	np := r.m.Snapshot()
	if np == nil || np.Status != "loading" || np.Title != "A Movie" || np.ContentID != "movie" || np.Poster != "movie.jpg" {
		t.Fatalf("snapshot = %+v", np)
	}

	r.connect(t, "")
	load := r.player.last("load")
	if load == nil || load["url"] != "https://cdn/movie.m3u8?in=tok" || load["referer"] != "https://net52.cc" ||
		load["contentId"] != "movie" || load["startPos"] != float64(0) || load["title"] != "A Movie" {
		t.Fatalf("load = %v", load)
	}
}

func TestPlay_ConnectedPlayerGetsLoadImmediately(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	if r.player.last("load") == nil {
		t.Fatal("no load sent to the connected player")
	}
	if r.launches != 1 {
		t.Fatalf("launches = %d, want 1 (brings the player to the front)", r.launches)
	}
}

func TestPlay_PlayerThatNeverConnectsIsAnError(t *testing.T) {
	r := newRig(t)
	r.play(t, "nf", "movie", "", "")
	r.clock.Advance(10 * time.Second)
	np := r.m.Snapshot()
	if np == nil || np.Status != "error" || np.Error == "" {
		t.Fatalf("snapshot = %+v", np)
	}
}

func TestPlay_WithoutTokenIsAnErrorAndLaunchesNothing(t *testing.T) {
	r := newRig(t)
	r.cat.resolveErr = netmirror.ErrNoToken
	if err := r.m.Play(context.Background(), "nf", "movie", "", ""); err == nil {
		t.Fatal("Play succeeded without a token")
	}
	np := r.m.Snapshot()
	if np == nil || np.Status != "error" || !np.NeedsLogin || np.Error != "NetMirror needs you to log in again" {
		t.Fatalf("snapshot = %+v", np)
	}
	if r.launches != 0 {
		t.Fatalf("launches = %d, want 0", r.launches)
	}
}

func TestPlay_UnstartedSeriesStartsAtFirstEpisode(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "show", "", "")
	np := r.m.Snapshot()
	if np.ContentID != "e1" || np.EpLabel != "S1 E1" || np.EpTitle != "One" || np.NextLabel != "S1 E2" || np.Title != "A Show" {
		t.Fatalf("snapshot = %+v", np)
	}
	if load := r.player.last("load"); load["subtitle"] != "S1 E1: One" {
		t.Fatalf("load subtitle = %v", load["subtitle"])
	}
}

func TestPlay_SeriesResumesWhereItLeftOff(t *testing.T) {
	r := newRig(t)
	r.hist.Touch(history.Entry{OTT: "nf", TitleID: "show", SeasonID: "s1", EpisodeID: "e3"})
	r.hist.SavePosition("nf", "e3", 5000, 100000)
	r.connect(t, "")
	r.play(t, "nf", "show", "", "")
	if np := r.m.Snapshot(); np.ContentID != "e3" {
		t.Fatalf("content = %s, want e3", np.ContentID)
	}
	if load := r.player.last("load"); load["startPos"] != float64(5000) {
		t.Fatalf("startPos = %v, want 5000", load["startPos"])
	}
}

func TestPlay_RecordsContinueWatching(t *testing.T) {
	r := newRig(t)
	r.play(t, "nf", "show", "s1", "e2")
	res := r.hist.Resume("nf", "show")
	if res == nil || res.EpisodeID != "e2" || res.SeasonID != "s1" || res.EpLabel != "S1 E2" {
		t.Fatalf("resume = %+v", res)
	}
	if items := r.hist.Continue(); len(items) != 1 || items[0].Poster != "show.jpg" || items[0].Title != "A Show" {
		t.Fatalf("continue = %+v", items)
	}
}

func TestPlay_SendsTrackPreferences(t *testing.T) {
	r := newRig(t)
	r.hist.SetPrefs(history.Prefs{Audio: "hin", Text: "off", Quality: "720"})
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	prefs, _ := r.player.last("load")["prefs"].(map[string]any)
	if prefs["audio"] != "hin" || prefs["text"] != "off" || prefs["quality"] != "720" {
		t.Fatalf("prefs = %v", prefs)
	}
}

func playing(pos, dur int) map[string]any {
	return map[string]any{"type": "state", "status": "playing", "pos": pos, "dur": dur}
}

func TestState_UpdatesSnapshotAndSavesEveryTenSeconds(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")

	r.playerSays(t, playing(1000, 100000))
	if np := r.m.Snapshot(); np.Status != "playing" || np.Pos != 1000 || np.Dur != 100000 {
		t.Fatalf("snapshot = %+v", np)
	}
	if p, _ := r.hist.Position("nf", "movie"); p.Pos != 1000 {
		t.Fatalf("first state not saved: %+v", p)
	}

	r.clock.Advance(5 * time.Second)
	r.playerSays(t, playing(6000, 100000))
	if p, _ := r.hist.Position("nf", "movie"); p.Pos != 1000 {
		t.Fatalf("saved again after 5s: %+v", p)
	}

	r.clock.Advance(5 * time.Second)
	r.playerSays(t, playing(11000, 100000))
	if p, _ := r.hist.Position("nf", "movie"); p.Pos != 11000 {
		t.Fatalf("not saved after 10s: %+v", p)
	}
}

func TestState_PauseSavesImmediately(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.playerSays(t, playing(1000, 100000))
	r.clock.Advance(time.Second)
	r.playerSays(t, map[string]any{"type": "state", "status": "paused", "pos": 2000, "dur": 100000})
	if p, _ := r.hist.Position("nf", "movie"); p.Pos != 2000 {
		t.Fatalf("pause not saved: %+v", p)
	}
}

func startEpisode(t *testing.T, r *rig, season, ep string) {
	t.Helper()
	r.connect(t, "")
	r.play(t, "nf", "show", season, ep)
	r.playerSays(t, playing(1000, 100000))
}

func TestEnded_CountsDownThenPlaysNextEpisode(t *testing.T) {
	r := newRig(t)
	startEpisode(t, r, "s1", "e1")
	r.playerSays(t, map[string]any{"type": "ended"})

	np := r.m.Snapshot()
	if np.Countdown == nil || np.Countdown.Seconds != 10 || np.Countdown.Label != "S1 E2" {
		t.Fatalf("countdown = %+v", np.Countdown)
	}
	if cd := r.player.last("countdown"); cd == nil || cd["seconds"] != float64(10) {
		t.Fatalf("player countdown = %v", cd)
	}

	r.clock.Advance(9 * time.Second)
	if np := r.m.Snapshot(); np.Countdown == nil || np.Countdown.Seconds != 1 {
		t.Fatalf("after 9s countdown = %+v", np.Countdown)
	}
	r.clock.Advance(time.Second)
	if np := r.m.Snapshot(); np.ContentID != "e2" || np.Countdown != nil {
		t.Fatalf("after 10s snapshot = %+v", np)
	}
	if load := r.player.last("load"); load["contentId"] != "e2" {
		t.Fatalf("last load = %v", load)
	}
}

func TestCancelAutoplay(t *testing.T) {
	r := newRig(t)
	startEpisode(t, r, "s1", "e1")
	r.playerSays(t, map[string]any{"type": "ended"})
	r.m.CancelAutoplay()

	if np := r.m.Snapshot(); np.Countdown != nil {
		t.Fatalf("countdown = %+v after cancel", np.Countdown)
	}
	if r.player.last("countdown_cancel") == nil {
		t.Fatal("player was not told to cancel")
	}
	r.clock.Advance(15 * time.Second)
	if loads := r.player.ofType("load"); len(loads) != 1 {
		t.Fatalf("%d loads, want 1 (no autoplay after cancel)", len(loads))
	}
}

func TestPlayerBackDuringCountdownCancelsIt(t *testing.T) {
	r := newRig(t)
	startEpisode(t, r, "s1", "e1")
	r.playerSays(t, map[string]any{"type": "ended"})
	r.playerSays(t, map[string]any{"type": "cancel_autoplay"})
	if np := r.m.Snapshot(); np.Countdown != nil {
		t.Fatalf("countdown = %+v", np.Countdown)
	}
}

func TestAutoplayNow(t *testing.T) {
	r := newRig(t)
	startEpisode(t, r, "s1", "e1")
	r.playerSays(t, map[string]any{"type": "ended"})
	_ = r.m.AutoplayNow(context.Background())
	if np := r.m.Snapshot(); np.ContentID != "e2" || np.Countdown != nil {
		t.Fatalf("snapshot = %+v", np)
	}
}

func TestEnded_SeriesMovesContinueWatchingToNextEpisode(t *testing.T) {
	r := newRig(t)
	startEpisode(t, r, "s1", "e2")
	r.playerSays(t, map[string]any{"type": "ended"})
	res := r.hist.Resume("nf", "show")
	if res == nil || res.EpisodeID != "e3" || res.EpLabel != "S1 E3" || res.Pos != 0 {
		t.Fatalf("resume = %+v", res)
	}
}

func TestEnded_FinaleLeavesContinueWatchingWithoutCountdown(t *testing.T) {
	r := newRig(t)
	startEpisode(t, r, "s2", "f1")
	r.playerSays(t, map[string]any{"type": "ended"})
	if res := r.hist.Resume("nf", "show"); res != nil {
		t.Fatalf("resume = %+v, want nil", res)
	}
	if np := r.m.Snapshot(); np.Countdown != nil || np.Status != "ended" {
		t.Fatalf("snapshot = %+v", np)
	}
}

func TestEnded_MovieLeavesContinueWatching(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.playerSays(t, map[string]any{"type": "ended"})
	if items := r.hist.Continue(); len(items) != 0 {
		t.Fatalf("continue = %+v", items)
	}
}

func TestStopNearTheEndCountsAsWatched(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.playerSays(t, playing(97000, 100000))
	r.m.Stop()
	if items := r.hist.Continue(); len(items) != 0 {
		t.Fatalf("continue = %+v", items)
	}
}

func TestNext_PlaysFollowingEpisodeAndSavesPosition(t *testing.T) {
	r := newRig(t)
	startEpisode(t, r, "s1", "e2")
	r.playerSays(t, playing(40000, 100000))
	if err := r.m.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if np := r.m.Snapshot(); np.ContentID != "e3" || np.EpLabel != "S1 E3" {
		t.Fatalf("snapshot = %+v", np)
	}
	if p, _ := r.hist.Position("nf", "e2"); p.Pos != 40000 {
		t.Fatalf("e2 position = %+v", p)
	}
}

func TestError_ReResolvesOnceThenFails(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.playerSays(t, map[string]any{"type": "error", "http": 403, "message": "Response code: 403", "pos": 5000})

	if len(r.cat.resolves) != 2 {
		t.Fatalf("resolves = %v, want a second resolve", r.cat.resolves)
	}
	if load := r.player.last("load"); load["startPos"] != float64(5000) {
		t.Fatalf("reload = %v", load)
	}

	r.clock.Advance(30 * time.Second)
	r.playerSays(t, map[string]any{"type": "error", "http": 403, "message": "Response code: 403", "pos": 9000})
	if len(r.cat.resolves) != 2 {
		t.Fatalf("resolved again within 60s: %v", r.cat.resolves)
	}
	np := r.m.Snapshot()
	if np.Status != "error" || np.Error == "" || r.player.last("fail") == nil {
		t.Fatalf("snapshot = %+v, fail = %v", np, r.player.last("fail"))
	}
}

func TestError_RetriesAgainAfterAMinute(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.playerSays(t, map[string]any{"type": "error", "pos": 5000})
	r.clock.Advance(61 * time.Second)
	r.playerSays(t, map[string]any{"type": "error", "pos": 9000})
	if len(r.cat.resolves) != 3 {
		t.Fatalf("resolves = %v, want 3", r.cat.resolves)
	}
}

func TestClosed_EndsSessionAndSavesPosition(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.playerSays(t, playing(1000, 100000))
	r.clock.Advance(3 * time.Second)
	r.playerSays(t, playing(4000, 100000))
	r.playerSays(t, map[string]any{"type": "closed"})

	if np := r.m.Snapshot(); np != nil {
		t.Fatalf("snapshot = %+v, want nil", np)
	}
	if last := r.snaps[len(r.snaps)-1]; last != nil {
		t.Fatalf("last broadcast = %+v, want nil", last)
	}
	if p, _ := r.hist.Position("nf", "movie"); p.Pos != 4000 {
		t.Fatalf("position = %+v", p)
	}
}

func TestStop_TellsPlayerAndEndsSession(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.m.Stop()
	if r.player.last("stop") == nil || r.m.Snapshot() != nil {
		t.Fatalf("stop = %v, snapshot = %+v", r.player.last("stop"), r.m.Snapshot())
	}
}

func TestHello_WithoutSessionStopsPlayer(t *testing.T) {
	r := newRig(t)
	r.connect(t, "movie")
	if r.player.last("stop") == nil {
		t.Fatal("player with no session was not stopped")
	}
}

func TestHello_SameContentAfterReconnectDoesNotReload(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.connect(t, "movie")
	if loads := r.player.ofType("load"); len(loads) != 1 {
		t.Fatalf("%d loads, want 1", len(loads))
	}
}

func withTracks() map[string]any {
	return map[string]any{
		"type": "state", "status": "playing", "pos": 1, "dur": 100,
		"tracks": map[string]any{
			"audio": []any{map[string]any{"id": "0:0", "lang": "eng", "label": "English"}, map[string]any{"id": "0:1", "lang": "hin", "label": "Hindi"}},
			"text":  []any{map[string]any{"id": "2:0", "lang": "eng", "label": "English"}},
			"video": []any{map[string]any{"id": "1:0", "height": 720, "bitrate": 600000}, map[string]any{"id": "1:1", "height": 1080, "bitrate": 1000000}},
		},
	}
}

func TestSelectTrack_ForwardsAndRemembersPreference(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.playerSays(t, withTracks())

	steps := []struct {
		kind, id string
		want     history.Prefs
	}{
		{"audio", "0:1", history.Prefs{Audio: "hin"}},
		{"text", "2:0", history.Prefs{Audio: "hin", Text: "eng"}},
		{"text", "off", history.Prefs{Audio: "hin", Text: "off"}},
		{"video", "1:1", history.Prefs{Audio: "hin", Text: "off", Quality: "1080"}},
		{"video", "auto", history.Prefs{Audio: "hin", Text: "off", Quality: "auto"}},
	}
	for _, s := range steps {
		if err := r.m.SelectTrack(s.kind, s.id); err != nil {
			t.Fatal(err)
		}
		sel := r.player.last("select_track")
		if sel["kind"] != s.kind || sel["id"] != s.id {
			t.Fatalf("forwarded %v, want %s %s", sel, s.kind, s.id)
		}
		if got := r.hist.Prefs(); got != s.want {
			t.Fatalf("after %s %s prefs = %+v, want %+v", s.kind, s.id, got, s.want)
		}
	}
}

func TestSelectTrack_UnknownIDIsError(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	r.playerSays(t, withTracks())
	if err := r.m.SelectTrack("audio", "9:9"); err == nil {
		t.Fatal("unknown track accepted")
	}
}

func TestTransportCommandsAreForwarded(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	_ = r.m.SetPaused(true)
	_ = r.m.SeekTo(42000)
	_ = r.m.Skip(-10000)
	if p := r.player.last("set_paused"); p["paused"] != true {
		t.Errorf("set_paused = %v", p)
	}
	if s := r.player.last("seek"); s["to"] != float64(42000) {
		t.Errorf("seek = %v", s)
	}
	if s := r.player.last("skip"); s["by"] != float64(-10000) {
		t.Errorf("skip = %v", s)
	}
}

// NetMirror playlists can understate the media's length; the player then
// errors out past the stated end. That's the end, not a broken stream.
func TestError_AtTheEndCountsAsEnded(t *testing.T) {
	r := newRig(t)
	startEpisode(t, r, "s1", "e1")
	r.playerSays(t, playing(99000, 100000))
	r.playerSays(t, map[string]any{"type": "error", "message": "ERROR_CODE_TIMEOUT", "pos": 99500})
	if len(r.cat.resolves) != 1 {
		t.Fatalf("re-resolved at the end: %v", r.cat.resolves)
	}
	if np := r.m.Snapshot(); np.Status != "ended" || np.Countdown == nil {
		t.Fatalf("snapshot = %+v", np)
	}
}

func TestError_RetriesAtMostThreeTimes(t *testing.T) {
	r := newRig(t)
	r.connect(t, "")
	r.play(t, "nf", "movie", "", "")
	for range 4 {
		r.playerSays(t, map[string]any{"type": "error", "pos": 5000})
		r.clock.Advance(2 * time.Minute)
	}
	if len(r.cat.resolves) != 4 {
		t.Fatalf("resolves = %d, want 1 + 3 retries", len(r.cat.resolves))
	}
	if np := r.m.Snapshot(); np.Status != "error" {
		t.Fatalf("status = %q after the 4th error, want error", np.Status)
	}
}

// Moving to the next episode shouldn't blank the phone's now-playing
// bar while the new stream resolves.
func TestNext_KeepsTitleWhileLoading(t *testing.T) {
	r := newRig(t)
	startEpisode(t, r, "s1", "e1")
	r.snaps = nil
	_ = r.m.Next(context.Background())
	first := r.snaps[0]
	if first.Status != "loading" || first.Title != "A Show" || first.Poster != "show.jpg" {
		t.Fatalf("first snapshot while loading = %+v", first)
	}
}

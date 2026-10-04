// Package playback runs the one playback session the projector can
// have: it resolves what to play through the catalogue, drives the player
// app over its /player WebSocket, and keeps watch history, the
// next-episode countdown, and every phone's now-playing view in step.
//
// The player app is kept deliberately thin. All session logic lives
// here, so changing behavior rarely means rebuilding the APK.
package playback

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"slices"
	"strconv"
	"sync"
	"time"

	"projectorremote/internal/catalog"
	"projectorremote/internal/history"
	"projectorremote/internal/netmirror"
)

const (
	PlayerActivity = "com.projectorremote.player/.PlayerActivity"

	connectTimeout = 10 * time.Second
	countdownFrom  = 10
	saveEvery      = 10 * time.Second
	// A playback error within this long of the last automatic
	// re-resolve, or after maxRetries of them, fails the session instead
	// of retrying again.
	retryWindow = 60 * time.Second
	maxRetries  = 3
)

// History is the subset of *history.Store playback uses.
type History interface {
	SavePosition(ott, id string, pos, dur int64)
	StartPosition(ott, id string) int64
	Touch(e history.Entry)
	Forget(ott, titleID string)
	Resume(ott, titleID string) *history.Resume
	Prefs() history.Prefs
	SetPrefs(p history.Prefs)
}

type Deps struct {
	Catalog Catalog
	History History
	// Launch brings the player app to the front, starting it if needed.
	Launch func() error
	Clock  Clock
	// OnChange receives every new snapshot (nil once nothing is
	// playing). Called with the manager's lock held: it must not call
	// back into the Manager.
	OnChange func(*NowPlaying)
}

// LaunchPlayer starts (or brings forward) the player app. The
// projector drops into a screensaver ("dreaming") when idle, which sits
// above anything started under it, so it's woken first; the wakeup key
// is a no-op when already awake.
func LaunchPlayer() error {
	if err := exec.Command("input", "keyevent", "KEYCODE_WAKEUP").Run(); err != nil {
		log.Printf("playback: wakeup: %v", err)
	}
	return exec.Command("am", "start", "-n", PlayerActivity).Run()
}

type playerConn interface {
	Send(v any) error
	Close() error
}

type nextTarget struct {
	seasonID, episodeID, label string
}

type session struct {
	cancel   context.CancelFunc
	gen      int
	np       NowPlaying
	seasonID string
	next     *nextTarget
	stream   *catalog.Stream
	startPos int64

	// delivered is set once the connected player has this session's
	// stream (sent a load, or said on reconnect it's already playing it).
	delivered bool
	lastSave  time.Time
	lastRetry time.Time
	retries   int

	connectTimer   Timer
	countdownTimer Timer
}

type Manager struct {
	d Deps

	mu     sync.Mutex
	gen    int
	sess   *session
	player playerConn
}

func New(d Deps) *Manager {
	if d.Clock == nil {
		d.Clock = realClock{}
	}
	return &Manager{d: d}
}

// Snapshot returns the current session, or nil if nothing is playing.
func (m *Manager) Snapshot() *NowPlaying {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

// snapshotLocked copies the session. The copy is shallow: slices and
// the Countdown pointer are only ever replaced, never mutated in place,
// so sharing them is safe.
func (m *Manager) snapshotLocked() *NowPlaying {
	if m.sess == nil {
		return nil
	}
	np := m.sess.np
	return &np
}

func (m *Manager) publishLocked() {
	if m.d.OnChange != nil {
		m.d.OnChange(m.snapshotLocked())
	}
}

func (m *Manager) sendLocked(msg map[string]any) {
	if m.player == nil {
		return
	}
	if err := m.player.Send(msg); err != nil {
		log.Printf("playback: send %v: %v", msg["type"], err)
	}
}

// userMessage turns a failure into what the phone shows.
func userMessage(err error) string {
	switch {
	case errors.Is(err, netmirror.ErrNoToken):
		return "NetMirror needs you to log in again"
	case errors.Is(err, context.DeadlineExceeded):
		return "The source providers took too long to respond. Try again."
	case errors.Is(err, catalog.ErrNotFound):
		return "This title is not available"
	}
	return "No playable source is available. Try again in a moment."
}

type prepared struct {
	title     *catalog.Title
	contentID string
	seasonID  string
	epLabel   string
	epTitle   string
	next      *nextTarget
	stream    *catalog.Stream
}

// prepare does Play's network work, without the lock held: the title,
// the episode (and the one after it, so the countdown and continue-
// watching can move on without fetching later), and the stream.
func (m *Manager) prepare(ctx context.Context, ott, titleID, seasonID, episodeID string) (*prepared, error) {
	t, err := m.d.Catalog.Title(ctx, ott, titleID)
	if err != nil {
		return nil, err
	}
	p := &prepared{title: t, contentID: t.ID}
	if t.Type == "series" {
		if seasonID == "" && episodeID == "" {
			if r := m.d.History.Resume(ott, titleID); r != nil {
				seasonID, episodeID = r.SeasonID, r.EpisodeID
			}
		}
		c, err := locate(ctx, m.d.Catalog, ott, t, seasonID, episodeID)
		if err != nil {
			return nil, err
		}
		ep := c.current()
		p.contentID, p.seasonID, p.epLabel, p.epTitle = ep.ID, c.season().ID, episodeLabel(ott, c.season(), ep), ep.Title
		season, nextEp, ok, err := c.next(ctx, m.d.Catalog, ott)
		if err != nil {
			// Not fatal: this episode still plays, just without a Next.
			log.Printf("playback: next episode after %s: %v", ep.ID, err)
		} else if ok {
			p.next = &nextTarget{seasonID: season.ID, episodeID: nextEp.ID, label: episodeLabel(ott, season, nextEp)}
		}
	}
	if p.stream, err = m.d.Catalog.Resolve(ctx, ott, p.contentID); err != nil {
		return p, err
	}
	return p, nil
}

// Play starts a movie, or an episode of a series, on the projector. For
// a series with no seasonID/episodeID it continues from watch history,
// or starts at S1 E1. A newer Play (or Stop) supersedes one still
// resolving.
func (m *Manager) Play(ctx context.Context, ott, titleID, seasonID, episodeID string) error {
	if !catalog.ValidOTT(ott) || titleID == "" {
		return fmt.Errorf("playback: bad play request %q %q", ott, titleID)
	}
	m.mu.Lock()
	np := NowPlaying{OTT: ott, TitleID: titleID, ContentID: episodeID, SeasonID: seasonID, Status: "loading", Tracks: emptyTracks()}
	if prev := m.sess; prev != nil && prev.np.OTT == ott && prev.np.TitleID == titleID {
		// Same show (Next, autoplay): keep its name and art up while
		// the new episode resolves.
		np.Title, np.Poster = prev.np.Title, prev.np.Poster
	}
	m.endLocked()
	m.gen++
	gen := m.gen
	ctx, cancel := context.WithCancel(ctx)
	m.sess = &session{gen: gen, np: np, cancel: cancel}
	m.publishLocked()
	m.mu.Unlock()

	p, err := m.prepare(ctx, ott, titleID, seasonID, episodeID)

	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sess
	if s == nil || s.gen != gen {
		return nil
	}
	if p != nil {
		s.np.Title, s.np.Poster = p.title.Title, p.title.Poster
		s.np.ContentID, s.np.SeasonID, s.np.EpLabel, s.np.EpTitle = p.contentID, p.seasonID, p.epLabel, p.epTitle
	}
	if err != nil {
		m.failErrLocked(err)
		return err
	}
	s.np.Title, s.np.Poster = p.title.Title, p.title.Poster
	s.np.ContentID, s.np.EpLabel, s.np.EpTitle = p.contentID, p.epLabel, p.epTitle
	s.seasonID, s.next, s.stream = p.seasonID, p.next, p.stream
	if p.next != nil {
		s.np.NextLabel = p.next.label
	}
	s.startPos = m.d.History.StartPosition(ott, p.contentID)
	m.d.History.Touch(history.Entry{
		OTT: ott, TitleID: titleID, SeasonID: s.seasonID, EpisodeID: p.contentID,
		Title: s.np.Title, EpLabel: s.np.EpLabel, Poster: s.np.Poster,
	})

	if err := m.d.Launch(); err != nil {
		m.failLocked("Couldn't start the player app on the projector")
		return fmt.Errorf("playback: launch player: %w", err)
	}
	if m.player != nil {
		m.sendLoadLocked()
	} else {
		s.connectTimer = m.d.Clock.AfterFunc(connectTimeout, func() { m.connectTimedOut(gen) })
	}
	m.publishLocked()
	return nil
}

func (m *Manager) sendLoadLocked() {
	s := m.sess
	subtitle := s.np.EpLabel
	if s.np.EpTitle != "" {
		subtitle += ": " + s.np.EpTitle
	}
	m.sendLocked(map[string]any{
		"type":      "load",
		"contentId": s.np.ContentID,
		"url":       s.stream.URL,
		"referer":   s.stream.Referer,
		"headers":   s.stream.Headers,
		"mimeType":  s.stream.MimeType,
		"subtitles": s.stream.Subtitles,
		"title":     s.np.Title,
		"subtitle":  subtitle,
		"startPos":  s.startPos,
		"prefs":     m.d.History.Prefs(),
	})
	s.delivered = true
	if s.connectTimer != nil {
		s.connectTimer.Stop()
	}
}

func (m *Manager) connectTimedOut(gen int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sess; s != nil && s.gen == gen && !s.delivered {
		m.failLocked("The player app on the projector isn't responding")
	}
}

// failErrLocked fails the session with err's message for the phone.
func (m *Manager) failErrLocked(err error) {
	m.sess.np.NeedsLogin = errors.Is(err, netmirror.ErrNoToken)
	m.failLocked(userMessage(err))
}

// failLocked puts the session in its error state, shown on the phone
// until dismissed, and on the projector if the player is up.
func (m *Manager) failLocked(msg string) {
	s := m.sess
	m.stopTimersLocked(s)
	s.np.Status, s.np.Error, s.np.Countdown = "error", msg, nil
	m.sendLocked(map[string]any{"type": "fail", "message": msg})
	m.publishLocked()
}

func (m *Manager) stopTimersLocked(s *session) {
	for _, t := range []Timer{s.connectTimer, s.countdownTimer} {
		if t != nil {
			t.Stop()
		}
	}
}

// endLocked closes the current session (if any): saves where it got to
// and applies the watched-to-the-end rules. It doesn't tell the player
// or publish; callers decide that.
func (m *Manager) endLocked() {
	s := m.sess
	if s == nil {
		return
	}
	m.stopTimersLocked(s)
	if s.cancel != nil {
		s.cancel()
	}
	if s.stream != nil && s.np.Dur > 0 && s.np.Status != "ended" {
		m.d.History.SavePosition(s.np.OTT, s.np.ContentID, s.np.Pos, s.np.Dur)
		if float64(s.np.Pos) >= history.FinishedRatio*float64(s.np.Dur) {
			m.finishedLocked(s)
		}
	}
	m.sess = nil
}

// finishedLocked moves continue-watching past a title that's been
// watched: a series on to its next episode, anything else off the list.
func (m *Manager) finishedLocked(s *session) {
	if s.next == nil {
		m.d.History.Forget(s.np.OTT, s.np.TitleID)
		return
	}
	m.d.History.Touch(history.Entry{
		OTT: s.np.OTT, TitleID: s.np.TitleID, SeasonID: s.next.seasonID, EpisodeID: s.next.episodeID,
		Title: s.np.Title, EpLabel: s.next.label, Poster: s.np.Poster,
	})
}

// Next plays the episode after the current one.
func (m *Manager) Next(ctx context.Context) error {
	m.mu.Lock()
	s := m.sess
	if s == nil || s.next == nil {
		m.mu.Unlock()
		return errors.New("playback: no next episode")
	}
	ott, titleID, n := s.np.OTT, s.np.TitleID, *s.next
	m.mu.Unlock()
	return m.Play(ctx, ott, titleID, n.seasonID, n.episodeID)
}

// Stop ends the session and closes the player.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess == nil {
		return
	}
	m.endLocked()
	m.sendLocked(map[string]any{"type": "stop"})
	m.publishLocked()
}

func (m *Manager) startCountdownLocked(s *session) {
	s.np.Countdown = &Countdown{Seconds: countdownFrom, Label: s.next.label}
	m.sendCountdownLocked(s)
	gen := s.gen
	s.countdownTimer = m.d.Clock.AfterFunc(time.Second, func() { m.tick(gen) })
}

func (m *Manager) sendCountdownLocked(s *session) {
	m.sendLocked(map[string]any{"type": "countdown", "seconds": s.np.Countdown.Seconds, "label": s.np.Countdown.Label})
}

func (m *Manager) tick(gen int) {
	m.mu.Lock()
	s := m.sess
	if s == nil || s.gen != gen || s.np.Countdown == nil {
		m.mu.Unlock()
		return
	}
	left := s.np.Countdown.Seconds - 1
	if left <= 0 {
		s.np.Countdown = nil
		m.mu.Unlock()
		if err := m.Next(context.Background()); err != nil {
			log.Printf("playback: autoplay: %v", err)
		}
		return
	}
	s.np.Countdown = &Countdown{Seconds: left, Label: s.np.Countdown.Label}
	m.sendCountdownLocked(s)
	m.publishLocked()
	s.countdownTimer = m.d.Clock.AfterFunc(time.Second, func() { m.tick(gen) })
	m.mu.Unlock()
}

func (m *Manager) CancelAutoplay() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelCountdownLocked()
}

func (m *Manager) cancelCountdownLocked() {
	s := m.sess
	if s == nil || s.np.Countdown == nil {
		return
	}
	s.countdownTimer.Stop()
	s.np.Countdown = nil
	m.sendLocked(map[string]any{"type": "countdown_cancel"})
	m.publishLocked()
}

// AutoplayNow skips the rest of the countdown.
func (m *Manager) AutoplayNow(ctx context.Context) error {
	m.mu.Lock()
	s := m.sess
	if s == nil || s.np.Countdown == nil {
		m.mu.Unlock()
		return errors.New("playback: no countdown running")
	}
	s.countdownTimer.Stop()
	s.np.Countdown = nil
	m.mu.Unlock()
	return m.Next(ctx)
}

// playerCommand forwards a transport command to the player.
func (m *Manager) playerCommand(msg map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess == nil || m.player == nil {
		return errors.New("playback: nothing playing")
	}
	m.sendLocked(msg)
	return nil
}

func (m *Manager) SetPaused(paused bool) error {
	return m.playerCommand(map[string]any{"type": "set_paused", "paused": paused})
}

func (m *Manager) SeekTo(toMs int64) error {
	return m.playerCommand(map[string]any{"type": "seek", "to": toMs})
}

func (m *Manager) Skip(byMs int64) error {
	return m.playerCommand(map[string]any{"type": "skip", "by": byMs})
}

// SelectTrack switches a track live and remembers the choice (by
// language or height, not id) for everything played after.
func (m *Manager) SelectTrack(kind, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sess
	if s == nil || m.player == nil {
		return errors.New("playback: nothing playing")
	}
	prefs := m.d.History.Prefs()
	switch kind {
	case "audio":
		i := slices.IndexFunc(s.np.Tracks.Audio, func(t Track) bool { return t.ID == id })
		if i < 0 {
			return fmt.Errorf("playback: no audio track %q", id)
		}
		prefs.Audio = s.np.Tracks.Audio[i].Lang
	case "text":
		if id == "off" {
			prefs.Text = "off"
			break
		}
		i := slices.IndexFunc(s.np.Tracks.Text, func(t Track) bool { return t.ID == id })
		if i < 0 {
			return fmt.Errorf("playback: no subtitle track %q", id)
		}
		prefs.Text = s.np.Tracks.Text[i].Lang
	case "video":
		if id == "auto" {
			prefs.Quality = "auto"
			break
		}
		i := slices.IndexFunc(s.np.Tracks.Video, func(t VideoTrack) bool { return t.ID == id })
		if i < 0 {
			return fmt.Errorf("playback: no video track %q", id)
		}
		prefs.Quality = strconv.Itoa(s.np.Tracks.Video[i].Height)
	default:
		return fmt.Errorf("playback: unknown track kind %q", kind)
	}
	m.d.History.SetPrefs(prefs)
	m.sendLocked(map[string]any{"type": "select_track", "kind": kind, "id": id})
	return nil
}

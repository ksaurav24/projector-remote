package playback

import (
	"context"
	"errors"
	"time"

	"projectorremote/internal/catalog"
	"projectorremote/internal/history"
)

type skipStore interface {
	SkipSettings() history.SkipSettings
	SetSkipMode(string, string) error
}

func (m *Manager) SkipSettings() history.SkipSettings {
	if store, ok := m.d.History.(skipStore); ok {
		return store.SkipSettings()
	}
	return (history.SkipSettings{}).Defaults()
}

func (m *Manager) SetSkipMode(kind, mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	store, ok := m.d.History.(skipStore)
	if !ok {
		return errors.New("anime skip settings unavailable")
	}
	if err := store.SetSkipMode(kind, mode); err != nil {
		return err
	}
	if s := m.sess; s != nil {
		s.np.SkipSettings = store.SkipSettings()
		if s.np.SkipSettings.Intro == "off" && s.np.SkipSettings.Outro == "off" && s.skipCancel != nil {
			s.skipCancel()
			s.skipCancel = nil
			s.skipAttempted = false
		}
		m.sendSkipLocked()
		m.fetchSkipLocked(s)
		m.publishLocked()
	}
	return nil
}

func (m *Manager) SkipSegment(contentID, loadID, kind string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sess
	if s == nil || s.np.OTT != "anime" || s.np.ContentID != contentID || s.np.LoadID != loadID || !s.delivered || m.player == nil {
		return errors.New("this episode is no longer playing")
	}
	var r *catalog.SkipRange
	mode := "off"
	switch kind {
	case "intro":
		r, mode = s.np.SkipRanges.Intro, s.np.SkipSettings.Intro
	case "outro":
		r, mode = s.np.SkipRanges.Outro, s.np.SkipSettings.Outro
	}
	if mode == "off" || r == nil {
		return errors.New("skip timestamps unavailable")
	}
	// Android checks its precise current position; phone snapshots can lag.
	m.sendLocked(map[string]any{"type": "skip_segment", "contentId": contentID, "loadId": loadID, "kind": kind})
	return nil
}

func (m *Manager) sendSkipLocked() {
	s := m.sess
	if s == nil || !s.delivered {
		return
	}
	m.sendLocked(map[string]any{"type": "skip_ranges", "contentId": s.np.ContentID, "loadId": s.np.LoadID, "skipRanges": s.np.SkipRanges, "skipSettings": s.np.SkipSettings})
}

func (m *Manager) fetchSkipLocked(s *session) {
	if s.np.OTT != "anime" || s.stream == nil || !s.delivered || s.np.Dur <= 0 || s.skipAttempted || s.np.Status == "ended" || s.np.Status == "error" {
		return
	}
	prefs := s.np.SkipSettings
	if (prefs.Intro == "off" || s.np.SkipRanges.Intro != nil) && (prefs.Outro == "off" || s.np.SkipRanges.Outro != nil) {
		return
	}
	source, ok := m.d.Catalog.(interface {
		SkipTimes(context.Context, string, string, *catalog.Stream, int64) catalog.SkipRanges
	})
	if !ok {
		return
	}
	s.skipAttempted = true
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	s.skipCancel = cancel
	gen, loadID, id, stream, dur := s.gen, s.np.LoadID, s.np.ContentID, s.stream, s.np.Dur
	go func() {
		defer cancel()
		ranges := source.SkipTimes(ctx, "anime", id, stream, dur)
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		current := m.sess
		if current == nil || current.gen != gen || current.np.LoadID != loadID || current.np.Status == "error" || current.np.Status == "ended" {
			return
		}
		current.np.SkipRanges = ranges.ForDuration(current.np.Dur)
		m.sendSkipLocked()
		m.publishLocked()
	}()
}

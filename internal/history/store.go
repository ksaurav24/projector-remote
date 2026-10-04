// Package history persists what has been watched: playback positions,
// the continue-watching list, and track preferences. Everything lives
// in one small JSON file, rewritten atomically on each change.
package history

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	maxPositions = 500
	maxRecent    = 20
	// FinishedRatio is how far through a title counts as watched: a
	// saved position at or past it restarts from 0 rather than resuming.
	FinishedRatio = 0.95
)

// Entry is one continue-watching item: the latest episode (or the
// movie itself, where EpisodeID == TitleID) watched from a title.
type Entry struct {
	OTT       string `json:"ott"`
	TitleID   string `json:"titleId"`
	SeasonID  string `json:"seasonId,omitempty"`
	EpisodeID string `json:"episodeId"`
	Title     string `json:"title"`
	EpLabel   string `json:"epLabel,omitempty"`
	Poster    string `json:"poster"`
	Updated   int64  `json:"updated"`
}

// Position is a saved playback point. Pos and Dur are milliseconds;
// Updated is unix seconds.
type Position struct {
	Pos     int64 `json:"pos"`
	Dur     int64 `json:"dur"`
	Updated int64 `json:"updated"`
}

// Prefs are the last track choices, applied to every new stream so
// they carry across episodes. Empty means no preference; Text "off"
// disables subtitles; Quality is "auto" or a height like "720".
type Prefs struct {
	Audio   string `json:"audio"`
	Text    string `json:"text"`
	Quality string `json:"quality"`
}

// ContinueItem is an Entry plus its saved position, as the phone's
// continue-watching row shows it.
type ContinueItem struct {
	OTT       string `json:"ott"`
	TitleID   string `json:"titleId"`
	EpisodeID string `json:"episodeId"`
	Title     string `json:"title"`
	EpLabel   string `json:"epLabel"`
	Poster    string `json:"poster"`
	Pos       int64  `json:"pos"`
	Dur       int64  `json:"dur"`
}

// Resume is where a title's Play button picks up.
type Resume struct {
	EpisodeID string `json:"episodeId"`
	SeasonID  string `json:"seasonId"`
	EpLabel   string `json:"epLabel"`
	Pos       int64  `json:"pos"`
	Dur       int64  `json:"dur"`
}

type Bookmark struct {
	OTT     string `json:"ott"`
	TitleID string `json:"titleId"`
	Title   string `json:"title"`
	Poster  string `json:"poster"`
}

type fileData struct {
	Bookmarks []Bookmark          `json:"bookmarks,omitempty"`
	Positions map[string]Position `json:"positions"`
	Recent    []Entry             `json:"recent"`
	Prefs     Prefs               `json:"prefs"`
}

type Store struct {
	path string
	now  func() time.Time

	mu   sync.Mutex
	data fileData
}

// Open loads the store at path. A missing file is an empty store; a
// corrupt one is an error (callers can fall back to New, which will
// overwrite it on the next change).
func Open(path string) (*Store, error) {
	s := New(path)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("history: %s: %w", path, err)
	}
	if s.data.Positions == nil {
		s.data.Positions = make(map[string]Position)
	}
	return s, nil
}

// New returns an empty store that will write to path.
func New(path string) *Store {
	return &Store{
		path: path,
		now:  time.Now,
		data: fileData{Positions: make(map[string]Position)},
	}
}

func key(ott, id string) string { return ott + ":" + id }

func (s *Store) SavePosition(ott, id string, pos, dur int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Positions[key(ott, id)] = Position{Pos: pos, Dur: dur, Updated: s.now().Unix()}
	if len(s.data.Positions) > maxPositions {
		s.evictOldestPositions()
	}
	s.save()
}

func (s *Store) evictOldestPositions() {
	keys := make([]string, 0, len(s.data.Positions))
	for k := range s.data.Positions {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Compare(s.data.Positions[a].Updated, s.data.Positions[b].Updated)
	})
	for _, k := range keys[:len(keys)-maxPositions] {
		delete(s.data.Positions, k)
	}
}

// Position returns the saved position for a movie or episode.
func (s *Store) Position(ott, id string) (Position, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.position(ott, id)
}

func (s *Store) position(ott, id string) (Position, bool) {
	p, ok := s.data.Positions[key(ott, id)]
	return p, ok
}

// StartPosition is where playback of id should begin: its saved
// position, or 0 if it was never started or was watched to the end.
func (s *Store) StartPosition(ott, id string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startPosition(ott, id)
}

func (s *Store) startPosition(ott, id string) int64 {
	p, ok := s.position(ott, id)
	if !ok || p.Dur <= 0 || float64(p.Pos) >= FinishedRatio*float64(p.Dur) {
		return 0
	}
	return p.Pos
}

// Touch makes e the title's continue-watching entry and moves it to
// the front.
func (s *Store) Touch(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.Updated = s.now().Unix()
	s.data.Recent = slices.DeleteFunc(s.data.Recent, func(r Entry) bool {
		return r.OTT == e.OTT && r.TitleID == e.TitleID
	})
	s.data.Recent = slices.Insert(s.data.Recent, 0, e)
	if len(s.data.Recent) > maxRecent {
		s.data.Recent = s.data.Recent[:maxRecent]
	}
	s.save()
}

// Forget drops a title from continue-watching (a finished movie, or a
// series after its finale).
func (s *Store) Forget(ott, titleID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Recent = slices.DeleteFunc(s.data.Recent, func(r Entry) bool {
		return r.OTT == ott && r.TitleID == titleID
	})
	s.save()
}

func (s *Store) Continue() []ContinueItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]ContinueItem, 0, len(s.data.Recent))
	for _, e := range s.data.Recent {
		p, _ := s.position(e.OTT, e.EpisodeID)
		items = append(items, ContinueItem{
			OTT: e.OTT, TitleID: e.TitleID, EpisodeID: e.EpisodeID,
			Title: e.Title, EpLabel: e.EpLabel, Poster: e.Poster,
			Pos: p.Pos, Dur: p.Dur,
		})
	}
	return items
}

// Resume returns where the title left off, or nil if it isn't in
// continue-watching.
func (s *Store) Resume(ott, titleID string) *Resume {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.data.Recent {
		if e.OTT == ott && e.TitleID == titleID {
			p, _ := s.position(ott, e.EpisodeID)
			return &Resume{
				EpisodeID: e.EpisodeID, SeasonID: e.SeasonID, EpLabel: e.EpLabel,
				Pos: s.startPosition(ott, e.EpisodeID), Dur: p.Dur,
			}
		}
	}
	return nil
}

func (s *Store) Prefs() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Prefs
}

func (s *Store) SetPrefs(p Prefs) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Prefs = p
	s.save()
}

// save writes the whole file via a temp file and rename, so a crash or
// power cut mid-write never leaves a truncated state.json. Failures are
// logged, not returned: losing a position update isn't worth failing
// playback over. Callers hold s.mu.
func (s *Store) save() {
	if err := s.persist(); err != nil {
		log.Printf("history: save: %v", err)
	}
}

func (s *Store) persist() error {
	b, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	// CreateTemp makes the file 0600; nothing in it is secret, and being
	// able to read it from a plain `adb shell` helps when debugging.
	cherr := tmp.Chmod(0o644)
	cerr := tmp.Close()
	if werr != nil || cherr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return errors.Join(werr, cherr, cerr)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Bookmarks are independent of continue-watching and are never evicted when
// a title finishes. Return a copy so callers cannot mutate the store.
func (s *Store) Bookmarks() []Bookmark {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Bookmark{}, s.data.Bookmarks...)
}
func (s *Store) SetBookmark(b Bookmark, saved bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.data.Bookmarks
	next := make([]Bookmark, 0, len(previous)+1)
	if saved {
		next = append(next, b)
	}
	for _, item := range previous {
		if item.OTT != b.OTT || item.TitleID != b.TitleID {
			next = append(next, item)
		}
	}
	s.data.Bookmarks = next
	if err := s.persist(); err != nil {
		s.data.Bookmarks = previous
		return err
	}
	return nil
}

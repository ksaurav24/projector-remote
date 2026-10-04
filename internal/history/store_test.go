package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time {
	c.t = c.t.Add(time.Second)
	return c.t
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = (&fakeClock{t: time.Unix(1790000000, 0)}).now
	return s, path
}

func episode(title, ep string) Entry {
	return Entry{OTT: "nf", TitleID: title, SeasonID: "s1", EpisodeID: ep, Title: "Show " + title, EpLabel: "S1 E1", Poster: "p.jpg"}
}

func TestOpen_MissingFileIsEmpty(t *testing.T) {
	s, _ := newTestStore(t)
	if got := s.Continue(); got == nil || len(got) != 0 {
		t.Fatalf("Continue() = %#v, want empty non-nil", got)
	}
	if got := s.Prefs(); got != (Prefs{}) {
		t.Fatalf("Prefs() = %+v", got)
	}
}

func TestOpen_CorruptFileIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open succeeded on corrupt file")
	}
}

func TestRoundTripThroughDisk(t *testing.T) {
	s, path := newTestStore(t)
	s.Touch(episode("t1", "e1"))
	s.SavePosition("nf", "e1", 60_000, 1_000_000)
	s.SetPrefs(Prefs{Audio: "hin", Text: "eng", Quality: "720"})

	re, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	items := re.Continue()
	if len(items) != 1 || items[0].EpisodeID != "e1" || items[0].Pos != 60_000 || items[0].Dur != 1_000_000 {
		t.Fatalf("reloaded Continue() = %+v", items)
	}
	if got := re.Prefs(); got != (Prefs{Audio: "hin", Text: "eng", Quality: "720"}) {
		t.Fatalf("reloaded Prefs() = %+v", got)
	}
}

func TestSave_LeavesOnlyTheStateFile(t *testing.T) {
	s, path := newTestStore(t)
	s.SavePosition("nf", "e1", 1, 2)
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("dir has %v, want only state.json", entries)
	}
	b, _ := os.ReadFile(path)
	if !json.Valid(b) {
		t.Fatalf("state.json is not valid JSON: %s", b)
	}
}

func TestTouch_MovesTitleToFrontWithoutDuplicating(t *testing.T) {
	s, _ := newTestStore(t)
	s.Touch(episode("a", "a1"))
	s.Touch(episode("b", "b1"))
	s.Touch(episode("a", "a2"))

	items := s.Continue()
	if len(items) != 2 || items[0].TitleID != "a" || items[0].EpisodeID != "a2" || items[1].TitleID != "b" {
		t.Fatalf("Continue() = %+v", items)
	}
}

func TestTouch_CapsRecentAt20(t *testing.T) {
	s, _ := newTestStore(t)
	for i := range 25 {
		s.Touch(episode(fmt.Sprint(i), "e"))
	}
	items := s.Continue()
	if len(items) != 20 || items[0].TitleID != "24" || items[19].TitleID != "5" {
		t.Fatalf("len %d, first %s, last %s", len(items), items[0].TitleID, items[len(items)-1].TitleID)
	}
}

func TestSavePosition_CapsAt500DroppingOldest(t *testing.T) {
	s, _ := newTestStore(t)
	for i := range 505 {
		s.SavePosition("nf", fmt.Sprint(i), 10, 100)
	}
	if _, ok := s.position("nf", "4"); ok {
		t.Error("oldest positions were not evicted")
	}
	if _, ok := s.position("nf", "5"); !ok {
		t.Error("position 5 evicted, want kept")
	}
	if n := len(s.data.Positions); n != 500 {
		t.Errorf("positions = %d, want 500", n)
	}
}

func TestStartPosition(t *testing.T) {
	s, _ := newTestStore(t)
	s.SavePosition("nf", "half", 500, 1000)
	s.SavePosition("nf", "done", 950, 1000)
	cases := map[string]int64{"half": 500, "done": 0, "never": 0}
	for id, want := range cases {
		if got := s.StartPosition("nf", id); got != want {
			t.Errorf("StartPosition(%s) = %d, want %d", id, got, want)
		}
	}
}

func TestResume(t *testing.T) {
	s, _ := newTestStore(t)
	s.Touch(episode("t1", "e3"))
	s.SavePosition("nf", "e3", 400, 1000)

	r := s.Resume("nf", "t1")
	if r == nil || r.EpisodeID != "e3" || r.SeasonID != "s1" || r.EpLabel != "S1 E1" || r.Pos != 400 || r.Dur != 1000 {
		t.Fatalf("Resume = %+v", r)
	}
	if s.Resume("nf", "other") != nil || s.Resume("pv", "t1") != nil {
		t.Fatal("Resume matched the wrong title or OTT")
	}
}

func TestForget(t *testing.T) {
	s, _ := newTestStore(t)
	s.Touch(episode("t1", "e1"))
	s.Forget("nf", "t1")
	if len(s.Continue()) != 0 {
		t.Fatalf("Continue() = %+v after Forget", s.Continue())
	}
}

func TestSave_FileIsReadableFromShell(t *testing.T) {
	s, path := newTestStore(t)
	s.SavePosition("nf", "e1", 1, 2)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestBookmarksPersistIndependentlyOfWatching(t *testing.T) {
	s, path := newTestStore(t)
	s.SavePosition("anime", "1:sub:1", 100, 1000)
	for _, ott := range []string{"nf", "pv", "hs", "anime"} {
		b := Bookmark{OTT: ott, TitleID: "1", Title: "Saved title", Poster: "poster.jpg"}
		if err := s.SetBookmark(b, true); err != nil {
			t.Fatal(err)
		}
		if err := s.SetBookmark(b, true); err != nil {
			t.Fatal(err)
		}
		s.Forget(ott, "1")
	}
	if len(s.Bookmarks()) != 4 {
		t.Fatal("duplicates or platform collision")
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Bookmarks()) != 4 {
		t.Fatal("bookmarks lost after restart")
	}
	if p, ok := reopened.Position("anime", "1:sub:1"); !ok || p.Pos != 100 {
		t.Fatal("bookmark write lost progress")
	}
	copy := reopened.Bookmarks()
	copy[0].Title = "changed"
	if reopened.Bookmarks()[0].Title == "changed" {
		t.Fatal("exposed internal slice")
	}
	for i := 0; i < 2; i++ {
		if err := reopened.SetBookmark(Bookmark{OTT: "anime", TitleID: "1"}, false); err != nil {
			t.Fatal(err)
		}
	}
	again, err := Open(path)
	if err != nil || len(again.Bookmarks()) != 3 {
		t.Fatal("remove did not persist", err)
	}
}
func TestBookmarkWriteFailureRollsBack(t *testing.T) {
	s, path := newTestStore(t)
	b := Bookmark{OTT: "anime", TitleID: "1", Title: "Title"}
	if err := s.SetBookmark(b, true); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(t.TempDir(), "missing", "state.json")
	if err := s.SetBookmark(b, false); err == nil {
		t.Fatal("swallowed disk failure")
	}
	if len(s.Bookmarks()) != 1 {
		t.Fatal("failed write changed in-memory bookmarks")
	}
	s.path = path
}

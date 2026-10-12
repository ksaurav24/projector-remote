package medialib

import (
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// File is one indexed video. Entries are replaced, never mutated, once
// published, so readers can hold them without locks.
type File struct {
	Rel     string   `json:"rel"`
	Size    int64    `json:"size"`
	ModTime int64    `json:"mtime"`
	Probe   *Probe   `json:"probe,omitempty"`
	Err     string   `json:"err,omitempty"`
	ExtSubs []ExtSub `json:"extSubs,omitempty"`
	// NFO is the Kodi-style sidecar for this movie or episode, if any.
	NFO *NFO `json:"nfo,omitempty"`
	// Thumb is a sidecar still or poster image (relative path), if any.
	Thumb string `json:"thumb,omitempty"`
	// Keyframes are video keyframe times in seconds from the start of the
	// file, filled in on demand for stream-copy playback and cached.
	Keyframes []float64 `json:"keyframes,omitempty"`
	// KeyDelay is how far keyframes' decode times run ahead of their
	// presentation times.
	KeyDelay float64 `json:"keyDelay,omitempty"`
}

// ID is a stable identifier for the file's current contents.
func (f *File) ID() string {
	return shortHash(f.Rel + "|" + strconv.FormatInt(f.Size, 10) + "|" + strconv.FormatInt(f.ModTime, 10))
}

type Config struct {
	Root     string
	CacheDir string
	FFprobe  string
	// Settle is how long a file must go without writes before it's
	// indexed, so downloads in progress aren't probed half-written.
	Settle time.Duration
	// OnIndexed is called (off the library's locks) with files the
	// watcher just added or changed. The startup scan doesn't call it.
	OnIndexed func([]*File)
}

// Library is the indexed media folder. The directory is read once at
// startup and then kept current by watching it; requests only ever read
// the in-memory snapshot.
type Library struct {
	cfg Config

	mu    sync.Mutex
	files map[string]*File
	snap  atomic.Pointer[Catalog]

	saveMu sync.Mutex
	dirty  atomic.Bool
}

func Open(cfg Config) (*Library, error) {
	if cfg.Settle == 0 {
		cfg.Settle = 5 * time.Second
	}
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, err
	}
	cfg.Root = root
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, errors.New("medialib: media folder " + root + " is not a directory")
	}
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		return nil, err
	}
	l := &Library{cfg: cfg, files: map[string]*File{}}
	l.loadCache()
	l.rebuild()
	return l, nil
}

func (l *Library) Root() string { return l.cfg.Root }

// Abs returns the absolute path of a library-relative path.
func (l *Library) Abs(rel string) string { return filepath.Join(l.cfg.Root, filepath.FromSlash(rel)) }

// Catalog is the current snapshot.
func (l *Library) Catalog() *Catalog { return l.snap.Load() }

// File returns the indexed file at rel.
func (l *Library) File(rel string) *File {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.files[rel]
}

// SetKeyframes stores a file's keyframe index so it's computed only once.
func (l *Library) SetKeyframes(rel string, kf []float64, delay float64) {
	l.mu.Lock()
	f := l.files[rel]
	if f != nil {
		c := *f
		c.Keyframes, c.KeyDelay = kf, delay
		l.files[rel] = &c
	}
	l.mu.Unlock()
	if f != nil {
		l.dirty.Store(true)
	}
}

func (l *Library) cachePath() string { return filepath.Join(l.cfg.CacheDir, "index.json.gz") }

// indexVersion changes whenever File or Probe gains information, so an
// older cache is reprobed rather than trusted.
const indexVersion = 4

type indexCache struct {
	Version int              `json:"version"`
	Files   map[string]*File `json:"files"`
}

func (l *Library) loadCache() {
	f, err := os.Open(l.cachePath())
	if err != nil {
		return
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		log.Printf("medialib: index cache unreadable, rescanning: %v", err)
		return
	}
	var c indexCache
	if err := json.NewDecoder(zr).Decode(&c); err != nil {
		log.Printf("medialib: index cache unreadable, rescanning: %v", err)
		return
	}
	if c.Version != indexVersion || c.Files == nil {
		log.Printf("medialib: index cache is from an older version, rescanning")
		return
	}
	l.files = c.Files
}

// Save writes the index cache if anything changed since the last save.
func (l *Library) Save() {
	if !l.dirty.Swap(false) {
		return
	}
	l.saveMu.Lock()
	defer l.saveMu.Unlock()
	l.mu.Lock()
	files := make(map[string]*File, len(l.files))
	for k, v := range l.files {
		files[k] = v
	}
	l.mu.Unlock()
	tmp := l.cachePath() + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		log.Printf("medialib: save index: %v", err)
		return
	}
	zw := gzip.NewWriter(f)
	err = json.NewEncoder(zw).Encode(indexCache{Version: indexVersion, Files: files})
	if err == nil {
		err = zw.Close()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, l.cachePath())
	}
	if err != nil {
		log.Printf("medialib: save index: %v", err)
	}
}

type found struct {
	rel     string
	size    int64
	modTime int64
}

// walk lists every video under dir (absolute), skipping hidden folders.
func (l *Library) walk(dir string) []found {
	var out []found
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && isHidden(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !IsVideo(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(l.cfg.Root, p)
		out = append(out, found{rel: filepath.ToSlash(rel), size: info.Size(), modTime: info.ModTime().Unix()})
		return nil
	})
	return out
}

// Scan reads the whole media folder: new or changed files are probed,
// missing ones dropped. Run once at startup; Watch keeps it current.
func (l *Library) Scan(ctx context.Context) {
	start := time.Now()
	all := l.walk(l.cfg.Root)
	seen := make(map[string]bool, len(all))
	var todo []found
	l.mu.Lock()
	for _, f := range all {
		seen[f.rel] = true
		if old := l.files[f.rel]; old == nil || old.Size != f.size || old.ModTime != f.modTime {
			todo = append(todo, f)
		}
	}
	removed := 0
	for rel := range l.files {
		if !seen[rel] {
			delete(l.files, rel)
			removed++
		}
	}
	l.mu.Unlock()
	if removed > 0 {
		l.dirty.Store(true)
	}
	log.Printf("medialib: %d videos in %s, %d to probe, %d removed", len(all), l.cfg.Root, len(todo), removed)
	l.index(ctx, todo)
	// Sidecars (subtitles, nfo, thumbnails) are cheap to re-read and may
	// have changed while the server was down.
	l.refreshSidecars(func(string) bool { return true })
	l.rebuild()
	l.Save()
	log.Printf("medialib: indexed in %s", time.Since(start).Round(time.Millisecond))
}

// index probes files concurrently and stores the results.
func (l *Library) index(ctx context.Context, todo []found) []*File {
	if len(todo) == 0 {
		return nil
	}
	workers := min(max(runtime.NumCPU()/2, 2), 8)
	jobs := make(chan found)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var out []*File
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				entry := &File{Rel: f.rel, Size: f.size, ModTime: f.modTime}
				pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				p, err := ProbeFile(pctx, l.cfg.FFprobe, l.Abs(f.rel))
				cancel()
				if err != nil {
					entry.Err = err.Error()
					log.Printf("medialib: %v", err)
				} else {
					entry.Probe = p
				}
				l.readSidecars(entry)
				mu.Lock()
				out = append(out, entry)
				mu.Unlock()
			}
		}()
	}
	for _, f := range todo {
		select {
		case jobs <- f:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	l.mu.Lock()
	for _, f := range out {
		l.files[f.Rel] = f
	}
	l.mu.Unlock()
	l.dirty.Store(true)
	return out
}

func (l *Library) readSidecars(f *File) {
	abs := l.Abs(f.Rel)
	f.ExtSubs = nil
	for _, s := range FindExtSubs(abs) {
		rel, _ := filepath.Rel(l.cfg.Root, s.Path)
		s.Path = filepath.ToSlash(rel)
		f.ExtSubs = append(f.ExtSubs, s)
	}
	stem := strings.TrimSuffix(abs, filepath.Ext(abs))
	f.NFO = nil
	if n, err := ReadNFO(stem + ".nfo"); err == nil {
		f.NFO = n
	}
	f.Thumb = ""
	for _, c := range []string{"-thumb.jpg", "-thumb.png", ".jpg", ".png", ".tbn"} {
		if _, err := os.Stat(stem + c); err == nil {
			rel, _ := filepath.Rel(l.cfg.Root, stem+c)
			f.Thumb = filepath.ToSlash(rel)
			break
		}
	}
}

// refreshSidecars re-reads sidecars for indexed files whose folder
// matches.
func (l *Library) refreshSidecars(match func(dir string) bool) {
	l.mu.Lock()
	var targets []*File
	for _, f := range l.files {
		if match(pathDir(f.Rel)) {
			targets = append(targets, f)
		}
	}
	l.mu.Unlock()
	for _, f := range targets {
		c := *f
		l.readSidecars(&c)
		l.mu.Lock()
		if l.files[f.Rel] == f {
			l.files[f.Rel] = &c
		}
		l.mu.Unlock()
	}
	if len(targets) > 0 {
		l.dirty.Store(true)
	}
}

func pathDir(rel string) string {
	d := filepath.ToSlash(filepath.Dir(rel))
	if d == "." {
		return ""
	}
	return d
}

func (l *Library) rebuild() {
	l.mu.Lock()
	files := make([]*File, 0, len(l.files))
	for _, f := range l.files {
		if f.Probe != nil {
			files = append(files, f)
		}
	}
	l.mu.Unlock()
	l.snap.Store(buildCatalog(l.cfg.Root, files))
}

// Watch keeps the index current until ctx ends.
func (l *Library) Watch(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	addTree := func(dir string) {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if p != l.cfg.Root && isHidden(d.Name()) {
				return filepath.SkipDir
			}
			if err := w.Add(p); err != nil {
				log.Printf("medialib: watch %s: %v", p, err)
			}
			return nil
		})
	}
	addTree(l.cfg.Root)

	// Paths with pending events and when they last changed. A path is
	// processed once it has been quiet for Settle.
	pending := map[string]time.Time{}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	saveTick := time.NewTicker(30 * time.Second)
	defer saveTick.Stop()
	for {
		select {
		case <-ctx.Done():
			l.Save()
			return nil
		case err := <-w.Errors:
			log.Printf("medialib: watch: %v", err)
		case ev := <-w.Events:
			if ev.Op&fsnotify.Create != 0 {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() && !isHidden(filepath.Base(ev.Name)) {
					addTree(ev.Name)
				}
			}
			pending[ev.Name] = time.Now()
		case <-tick.C:
			var ready []string
			for p, t := range pending {
				if time.Since(t) >= l.cfg.Settle {
					ready = append(ready, p)
					delete(pending, p)
				}
			}
			if len(ready) > 0 {
				l.apply(ctx, ready)
			}
		case <-saveTick.C:
			l.Save()
		}
	}
}

// apply processes settled filesystem changes.
func (l *Library) apply(ctx context.Context, paths []string) {
	var todo []found
	sidecarDirs := map[string]bool{}
	removedAny := false
	for _, abs := range paths {
		rel, err := filepath.Rel(l.cfg.Root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		st, err := os.Stat(abs)
		switch {
		case err != nil:
			// Gone: a file, or a whole folder.
			l.mu.Lock()
			for r := range l.files {
				if r == rel || strings.HasPrefix(r, rel+"/") {
					delete(l.files, r)
					removedAny = true
				}
			}
			l.mu.Unlock()
			sidecarDirs[pathDir(rel)] = true
		case st.IsDir():
			for _, f := range l.walk(abs) {
				if old := l.File(f.rel); old == nil || old.Size != f.size || old.ModTime != f.modTime {
					todo = append(todo, f)
				}
			}
		case IsVideo(abs):
			if old := l.File(rel); old == nil || old.Size != st.Size() || old.ModTime != st.ModTime().Unix() {
				todo = append(todo, found{rel: rel, size: st.Size(), modTime: st.ModTime().Unix()})
			}
		default:
			// Subtitles, nfo files and artwork belong to videos in the same
			// folder or the folder above (Subs/).
			d := pathDir(rel)
			sidecarDirs[d] = true
			sidecarDirs[pathDir(d)] = true
		}
	}
	if removedAny {
		l.dirty.Store(true)
	}
	// A new folder and the files in it arrive as separate events.
	seen := map[string]bool{}
	unique := todo[:0]
	for _, f := range todo {
		if !seen[f.rel] {
			seen[f.rel] = true
			unique = append(unique, f)
		}
	}
	indexed := l.index(ctx, unique)
	if len(sidecarDirs) > 0 {
		l.refreshSidecars(func(d string) bool { return sidecarDirs[d] })
	}
	l.rebuild()
	for _, f := range indexed {
		log.Printf("medialib: indexed %s", f.Rel)
	}
	if l.cfg.OnIndexed != nil && len(indexed) > 0 {
		l.cfg.OnIndexed(indexed)
	}
}

func shortHash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:6])
}

// sortedKeys returns m's keys in ascending order.
func sortedKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

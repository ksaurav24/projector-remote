package mediastream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"projectorremote/internal/medialib"
)

var ErrUnknownStream = errors.New("mediastream: unknown stream")

const (
	// A request this many segments past what the running job has made
	// waits for it; anything further restarts ffmpeg there (a seek).
	waitAheadCopy      = 30
	waitAheadTranscode = 4
	// ffmpeg is paused once it's this far ahead of the player, so a long
	// movie doesn't fill the disk (or heat the GPU) for nothing.
	pauseAhead  = 45
	resumeAhead = 30
	// A job nobody has asked anything of for this long is stopped.
	idleJob      = 45 * time.Second
	segmentWait  = 90 * time.Second
	pollInterval = 50 * time.Millisecond
)

// Library is what the manager needs from the media index.
type Library interface {
	Abs(rel string) string
	File(rel string) *medialib.File
	SetKeyframes(rel string, kf []float64, delay float64)
}

type Manager struct {
	FFmpeg, FFprobe string
	Enc             Encoder
	Opt             Options
	Dir             string
	// CacheLimit is the most disk the segment cache may use (bytes).
	CacheLimit int64
	Lib        Library

	mu      sync.Mutex
	streams map[string]*stream
	jobSeq  int

	kfMu    sync.Mutex
	kfLocks map[string]*sync.Mutex
}

type stream struct {
	key   string
	dir   string
	src   string
	rel   string
	probe *medialib.Probe
	plan  *Plan

	// Jobs, requests and progress are per group: each video quality
	// ("v0", "v1", ...) is made by its own ffmpeg job, all audio tracks
	// ("a") by one more, so the player switching quality starts only the
	// new quality's encode.
	mu      sync.Mutex
	jobs    map[string]*job
	lastReq map[string]time.Time
	maxReq  map[string]int
	used    time.Time
}

func newStream(key, dir, src, rel string, probe *medialib.Probe, plan *Plan) *stream {
	return &stream{key: key, dir: dir, src: src, rel: rel, probe: probe, plan: plan,
		jobs: map[string]*job{}, lastReq: map[string]time.Time{}, maxReq: map[string]int{}}
}

type job struct {
	id       int
	group    string
	rends    []string
	start    int
	cmd      *exec.Cmd
	done     chan struct{}
	err      error
	stderr   bytes.Buffer
	produced int // highest segment finished in every rendition; start-1 before the first
	paused   bool
	stopped  bool
}

// state is what's saved beside a stream's segments so its URLs keep
// working across server restarts.
type state struct {
	Rel    string    `json:"rel"`
	Plan   *Plan     `json:"plan"`
	Bounds []float64 `json:"bounds"`
}

// Init clears segments left half-written by a previous run and starts the
// background housekeeping.
func (m *Manager) Init(ctx context.Context) error {
	m.streams = map[string]*stream{}
	m.kfLocks = map[string]*sync.Mutex{}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return err
	}
	partial, _ := filepath.Glob(filepath.Join(m.Dir, "*", "*", "j*"))
	for _, p := range partial {
		_ = os.Remove(p)
	}
	go m.housekeep(ctx)
	return nil
}

// Prepare plans the stream for one file and device and returns its key.
func (m *Manager) Prepare(ctx context.Context, f *medialib.File, caps Caps) (string, *Plan, error) {
	if f.Probe == nil || f.Probe.Video == nil {
		return "", nil, fmt.Errorf("mediastream: %s has no playable video", f.Rel)
	}
	var kf []float64
	var delay float64
	if NeedsKeyframes(f.Probe) {
		var err error
		if kf, delay, err = m.keyframes(ctx, f); err != nil {
			// Not fatal: the file is transcoded instead of copied.
			log.Printf("mediastream: %s: %v", f.Rel, err)
		}
	}
	plan := Decide(f.Probe, caps, m.Opt, kf, delay)
	key := plan.Key(f.ID())
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.streams[key]; !ok {
		s := newStream(key, filepath.Join(m.Dir, key), m.Lib.Abs(f.Rel), f.Rel, f.Probe, &plan)
		if err := os.MkdirAll(s.dir, 0o755); err != nil {
			return "", nil, err
		}
		raw, _ := json.Marshal(state{Rel: f.Rel, Plan: &plan, Bounds: plan.Bounds})
		_ = os.WriteFile(filepath.Join(s.dir, "stream.json"), raw, 0o644)
		m.streams[key] = s
	}
	var modes []string
	for _, v := range plan.Variants {
		if v.Copy {
			modes = append(modes, "copy")
		} else {
			modes = append(modes, strconv.Itoa(v.Height)+"p")
		}
	}
	mode := strings.Join(modes, " + ")
	if plan.Tonemap {
		mode += ", tonemapped"
	}
	log.Printf("mediastream: %s: video %s (%s), %d audio, %d segments", f.Rel, mode, m.Enc.Name, len(plan.Audio), plan.Segments())
	return key, &plan, nil
}

// keyframes returns f's cached keyframe index, computing it once.
func (m *Manager) keyframes(ctx context.Context, f *medialib.File) ([]float64, float64, error) {
	if len(f.Keyframes) > 0 {
		return f.Keyframes, f.KeyDelay, nil
	}
	m.kfMu.Lock()
	l := m.kfLocks[f.Rel]
	if l == nil {
		l = &sync.Mutex{}
		m.kfLocks[f.Rel] = l
	}
	m.kfMu.Unlock()
	l.Lock()
	defer l.Unlock()
	if cur := m.Lib.File(f.Rel); cur != nil && cur.ID() == f.ID() && len(cur.Keyframes) > 0 {
		return cur.Keyframes, cur.KeyDelay, nil
	}
	start := time.Now()
	kf, delay, err := Keyframes(ctx, m.FFprobe, m.Lib.Abs(f.Rel), f.Probe)
	if err != nil {
		return nil, 0, err
	}
	log.Printf("mediastream: %s: %d keyframes indexed in %s", f.Rel, len(kf), time.Since(start).Round(time.Millisecond))
	m.Lib.SetKeyframes(f.Rel, kf, delay)
	return kf, delay, nil
}

// Warm computes what a first play would otherwise wait for.
func (m *Manager) Warm(ctx context.Context, f *medialib.File) {
	if f.Probe != nil && NeedsKeyframes(f.Probe) {
		if _, _, err := m.keyframes(ctx, f); err != nil {
			log.Printf("mediastream: warm %s: %v", f.Rel, err)
		}
	}
}

func (m *Manager) stream(key string) (*stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.streams[key]; s != nil {
		return s, nil
	}
	// A stream from before a restart: its plan is on disk.
	if strings.ContainsAny(key, "/\\.") {
		return nil, ErrUnknownStream
	}
	dir := filepath.Join(m.Dir, key)
	raw, err := os.ReadFile(filepath.Join(dir, "stream.json"))
	if err != nil {
		return nil, ErrUnknownStream
	}
	var st state
	if json.Unmarshal(raw, &st) != nil || st.Plan == nil {
		return nil, ErrUnknownStream
	}
	f := m.Lib.File(st.Rel)
	if f == nil || f.Probe == nil || !strings.HasPrefix(key, f.ID()+"-") {
		return nil, ErrUnknownStream
	}
	st.Plan.Bounds = st.Bounds
	s := newStream(key, dir, m.Lib.Abs(st.Rel), st.Rel, f.Probe, st.Plan)
	m.streams[key] = s
	return s, nil
}

func (s *stream) renditions() []string {
	var r []string
	for i := range s.plan.Variants {
		r = append(r, "v"+strconv.Itoa(i))
	}
	return append(r, s.groupRends("a")...)
}

// group is the job that makes rendition r.
func group(r string) string {
	if strings.HasPrefix(r, "v") {
		return r
	}
	return "a"
}

func (s *stream) groupRends(g string) []string {
	if g != "a" {
		return []string{g}
	}
	var r []string
	for i := range s.plan.Audio {
		r = append(r, "a"+strconv.Itoa(i))
	}
	return r
}

func (s *stream) lastUse() time.Time {
	t := s.used
	for _, r := range s.lastReq {
		if r.After(t) {
			t = r
		}
	}
	return t
}

func (s *stream) running() bool {
	for _, j := range s.jobs {
		if !j.finished() {
			return true
		}
	}
	return false
}

func segName(k int) string { return fmt.Sprintf("%05d.ts", k) }

// Master writes the multivariant playlist.
func (m *Manager) Master(key string) (string, error) {
	s, err := m.stream(key)
	if err != nil {
		return "", err
	}
	p, v := s.plan, s.probe.Video
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	for i, a := range p.Audio {
		def := "NO"
		if a.Default {
			def = "YES"
		}
		lang := ""
		if a.Src.Lang != "" {
			lang = fmt.Sprintf(`LANGUAGE="%s",`, a.Src.Lang)
		}
		fmt.Fprintf(&b, `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="%s",%sDEFAULT=%s,AUTOSELECT=YES,CHANNELS="%d",URI="a%d.m3u8"`+"\n",
			quoteSafe(a.Name), lang, def, a.Channels, i)
	}
	// Highest quality first: players start on the first variant listed
	// until they've measured the network.
	for i, vr := range p.Variants {
		w, h := v.Width, v.Height
		// BANDWIDTH is meant to be the peak; a copy's average plus headroom
		// approximates it.
		bw := s.probe.Bitrate * 3 / 2
		if !vr.Copy {
			w, h = Fit(v.Width, v.Height, vr.Height)
			bw = int64(vr.Maxrate)
		}
		if bw <= 0 {
			bw = 8_000_000
		}
		bw += audioPeak(p.Audio)
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d", bw, w, h)
		if v.FPS > 0 {
			fmt.Fprintf(&b, ",FRAME-RATE=%.3f", v.FPS)
		}
		if len(p.Audio) > 0 {
			b.WriteString(`,AUDIO="audio"`)
		}
		fmt.Fprintf(&b, "\nv%d.m3u8\n", i)
	}
	return b.String(), nil
}

// audioPeak is the bitrate of the heaviest audio track.
func audioPeak(audio []AudioOut) int64 {
	var peak int64
	for _, a := range audio {
		br := int64(640_000)
		if !a.Copy {
			br = int64(audioBits(a.Channels))
		}
		peak = max(peak, br)
	}
	return peak
}

func quoteSafe(s string) string { return strings.NewReplacer(`"`, "'", "\n", " ").Replace(s) }

// Media writes one rendition's playlist. It's the whole file from the
// start: segments that don't exist yet are made when asked for.
func (m *Manager) Media(key, rendition string) (string, error) {
	s, err := m.stream(key)
	if err != nil {
		return "", err
	}
	if !s.hasRendition(rendition) {
		return "", ErrUnknownStream
	}
	bounds := s.plan.Bounds
	target := 1.0
	for i := 0; i+1 < len(bounds); i++ {
		target = max(target, bounds[i+1]-bounds[i])
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n", int(math.Ceil(target)))
	for i := 0; i+1 < len(bounds); i++ {
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s/%s\n", bounds[i+1]-bounds[i], rendition, segName(i))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String(), nil
}

func (s *stream) hasRendition(r string) bool {
	for _, x := range s.renditions() {
		if x == r {
			return true
		}
	}
	return false
}

// Segment returns the path of a finished segment, making it first if
// needed.
func (m *Manager) Segment(ctx context.Context, key, rendition string, k int) (string, error) {
	s, err := m.stream(key)
	if err != nil {
		return "", err
	}
	if !s.hasRendition(rendition) || k < 0 || k >= s.plan.Segments() {
		return "", ErrUnknownStream
	}
	path := filepath.Join(s.dir, rendition, segName(k))
	g := group(rendition)

	s.mu.Lock()
	s.lastReq[g] = time.Now()
	if cur, ok := s.maxReq[g]; !ok || k > cur {
		s.maxReq[g] = k
	}
	if j := s.jobs[g]; j != nil && j.paused && j.produced-s.maxReq[g] < resumeAhead {
		j.resume()
	}
	if exists(path) {
		s.mu.Unlock()
		touch(s.dir)
		return path, nil
	}
	window := waitAheadTranscode
	if g == "a" || s.plan.Variants[variantIndex(g)].Copy {
		window = waitAheadCopy
	}
	j := s.jobs[g]
	if j == nil || j.finished() || k < j.start || k > j.produced+1+window {
		m.startJob(s, g, k)
		j = s.jobs[g]
	}
	s.mu.Unlock()

	deadline := time.NewTimer(segmentWait)
	defer deadline.Stop()
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		if exists(path) {
			return path, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("mediastream: segment %d of %s timed out", k, s.rel)
		case <-j.done:
			if exists(path) {
				return path, nil
			}
			if j.stopped {
				// Superseded by a newer job (another seek); follow it.
				s.mu.Lock()
				nj := s.jobs[g]
				if nj == j || nj == nil {
					m.startJob(s, g, k)
					nj = s.jobs[g]
				}
				j = nj
				s.mu.Unlock()
				continue
			}
			return "", fmt.Errorf("mediastream: ffmpeg failed on %s: %v: %s", s.rel, j.err, strings.TrimSpace(j.stderr.String()))
		case <-tick.C:
		}
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// touch marks a stream as recently used for cache eviction, at most once
// a minute.
func touch(dir string) {
	p := filepath.Join(dir, "stream.json")
	if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) > time.Minute {
		now := time.Now()
		_ = os.Chtimes(p, now, now)
	}
}

func (j *job) finished() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

func (j *job) pause() {
	if j.cmd.Process != nil && j.cmd.Process.Signal(syscall.SIGSTOP) == nil {
		j.paused = true
	}
}

func (j *job) resume() {
	if j.cmd.Process != nil && j.cmd.Process.Signal(syscall.SIGCONT) == nil {
		j.paused = false
	}
}

// stop ends a job. Called with the stream's lock held.
func (j *job) stop() {
	if j.finished() {
		return
	}
	j.stopped = true
	if j.cmd.Process != nil {
		_ = j.cmd.Process.Signal(syscall.SIGCONT)
		_ = j.cmd.Process.Kill()
	}
}

func variantIndex(g string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(g, "v"))
	return n
}

// startJob replaces group g's job with one starting at segment k.
// Called with s.mu held.
func (m *Manager) startJob(s *stream, g string, k int) {
	if old := s.jobs[g]; old != nil {
		old.stop()
	}
	m.stopOthers(s)
	m.mu.Lock()
	m.jobSeq++
	id := m.jobSeq
	m.mu.Unlock()

	rends := s.groupRends(g)
	for _, r := range rends {
		_ = os.MkdirAll(filepath.Join(s.dir, r), 0o755)
	}
	spec := jobSpec{src: s.src, probe: s.probe, plan: s.plan, enc: m.Enc, start: k, group: g,
		out: func(r string) (string, string) {
			d := filepath.Join(s.dir, r)
			return filepath.Join(d, fmt.Sprintf("j%d_%%05d.ts", id)), filepath.Join(d, fmt.Sprintf("j%d.csv", id))
		}}
	j := &job{id: id, group: g, rends: rends, start: k, produced: k - 1, done: make(chan struct{})}
	j.cmd = exec.Command(m.FFmpeg, spec.args()...)
	j.cmd.Stderr = &limitedWriter{buf: &j.stderr, max: 8192}
	s.jobs[g] = j
	if err := j.cmd.Start(); err != nil {
		j.err = err
		close(j.done)
		return
	}
	log.Printf("mediastream: %s: ffmpeg job %d (%s) from segment %d", s.rel, id, g, k)
	go m.run(s, j)
}

// stopOthers stops other streams' jobs once the player has moved on to a
// new stream (next episode, a different title). Called with s.mu held;
// takes the other streams' locks only.
func (m *Manager) stopOthers(s *stream) {
	m.mu.Lock()
	others := make([]*stream, 0, len(m.streams))
	for _, o := range m.streams {
		if o != s {
			others = append(others, o)
		}
	}
	m.mu.Unlock()
	for _, o := range others {
		// Another stream busy starting its own job may be waiting on this
		// one's lock; skip it rather than deadlock (housekeeping stops it
		// once idle).
		if !o.mu.TryLock() {
			continue
		}
		if time.Since(o.lastUse()) > 5*time.Second {
			for _, j := range o.jobs {
				j.stop()
			}
		}
		o.mu.Unlock()
	}
}

// run follows a job: publishes each finished segment under its final
// name (so a half-written file is never served), keeps ffmpeg from
// running too far ahead, and cleans up when it exits.
func (m *Manager) run(s *stream, j *job) {
	exited := make(chan error, 1)
	go func() { exited <- j.cmd.Wait() }()
	rends := j.rends
	offsets := make(map[string]int64, len(rends))
	last := make(map[string]int, len(rends))
	for _, r := range rends {
		last[r] = j.start - 1
	}
	publish := func() {
		for _, r := range rends {
			d := filepath.Join(s.dir, r)
			f, err := os.Open(filepath.Join(d, fmt.Sprintf("j%d.csv", j.id)))
			if err != nil {
				continue
			}
			_, _ = f.Seek(offsets[r], 0)
			rd := bufio.NewReader(f)
			for {
				line, err := rd.ReadString('\n')
				if err != nil {
					break // a partial line is read again next time
				}
				offsets[r] += int64(len(line))
				name, _, _ := strings.Cut(line, ",")
				prefix := fmt.Sprintf("j%d_", j.id)
				if !strings.HasPrefix(name, prefix) {
					continue
				}
				final := strings.TrimPrefix(name, prefix)
				if err := os.Rename(filepath.Join(d, name), filepath.Join(d, final)); err == nil {
					if n, err := strconv.Atoi(strings.TrimSuffix(final, ".ts")); err == nil {
						last[r] = max(last[r], n)
					}
				}
			}
			f.Close()
		}
		produced := math.MaxInt
		for _, r := range rends {
			produced = min(produced, last[r])
		}
		s.mu.Lock()
		j.produced = produced
		if s.jobs[j.group] == j && !j.stopped {
			ahead := produced - s.maxReq[j.group]
			switch {
			case !j.paused && ahead > pauseAhead:
				j.pause()
			case j.paused && ahead < resumeAhead:
				j.resume()
			}
			// Everything just ahead already exists (an earlier run made
			// it): this run is only redoing work.
			if next := produced + 1; next < s.plan.Segments() && next > j.start {
				redundant := true
				for i := next; i < min(next+3, s.plan.Segments()); i++ {
					for _, r := range rends {
						if !exists(filepath.Join(s.dir, r, segName(i))) {
							redundant = false
						}
					}
				}
				if redundant {
					j.stop()
				}
			}
		}
		s.mu.Unlock()
	}

	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-exited:
			publish()
			s.mu.Lock()
			if !j.stopped {
				j.err = err
			}
			s.mu.Unlock()
			for _, r := range rends {
				leftover, _ := filepath.Glob(filepath.Join(s.dir, r, fmt.Sprintf("j%d[_.]*", j.id)))
				for _, p := range leftover {
					_ = os.Remove(p)
				}
			}
			if err != nil && !j.stopped {
				log.Printf("mediastream: %s: ffmpeg job %d failed: %v: %s", s.rel, j.id, err, strings.TrimSpace(j.stderr.String()))
			}
			close(j.done)
			return
		case <-tick.C:
			publish()
		}
	}
}

type limitedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// housekeep stops idle jobs and keeps the segment cache under its limit.
func (m *Manager) housekeep(ctx context.Context) {
	idle := time.NewTicker(10 * time.Second)
	defer idle.Stop()
	evict := time.NewTicker(5 * time.Minute)
	defer evict.Stop()
	m.evict()
	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			streams := make([]*stream, 0, len(m.streams))
			for _, s := range m.streams {
				streams = append(streams, s)
			}
			m.mu.Unlock()
			for _, s := range streams {
				s.mu.Lock()
				for _, j := range s.jobs {
					j.stop()
				}
				s.mu.Unlock()
			}
			return
		case <-idle.C:
			m.mu.Lock()
			streams := make([]*stream, 0, len(m.streams))
			for _, s := range m.streams {
				streams = append(streams, s)
			}
			m.mu.Unlock()
			for _, s := range streams {
				s.mu.Lock()
				for g, j := range s.jobs {
					if !j.finished() && time.Since(s.lastReq[g]) > idleJob {
						log.Printf("mediastream: %s: stopping idle ffmpeg job %d (%s)", s.rel, j.id, g)
						j.stop()
					}
				}
				s.mu.Unlock()
			}
		case <-evict.C:
			m.evict()
		}
	}
}

// evict deletes the least recently watched streams' segments until the
// cache fits its limit.
func (m *Manager) evict() {
	if m.CacheLimit <= 0 {
		return
	}
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return
	}
	type dirUse struct {
		key  string
		size int64
		used time.Time
	}
	var dirs []dirUse
	var total int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		d := dirUse{key: e.Name()}
		root := filepath.Join(m.Dir, e.Name())
		if st, err := os.Stat(filepath.Join(root, "stream.json")); err == nil {
			d.used = st.ModTime()
		}
		_ = filepath.WalkDir(root, func(_ string, de os.DirEntry, err error) error {
			if err == nil && !de.IsDir() {
				if info, err := de.Info(); err == nil {
					d.size += info.Size()
				}
			}
			return nil
		})
		total += d.size
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, k int) bool { return dirs[i].used.Before(dirs[k].used) })
	for _, d := range dirs {
		if total <= m.CacheLimit {
			break
		}
		m.mu.Lock()
		s := m.streams[d.key]
		m.mu.Unlock()
		if s != nil {
			s.mu.Lock()
			active := s.running()
			s.mu.Unlock()
			if active || time.Since(s.lastUse()) < 10*time.Minute {
				continue
			}
		}
		if err := os.RemoveAll(filepath.Join(m.Dir, d.key)); err == nil {
			total -= d.size
			m.mu.Lock()
			delete(m.streams, d.key)
			m.mu.Unlock()
			log.Printf("mediastream: evicted %s (%d MB)", d.key, d.size>>20)
		}
	}
}

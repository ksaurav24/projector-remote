package medialib

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Probe is what ffprobe found in a file, reduced to what playback
// decisions need.
type Probe struct {
	Duration float64 `json:"duration"`
	// StartTime is the first timestamp in the file. Most files start at
	// 0; MPEG-TS recordings often don't.
	StartTime float64       `json:"startTime"`
	Format    string        `json:"format"`
	Bitrate   int64         `json:"bitrate"`
	Video     *VideoStream  `json:"video,omitempty"`
	Audio     []AudioStream `json:"audio"`
	Subs      []SubStream   `json:"subs"`
}

type VideoStream struct {
	Index    int     `json:"index"`
	Codec    string  `json:"codec"`
	Profile  string  `json:"profile"`
	PixFmt   string  `json:"pixFmt"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	BitDepth int     `json:"bitDepth"`
	FPS      float64 `json:"fps"`
	// Transfer is the color transfer: "smpte2084" (HDR10/PQ) or
	// "arib-std-b67" (HLG) mean HDR.
	Transfer    string `json:"transfer"`
	Primaries   string `json:"primaries"`
	Matrix      string `json:"matrix"`
	DolbyVision bool   `json:"dolbyVision"`
	Interlaced  bool   `json:"interlaced"`
	// StartTime is when the stream's first frame is, in seconds from the
	// start of the file.
	StartTime float64 `json:"startTime"`
}

// HDR reports whether the picture needs tone mapping on an SDR display.
func (v *VideoStream) HDR() bool {
	return v.Transfer == "smpte2084" || v.Transfer == "arib-std-b67" || v.DolbyVision
}

type AudioStream struct {
	Index      int    `json:"index"`
	Codec      string `json:"codec"`
	Profile    string `json:"profile"`
	Channels   int    `json:"channels"`
	SampleRate int    `json:"sampleRate"`
	Lang       string `json:"lang"`
	Title      string `json:"title"`
	Default    bool   `json:"default"`
	// StartTime is the first packet's time from the start of the file,
	// which can be slightly negative (encoder priming).
	StartTime float64 `json:"startTime"`
}

type SubStream struct {
	Index   int    `json:"index"`
	Codec   string `json:"codec"`
	Lang    string `json:"lang"`
	Title   string `json:"title"`
	Default bool   `json:"default"`
	Forced  bool   `json:"forced"`
	SDH     bool   `json:"sdh"`
}

// TextSub reports whether a subtitle codec is text (convertible to
// WebVTT/SSA) rather than a bitmap format like PGS or VobSub.
func TextSub(codec string) bool {
	switch codec {
	case "subrip", "srt", "ass", "ssa", "webvtt", "mov_text", "text", "microdvd", "subviewer", "subviewer1", "sami", "realtext", "jacosub", "mpl2", "pjs", "vplayer", "stl":
		return true
	}
	return false
}

type ffprobeOut struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		StartTime  string `json:"start_time"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		Index         int               `json:"index"`
		StartTime     string            `json:"start_time"`
		CodecType     string            `json:"codec_type"`
		CodecName     string            `json:"codec_name"`
		Profile       string            `json:"profile"`
		PixFmt        string            `json:"pix_fmt"`
		Width         int               `json:"width"`
		Height        int               `json:"height"`
		Channels      int               `json:"channels"`
		SampleRate    string            `json:"sample_rate"`
		AvgFrameRate  string            `json:"avg_frame_rate"`
		RFrameRate    string            `json:"r_frame_rate"`
		ColorTransfer string            `json:"color_transfer"`
		ColorPrim     string            `json:"color_primaries"`
		ColorSpace    string            `json:"color_space"`
		FieldOrder    string            `json:"field_order"`
		BitsPerRaw    string            `json:"bits_per_raw_sample"`
		Tags          map[string]string `json:"tags"`
		Disposition   map[string]int    `json:"disposition"`
		SideData      []struct {
			Type string `json:"side_data_type"`
		} `json:"side_data_list"`
	} `json:"streams"`
}

// ProbeFile runs ffprobe on path.
func ProbeFile(ctx context.Context, ffprobe, path string) (*Probe, error) {
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("ffprobe %s: %s", filepath.Base(path), strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	p, err := parseProbe(out)
	if err != nil {
		return nil, err
	}
	// Streams' reported start times hide encoder priming: an audio track
	// "starting at 0" often has its first packet at -5 ms (AC3) or -23 ms
	// (AAC). Segment cuts are computed from the real first packet, so read
	// the first few packets (only the head of the file).
	pk, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "packet=stream_index,pts_time",
		"-read_intervals", "%+#200", "-of", "csv=p=0", path).Output()
	if err == nil {
		first := map[int]float64{}
		for line := range strings.Lines(string(pk)) {
			idx, ts, ok := strings.Cut(strings.TrimSpace(line), ",")
			i, err1 := strconv.Atoi(idx)
			t, err2 := strconv.ParseFloat(strings.Trim(ts, ","), 64)
			if !ok || err1 != nil || err2 != nil {
				continue
			}
			if old, seen := first[i]; !seen || t < old {
				first[i] = t
			}
		}
		for i := range p.Audio {
			if t, ok := first[p.Audio[i].Index]; ok {
				p.Audio[i].StartTime = t - p.StartTime
			}
		}
	}
	return p, nil
}

func parseProbe(raw []byte) (*Probe, error) {
	var o ffprobeOut
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("ffprobe output: %w", err)
	}
	p := &Probe{Format: o.Format.FormatName, Audio: []AudioStream{}, Subs: []SubStream{}}
	p.Duration, _ = strconv.ParseFloat(o.Format.Duration, 64)
	p.StartTime, _ = strconv.ParseFloat(o.Format.StartTime, 64)
	p.Bitrate, _ = strconv.ParseInt(o.Format.BitRate, 10, 64)
	for _, s := range o.Streams {
		lang := NormalizeLang(s.Tags["language"])
		start, _ := strconv.ParseFloat(s.StartTime, 64)
		start = max(start-p.StartTime, 0)
		title := s.Tags["title"]
		switch s.CodecType {
		case "video":
			// Cover art and thumbnails ride along as single-frame video
			// streams; they aren't the movie.
			if p.Video != nil || s.Disposition["attached_pic"] == 1 || s.CodecName == "mjpeg" || s.CodecName == "png" {
				continue
			}
			v := &VideoStream{Index: s.Index, Codec: s.CodecName, Profile: s.Profile, PixFmt: s.PixFmt,
				Width: s.Width, Height: s.Height, Transfer: s.ColorTransfer, Primaries: s.ColorPrim, Matrix: s.ColorSpace,
				Interlaced: s.FieldOrder != "" && s.FieldOrder != "progressive" && s.FieldOrder != "unknown", StartTime: start}
			v.FPS = frameRate(s.AvgFrameRate)
			if v.FPS <= 0 || v.FPS > 240 {
				v.FPS = frameRate(s.RFrameRate)
			}
			v.BitDepth = 8
			if strings.Contains(s.PixFmt, "10") || strings.Contains(s.PixFmt, "p010") {
				v.BitDepth = 10
			} else if strings.Contains(s.PixFmt, "12") {
				v.BitDepth = 12
			}
			for _, sd := range s.SideData {
				if strings.Contains(strings.ToLower(sd.Type), "dovi") || strings.Contains(sd.Type, "Dolby Vision") {
					v.DolbyVision = true
				}
			}
			p.Video = v
		case "audio":
			sr, _ := strconv.Atoi(s.SampleRate)
			p.Audio = append(p.Audio, AudioStream{Index: s.Index, Codec: s.CodecName, Profile: s.Profile, SampleRate: sr,
				Channels: s.Channels, Lang: lang, Title: title, Default: s.Disposition["default"] == 1, StartTime: start})
		case "subtitle":
			lt := strings.ToLower(title)
			p.Subs = append(p.Subs, SubStream{Index: s.Index, Codec: s.CodecName, Lang: lang, Title: title,
				Default: s.Disposition["default"] == 1,
				Forced:  s.Disposition["forced"] == 1 || strings.Contains(lt, "forced"),
				SDH:     s.Disposition["hearing_impaired"] == 1 || strings.Contains(lt, "sdh")})
		}
	}
	if p.Video == nil {
		return nil, fmt.Errorf("no video stream")
	}
	return p, nil
}

func frameRate(s string) float64 {
	n, d, ok := strings.Cut(s, "/")
	if !ok {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	num, _ := strconv.ParseFloat(n, 64)
	den, _ := strconv.ParseFloat(d, 64)
	if den == 0 {
		return 0
	}
	return num / den
}

// ExtSub is a subtitle file beside the video ("Movie.en.srt",
// "Movie.eng.forced.ass", "Subs/Movie/2_English.srt").
type ExtSub struct {
	Path   string `json:"path"`
	Codec  string `json:"codec"`
	Lang   string `json:"lang"`
	Title  string `json:"title"`
	Forced bool   `json:"forced"`
	SDH    bool   `json:"sdh"`
}

var subExts = map[string]string{".srt": "subrip", ".ass": "ass", ".ssa": "ssa", ".vtt": "webvtt", ".sub": "microdvd", ".smi": "sami"}

// FindExtSubs lists subtitle files that belong to the video at path.
func FindExtSubs(path string) []ExtSub {
	dir := filepath.Dir(path)
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	lstem := strings.ToLower(stem)
	var out []ExtSub
	add := func(p, tagPart string) {
		codec, ok := subExts[strings.ToLower(filepath.Ext(p))]
		if !ok {
			return
		}
		// .sub is also VobSub's bitmap half (paired with .idx); only text
		// MicroDVD .sub files are usable.
		if codec == "microdvd" {
			if _, err := os.Stat(strings.TrimSuffix(p, filepath.Ext(p)) + ".idx"); err == nil {
				return
			}
		}
		s := ExtSub{Path: p, Codec: codec}
		for _, tag := range strings.FieldsFunc(strings.ToLower(tagPart), func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == ' ' || r == '[' || r == ']' }) {
			switch tag {
			case "forced", "foreign":
				s.Forced = true
			case "sdh", "cc":
				s.SDH = true
			case "hi":
				// "Movie.en.hi.srt" is hearing impaired; "Movie.hi.srt" is Hindi.
				if s.Lang != "" {
					s.SDH = true
				} else {
					s.Lang = "hi"
				}
			case "default":
			default:
				if l := NormalizeLang(tag); l != "" && s.Lang == "" {
					s.Lang = l
				} else if _, err := strconv.Atoi(tag); err != nil && s.Title == "" {
					s.Title = tag
				}
			}
		}
		out = append(out, s)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			// Subs/ or Subtitles/ folders, flat or per-episode.
			ln := strings.ToLower(name)
			if ln != "subs" && ln != "subtitles" && ln != "sub" {
				continue
			}
			sdir := filepath.Join(dir, name)
			sub, _ := os.ReadDir(sdir)
			for _, se := range sub {
				sp := filepath.Join(sdir, se.Name())
				if se.IsDir() && strings.EqualFold(se.Name(), stem) {
					inner, _ := os.ReadDir(sp)
					for _, ie := range inner {
						add(filepath.Join(sp, ie.Name()), strings.TrimSuffix(ie.Name(), filepath.Ext(ie.Name())))
					}
					continue
				}
				ls := strings.ToLower(se.Name())
				if strings.HasPrefix(ls, lstem) {
					add(sp, strings.TrimSuffix(se.Name()[len(stem):], filepath.Ext(se.Name())))
				} else if len(entries) > 0 && onlyVideoIn(entries) {
					// A folder with one video: every subtitle is for it.
					add(sp, strings.TrimSuffix(se.Name(), filepath.Ext(se.Name())))
				}
			}
			continue
		}
		if strings.HasPrefix(strings.ToLower(name), lstem) && len(name) > len(stem) {
			add(filepath.Join(dir, name), strings.TrimSuffix(name[len(stem):], filepath.Ext(name)))
		}
	}
	return out
}

func onlyVideoIn(entries []os.DirEntry) bool {
	n := 0
	for _, e := range entries {
		if !e.IsDir() && IsVideo(e.Name()) {
			n++
		}
	}
	return n == 1
}

// Language codes and names as they appear in files and tags, mapped to
// the ISO 639-1 codes the player and its preferences use.
var langCodes = map[string]string{
	"en": "en", "eng": "en", "english": "en", "ja": "ja", "jpn": "ja", "jap": "ja", "japanese": "ja",
	"hi": "hi", "hin": "hi", "hindi": "hi", "es": "es", "spa": "es", "spanish": "es", "esp": "es", "lat": "es",
	"fr": "fr", "fre": "fr", "fra": "fr", "french": "fr", "de": "de", "ger": "de", "deu": "de", "german": "de",
	"it": "it", "ita": "it", "italian": "it", "pt": "pt", "por": "pt", "portuguese": "pt", "pob": "pt", "ptbr": "pt",
	"ru": "ru", "rus": "ru", "russian": "ru", "zh": "zh", "chi": "zh", "zho": "zh", "chinese": "zh", "chs": "zh", "cht": "zh",
	"ko": "ko", "kor": "ko", "korean": "ko", "ar": "ar", "ara": "ar", "arabic": "ar", "nl": "nl", "dut": "nl", "nld": "nl", "dutch": "nl",
	"sv": "sv", "swe": "sv", "swedish": "sv", "no": "no", "nor": "no", "norwegian": "no", "da": "da", "dan": "da", "danish": "da",
	"fi": "fi", "fin": "fi", "finnish": "fi", "pl": "pl", "pol": "pl", "polish": "pl", "tr": "tr", "tur": "tr", "turkish": "tr",
	"el": "el", "gre": "el", "ell": "el", "greek": "el", "he": "he", "heb": "he", "hebrew": "he", "th": "th", "tha": "th", "thai": "th",
	"vi": "vi", "vie": "vi", "vietnamese": "vi", "id": "id", "ind": "id", "indonesian": "id", "ms": "ms", "may": "ms", "msa": "ms", "malay": "ms",
	"ta": "ta", "tam": "ta", "tamil": "ta", "te": "te", "tel": "te", "telugu": "te", "ml": "ml", "mal": "ml", "malayalam": "ml",
	"kn": "kn", "kan": "kn", "kannada": "kn", "bn": "bn", "ben": "bn", "bengali": "bn", "mr": "mr", "mar": "mr", "marathi": "mr",
	"pa": "pa", "pan": "pa", "punjabi": "pa", "ur": "ur", "urd": "ur", "urdu": "ur", "gu": "gu", "guj": "gu", "gujarati": "gu",
	"cs": "cs", "cze": "cs", "ces": "cs", "czech": "cs", "hu": "hu", "hun": "hu", "hungarian": "hu", "ro": "ro", "rum": "ro", "ron": "ro", "romanian": "ro",
	"uk": "uk", "ukr": "uk", "ukrainian": "uk", "bg": "bg", "bul": "bg", "bulgarian": "bg", "hr": "hr", "hrv": "hr", "croatian": "hr",
	"sr": "sr", "srp": "sr", "serbian": "sr", "sk": "sk", "slo": "sk", "slk": "sk", "slovak": "sk", "sl": "sl", "slv": "sl", "slovenian": "sl",
	"fa": "fa", "per": "fa", "fas": "fa", "persian": "fa", "fil": "fil", "tgl": "fil", "tagalog": "fil", "filipino": "fil",
}

// NormalizeLang maps a language tag ("eng", "English", "pt-BR") to its
// ISO 639-1 code, or "" when it isn't a language ("und", "forced").
func NormalizeLang(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	if l == "" || l == "und" || l == "unk" || l == "mul" || l == "zxx" {
		return ""
	}
	if v, ok := langCodes[l]; ok {
		return v
	}
	if base, _, ok := strings.Cut(l, "-"); ok {
		return langCodes[base]
	}
	return ""
}

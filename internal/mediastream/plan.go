// Package mediastream turns local video files into HLS for the
// projector. Everything heavy happens here, on the machine with the
// files: the projector only plays the resulting playlists.
//
// Streams the projector can decode are copied into MPEG-TS segments
// untouched; anything else is transcoded (NVENC when available) to H.264
// and AAC. Segments are produced on demand by one ffmpeg process per
// stream, restarted at the requested segment on a seek, and kept on disk
// so a rewatch or a seek back costs nothing.
package mediastream

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"projectorremote/internal/medialib"
)

// Caps are what the projector's hardware decoders handle. The projector
// reads them from its codec list and sends them with each play request.
type Caps struct {
	Video []VideoCap `json:"video"`
	// Audio lists ffmpeg codec names ("aac", "ac3", "eac3", ...).
	Audio []string `json:"audio"`
	// HDR is true when the display shows HDR10/HLG; otherwise HDR
	// sources are tone-mapped to SDR.
	HDR bool `json:"hdr"`
}

type VideoCap struct {
	// Codec is the ffmpeg codec name ("h264", "hevc").
	Codec     string `json:"codec"`
	MaxWidth  int    `json:"maxWidth"`
	MaxHeight int    `json:"maxHeight"`
	TenBit    bool   `json:"tenBit"`
}

// DefaultCaps is what every Android TV device decodes: 1080p 8-bit
// H.264 with AAC or MP3. Used when the projector doesn't say.
func DefaultCaps() Caps {
	return Caps{Video: []VideoCap{{Codec: "h264", MaxWidth: 1920, MaxHeight: 1088}}, Audio: []string{"aac", "mp3"}}
}

func (c Caps) video(codec string) (VideoCap, bool) {
	for _, v := range c.Video {
		if v.Codec == codec {
			return v, true
		}
	}
	return VideoCap{}, false
}

// Options are the server's quality settings.
type Options struct {
	// MaxHeight caps the resolution of transcodes (copies keep the
	// source's).
	MaxHeight int
	// MaxCopyBitrate is the highest source bitrate (bits/s) streamed
	// as-is; heavier files are transcoded so Wi-Fi can keep up. 0 means
	// no limit.
	MaxCopyBitrate int64
	// SegmentSeconds is the target segment length.
	SegmentSeconds float64
}

// Plan is how one file will be streamed to one kind of device.
type Plan struct {
	// Variants are the video qualities offered, best first; the player
	// picks among them as the network allows.
	Variants []Variant  `json:"variants"`
	Tonemap  bool       `json:"tonemap"`
	Audio    []AudioOut `json:"audio"`
	// FirstKey is the source's first keyframe, where a copy starts, and
	// KeyDelay how far keyframes' decode times precede it.
	FirstKey float64 `json:"firstKey"`
	KeyDelay float64 `json:"keyDelay"`
	// Bounds are segment boundaries in seconds from the start of the
	// file: segment i covers [Bounds[i], Bounds[i+1]).
	Bounds []float64 `json:"-"`
}

// Variant is one video quality: the source copied, or a transcode.
type Variant struct {
	Copy   bool `json:"copy"`
	Height int  `json:"height,omitempty"`
	// Bitrate is the transcode's target (0: constant quality, capped at
	// Maxrate).
	Bitrate int `json:"bitrate,omitempty"`
	Maxrate int `json:"maxrate,omitempty"`
}

type AudioOut struct {
	Src      medialib.AudioStream `json:"src"`
	Copy     bool                 `json:"copy"`
	Channels int                  `json:"channels"`
	Name     string               `json:"name"`
	Default  bool                 `json:"default"`
}

// Segments is the number of segments.
func (p *Plan) Segments() int { return len(p.Bounds) - 1 }

// Key identifies the plan's output, so differently planned streams of
// the same file never share segments.
func (p *Plan) Key(fileID string) string {
	raw, _ := json.Marshal(p)
	h := sha1.Sum(append(raw, []byte(fmt.Sprint(len(p.Bounds), fileID))...))
	return fileID + "-" + hex.EncodeToString(h[:5])
}

// Audio codecs that can be copied into MPEG-TS and played by ExoPlayer.
var tsAudio = []string{"aac", "ac3", "eac3", "mp3"}

// NeedsKeyframes reports whether planning f may need its keyframe index
// (a stream copy cuts segments on the source's keyframes).
func NeedsKeyframes(p *medialib.Probe) bool {
	c := p.Video.Codec
	return c == "h264" || c == "hevc"
}

// Decide plans how to stream f to a device with caps. keyframes may be
// nil when the video won't be copied; keyDelay is their decode lead
// (see Keyframes).
func Decide(p *medialib.Probe, caps Caps, opt Options, keyframes []float64, keyDelay float64) Plan {
	v := p.Video
	var plan Plan
	copyOK := false
	if vc, ok := caps.video(v.Codec); ok && (v.Codec == "h264" || v.Codec == "hevc") &&
		v.Width <= vc.MaxWidth && v.Height <= vc.MaxHeight &&
		(v.BitDepth <= 8 || vc.TenBit) &&
		(!v.HDR() || caps.HDR) &&
		(opt.MaxCopyBitrate == 0 || p.Bitrate == 0 || p.Bitrate <= opt.MaxCopyBitrate) &&
		len(keyframes) > 1 {
		copyOK = true
	}
	// Transcodes are capped at the server's limit and the device's own (a
	// 1080p-only decoder can't take a 4K transcode). Heights here are of
	// 16:9 boxes the picture is fitted into, so a 1920x800 film counts
	// as 1080p.
	top := boxClass(v.Width, v.Height)
	if opt.MaxHeight > 0 && top > opt.MaxHeight {
		top = opt.MaxHeight
	}
	if vc, ok := caps.video("h264"); ok && vc.MaxHeight > 0 && top > vc.MaxHeight {
		top = vc.MaxHeight
	}
	top &^= 1
	if copyOK {
		plan.Variants = append(plan.Variants, Variant{Copy: true})
	} else {
		plan.Variants = append(plan.Variants, Variant{Height: top, Maxrate: maxrate(top)})
		plan.Tonemap = v.HDR() && !caps.HDR
	}
	// Lighter rungs for weak Wi-Fi, each made only if the player asks
	// for it. A copy's own resolution counts as the top rung.
	shown := top
	if copyOK {
		shown = boxClass(v.Width, v.Height)
	}
	for _, r := range ladder {
		if shown >= r.height*5/4 {
			plan.Variants = append(plan.Variants, Variant{Height: r.height, Bitrate: r.bitrate, Maxrate: r.bitrate * 3 / 2})
			plan.Tonemap = plan.Tonemap || (v.HDR() && !caps.HDR)
		}
	}

	defaultIx := slices.IndexFunc(p.Audio, func(a medialib.AudioStream) bool { return a.Default })
	if defaultIx < 0 {
		defaultIx = 0
	}
	names := map[string]int{}
	for i, a := range p.Audio {
		if i >= 8 {
			break
		}
		out := AudioOut{Src: a, Default: i == defaultIx, Channels: a.Channels}
		out.Copy = slices.Contains(tsAudio, a.Codec) && slices.Contains(caps.Audio, a.Codec) && a.Channels <= 8
		if !out.Copy {
			out.Channels = min(max(a.Channels, 1), 6)
		}
		out.Name = audioName(a)
		// Two "English 5.1" tracks need telling apart.
		names[out.Name]++
		if n := names[out.Name]; n > 1 {
			out.Name = fmt.Sprintf("%s (%d)", out.Name, n)
		}
		plan.Audio = append(plan.Audio, out)
	}

	seg := opt.SegmentSeconds
	if seg <= 0 {
		seg = 4
	}
	if copyOK {
		plan.FirstKey, plan.KeyDelay = keyframes[0], keyDelay
		plan.Bounds = keyframeBounds(keyframes, p.Duration, seg)
	} else {
		// A whole number of AAC frames, so transcoded audio from runs
		// started at different segments falls on one frame grid.
		plan.Bounds = fixedBounds(p.Duration, math.Round(seg/aacFrame)*aacFrame)
	}
	return plan
}

// boxClass is the height of the smallest 16:9 box a w x h picture fills.
func boxClass(w, h int) int { return max(h, w*9/16) }

// Fit scales a w x h picture to fit a 16:9 box of height boxH, keeping
// its shape, with even dimensions.
func Fit(w, h, boxH int) (int, int) {
	bw := (boxH*16/9 + 1) &^ 1
	if w <= bw && h <= boxH {
		return w &^ 1, h &^ 1
	}
	scale := min(float64(bw)/float64(w), float64(boxH)/float64(h))
	return int(math.Round(float64(w)*scale/2)) * 2, int(math.Round(float64(h)*scale/2)) * 2
}

// ladder is the reduced-quality rungs offered under the top quality.
var ladder = []struct{ height, bitrate int }{
	{1080, 6_000_000},
	{720, 3_000_000},
	{480, 1_200_000},
}

// fixedBounds splits a transcode into equal segments; the encoder is told
// to put a keyframe on every boundary.
func fixedBounds(dur, seg float64) []float64 {
	n := int(math.Ceil(dur / seg))
	if n < 1 {
		n = 1
	}
	b := make([]float64, 0, n+1)
	for i := 0; i < n; i++ {
		b = append(b, float64(i)*seg)
	}
	// A tail shorter than a second joins the segment before it.
	if len(b) > 1 && dur-b[len(b)-1] < 1 {
		b = b[:len(b)-1]
	}
	return append(b, dur)
}

// keyframeBounds groups the source's keyframes into segments of at least
// seg seconds; a copied stream can only be cut on its keyframes.
func keyframeBounds(kf []float64, dur, seg float64) []float64 {
	b := []float64{0}
	for _, t := range kf {
		if t-b[len(b)-1] >= seg && dur-t >= 1 {
			b = append(b, t)
		}
	}
	return append(b, dur)
}

var langNames = map[string]string{
	"en": "English", "ja": "Japanese", "hi": "Hindi", "es": "Spanish", "fr": "French", "de": "German",
	"it": "Italian", "pt": "Portuguese", "ru": "Russian", "zh": "Chinese", "ko": "Korean", "ar": "Arabic",
	"nl": "Dutch", "sv": "Swedish", "no": "Norwegian", "da": "Danish", "fi": "Finnish", "pl": "Polish",
	"tr": "Turkish", "el": "Greek", "he": "Hebrew", "th": "Thai", "vi": "Vietnamese", "id": "Indonesian",
	"ms": "Malay", "ta": "Tamil", "te": "Telugu", "ml": "Malayalam", "kn": "Kannada", "bn": "Bengali",
	"mr": "Marathi", "pa": "Punjabi", "ur": "Urdu", "gu": "Gujarati", "cs": "Czech", "hu": "Hungarian",
	"ro": "Romanian", "uk": "Ukrainian", "bg": "Bulgarian", "hr": "Croatian", "sr": "Serbian",
	"sk": "Slovak", "sl": "Slovenian", "fa": "Persian", "fil": "Filipino",
}

// LangName is a language's English name, or "" when unknown.
func LangName(code string) string { return langNames[code] }

func channelLayout(n int) string {
	switch n {
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	}
	return fmt.Sprintf("%dch", n)
}

// audioName labels a track for the remote's audio picker.
func audioName(a medialib.AudioStream) string {
	lang := LangName(a.Lang)
	title := strings.TrimSpace(a.Title)
	lt := strings.ToLower(title)
	// Titles that only restate the codec ("AC3 5.1", "Surround") add
	// nothing over the language.
	informative := title != "" && !strings.EqualFold(title, lang) &&
		!strings.HasPrefix(lt, "stereo") && !strings.HasPrefix(lt, "surround") && !strings.HasPrefix(lt, "ac3") &&
		!strings.HasPrefix(lt, "aac") && !strings.HasPrefix(lt, "dts") && !strings.HasPrefix(lt, "dolby") &&
		!strings.HasPrefix(lt, "truehd") && !strings.HasPrefix(lt, "flac") && !strings.HasPrefix(lt, "eac3")
	switch {
	case informative && lang != "":
		return lang + " · " + title
	case informative:
		return title
	case lang != "":
		return lang + " " + channelLayout(a.Channels)
	}
	return "Audio " + channelLayout(a.Channels)
}

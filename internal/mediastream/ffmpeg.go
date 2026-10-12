package mediastream

import (
	"context"
	"fmt"
	"log"
	"math"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"projectorremote/internal/medialib"
)

// Encoder is the H.264 encoder transcodes use.
type Encoder struct {
	Name string
	// CUDA is true for NVENC, which also lets decoding and scaling stay
	// on the GPU.
	CUDA bool
}

// DetectEncoder picks the best working H.264 encoder: NVENC, then Quick
// Sync, then x264. prefer forces one by name.
func DetectEncoder(ffmpeg, prefer string) Encoder {
	candidates := []string{"h264_nvenc", "h264_qsv", "libx264"}
	if prefer != "" {
		candidates = []string{prefer}
	}
	for _, name := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24", "-frames:v", "12"}
		if name == "h264_nvenc" {
			args = append([]string{"-init_hw_device", "cuda"}, args...)
		}
		args = append(args, "-c:v", name, "-f", "null", "-")
		out, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput()
		cancel()
		if err == nil {
			return Encoder{Name: name, CUDA: name == "h264_nvenc"}
		}
		log.Printf("mediastream: encoder %s unusable: %s", name, strings.TrimSpace(string(out)))
	}
	return Encoder{Name: "libx264"}
}

// nvdec reports whether NVIDIA's decoder handles v. Anything else is
// decoded on the CPU.
func nvdec(v *medialib.VideoStream) bool {
	switch v.Codec {
	case "h264":
		// No 10-bit ("Hi10P") or 4:2:2 H.264 on NVDEC.
		return v.PixFmt == "yuv420p" || v.PixFmt == "yuvj420p"
	case "hevc", "av1", "vp9":
		return v.PixFmt == "yuv420p" || v.PixFmt == "yuv420p10le" || v.PixFmt == "yuv420p12le" || v.PixFmt == "yuvj420p"
	case "mpeg2video", "vc1", "mpeg4":
		return v.PixFmt == "yuv420p"
	}
	return false
}

func maxrate(height int) int {
	switch {
	case height > 1440:
		return 40_000_000
	case height > 1080:
		return 24_000_000
	case height > 720:
		return 16_000_000
	case height > 480:
		return 8_000_000
	}
	return 4_000_000
}

func audioBits(ch int) int {
	switch {
	case ch <= 1:
		return 128_000
	case ch <= 2:
		return 192_000
	}
	return 384_000
}

// tonemapChain converts HDR10/HLG frames (already scaled) to SDR BT.709.
func tonemapChain(v *medialib.VideoStream) string {
	tin := v.Transfer
	if tin != "arib-std-b67" {
		tin = "smpte2084"
	}
	pin, min := v.Primaries, v.Matrix
	if pin == "" || pin == "unknown" {
		pin = "bt2020"
	}
	if min == "" || min == "unknown" || min == "bt2020c" {
		min = "bt2020nc"
	}
	return fmt.Sprintf("zscale=tin=%s:pin=%s:min=%s:t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,"+
		"tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p", tin, pin, min)
}

// jobSpec is one ffmpeg run producing segments start.. of one group: a
// video variant ("v0", "v1", ...) or every audio track ("a").
type jobSpec struct {
	src   string
	probe *medialib.Probe
	plan  *Plan
	enc   Encoder
	start int
	group string
	// out returns the segment file pattern and list file for a rendition.
	out func(rendition string) (pattern, list string)
}

// args builds the ffmpeg command line.
//
// Segments from different runs must line up exactly, since a seek
// restarts ffmpeg mid-file and the player mixes old and new segments.
// So every segment carries its absolute time: the input is seeked to the
// segment's start (ss) with timestamps kept (-copyts), the output is
// trimmed exactly at ss (an input seek alone lets copied packets from the
// preceding keyframe through), and -output_ts_offset restores ss after the
// trim resets the clock. Two clocks then matter, both measured by
// experiment with ffmpeg 7-9:
//   - forced keyframes count from ss (or 0 on a run from the start);
//   - the segment muxer counts its cut times from the first packet it
//     receives in each output.
func (j *jobSpec) args() []string {
	p, v := j.plan, j.probe.Video
	audioJob := j.group == "a"
	var vr Variant
	if !audioJob {
		vr = p.Variants[variantIndex(j.group)]
	}
	ss := 0.0
	if j.start > 0 {
		ss = p.Bounds[j.start]
		if vr.Copy {
			// Trim just before the keyframe, which is then the first packet.
			// Copied packets are trimmed by decode time, which runs ahead
			// of presentation time by the B-frame delay.
			fps := v.FPS
			if fps <= 0 {
				fps = 30
			}
			ss -= p.KeyDelay + min(0.01, 0.4/fps)
		}
	}

	// cutList gives an output's cut times: at (each boundary mapped
	// through at) less early, relative to the output's first packet.
	cutList := func(first, early float64, at func(float64) float64) string {
		var cuts []string
		for _, b := range p.Bounds[j.start+1 : len(p.Bounds)-1] {
			cuts = append(cuts, strconv.FormatFloat(max(at(b)-first-early, 0.001), 'f', 4, 64))
		}
		if len(cuts) == 0 {
			// One segment left: a cut past the end means "no cut".
			return strconv.FormatFloat(p.Bounds[len(p.Bounds)-1]-first+60, 'f', 3, 64)
		}
		return strings.Join(cuts, ",")
	}

	type audioRun struct {
		trim, first float64
		at          func(float64) float64
	}
	var audio []audioRun
	seek := ss
	if audioJob {
		for i, a := range p.Audio {
			audio = append(audio, audioRun(audioTiming(a, p.Bounds[j.start], j.start > 0)))
			if i == 0 || audio[i].trim < seek {
				seek = audio[i].trim
			}
		}
	}

	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y"}
	if !audioJob && !vr.Copy && j.enc.CUDA && nvdec(v) {
		args = append(args, "-hwaccel", "cuda")
		if !v.Interlaced {
			args = append(args, "-hwaccel_output_format", "cuda")
		}
	}
	if j.start > 0 {
		// MPEG transport and program streams have no index, and ffmpeg's
		// seek in them can land past the target; start early and let the
		// exact output trims below do the rest.
		if f := j.probe.Format; strings.Contains(f, "mpegts") || strings.Contains(f, "mpeg") {
			seek -= 6
		}
		args = append(args, "-ss", fmtSec(max(seek, 0)))
	}
	args = append(args, "-copyts", "-start_at_zero", "-i", j.src)
	// ffmpeg puts the file's zero, and the trims' clock, at the earliest of
	// the streams a run reads, so every run must read the same ones: the
	// video and every audio track, whichever it actually makes. The rest
	// go to a null sink, which costs only demuxing.
	sink := []string{"-map", "0:" + strconv.Itoa(v.Index)}
	for _, a := range p.Audio {
		sink = append(sink, "-map", "0:"+strconv.Itoa(a.Src.Index))
	}
	sink = append(sink, "-c", "copy", "-f", "null", "-")

	segOut := func(rendition, cuts string, trim float64) []string {
		pattern, list := j.out(rendition)
		out := []string{}
		if j.start > 0 {
			out = append(out, "-ss", fmtSec(trim), "-output_ts_offset", fmtSec(trim))
		}
		// B-frames give the first packet a negative decode time; left on,
		// the muxer would shift a run from the start (only) to hide it.
		return append(out, "-avoid_negative_ts", "disabled", "-f", "segment", "-segment_format", "mpegts", "-segment_times", cuts,
			"-segment_time_delta", "0.005", "-segment_start_number", strconv.Itoa(j.start),
			"-segment_list", list, "-segment_list_type", "csv", pattern)
	}
	same := func(b float64) float64 { return b }

	if !audioJob {
		// When the output's first packet arrives, on the file's timeline.
		first := ss
		switch {
		case vr.Copy && j.start > 0:
			first = p.Bounds[j.start]
		case vr.Copy:
			first = p.FirstKey
		case j.start == 0:
			first = v.StartTime
		}
		// Transcodes have keyframes only on segment boundaries (scene-cut
		// keyframes are off), so a cut time a second early still lands on
		// the right one whatever the first frame's exact time. Copies have
		// keyframes everywhere and need the exact time.
		early := 0.0
		if !vr.Copy {
			early = 1
		}
		args = append(args, "-map", "0:"+strconv.Itoa(v.Index), "-an", "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1")
		if vr.Copy {
			bsf := "h264_mp4toannexb"
			if v.Codec == "hevc" {
				bsf = "hevc_mp4toannexb"
			}
			args = append(args, "-c:v", "copy", "-bsf:v", bsf)
		} else {
			args = append(args, j.videoEncode(ss, vr)...)
		}
		args = append(args, segOut(j.group, cutList(first, early, same), ss)...)
		return append(args, sink...)
	}

	// Each audio track is its own rendition, so the player can switch
	// language without the server restarting anything.
	for i, a := range p.Audio {
		args = append(args, "-map", "0:"+strconv.Itoa(a.Src.Index), "-vn", "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1")
		if a.Copy {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", strconv.Itoa(audioBits(a.Channels)), "-ac", strconv.Itoa(a.Channels), "-ar", "48000")
		}
		args = append(args, segOut("a"+strconv.Itoa(i), cutList(audio[i].first, 0, audio[i].at), audio[i].trim)...)
	}
	return append(args, sink...)
}

// aacFrame is the duration of one AAC frame at the 48 kHz transcodes use.
const aacFrame = 1024.0 / 48000

// audioTiming works out where a run trims audio track a and when its
// first packet then is, for a run starting at segment boundary b.
//
// Audio frames sit on a fixed grid from the stream's start, and a segment
// has to hold the same frames whichever run made it. A copy is trimmed
// at b and starts on the first source frame at or after it. An AAC encode
// is trimmed on the grid of 48 kHz AAC frames (so every run encodes the
// same frames) and, as measured, emits its first packet one frame of
// encoder delay before the trim.
func audioTiming(a AudioOut, b float64, seeked bool) (r struct {
	trim, first float64
	at          func(float64) float64
}) {
	start := a.Src.StartTime
	if !a.Copy {
		start = max(start, 0)
		r.trim = start
		if seeked && b > start {
			r.trim = start + math.Floor((b-start)/aacFrame+1e-6)*aacFrame
		}
		r.first = r.trim - aacFrame
		r.at = func(b float64) float64 { return b }
		return r
	}
	// A segment of copied audio starts on the first source frame at or
	// after its boundary.
	r.at = func(b float64) float64 { return b }
	if fd := audioFrame(a.Src); fd > 0 {
		r.at = func(b float64) float64 { return start + math.Ceil((b-start)/fd-1e-6)*fd }
	}
	r.trim, r.first = b, start
	if seeked && b > start {
		r.first = r.at(b)
		// Halfway into the frame before: keeps that first frame whatever
		// rounding the container applied to its timestamp.
		r.trim = r.first - audioFrame(a.Src)/2
	}
	return r
}

// audioFrame is a codec's frame duration in seconds (0 if unknown).
func audioFrame(a medialib.AudioStream) float64 {
	if a.SampleRate <= 0 {
		return 0
	}
	samples := 0
	switch a.Codec {
	case "ac3", "eac3":
		samples = 1536
	case "aac":
		samples = 1024
		if strings.Contains(a.Profile, "HE") {
			samples = 2048
		}
	case "mp3":
		samples = 1152
		if a.SampleRate < 32000 {
			samples = 576
		}
	case "mp2":
		samples = 1152
	}
	return float64(samples) / float64(a.SampleRate)
}

func fmtSec(f float64) string { return strconv.FormatFloat(f, 'f', 3, 64) }

func (j *jobSpec) videoEncode(ss float64, vr Variant) []string {
	p, v := j.plan, j.probe.Video
	h := vr.Height
	ow, oh := Fit(v.Width, v.Height, h)
	scale := ow != v.Width&^1 || oh != v.Height&^1
	gpuFrames := j.enc.CUDA && nvdec(v) && !v.Interlaced

	var vf []string
	if gpuFrames {
		format := "nv12"
		if p.Tonemap {
			format = "p010le"
		}
		vf = append(vf, fmt.Sprintf("scale_cuda=w=%d:h=%d:format=%s", ow, oh, format))
		if p.Tonemap {
			vf = append(vf, "hwdownload", "format=p010le", tonemapChain(v))
		}
	} else {
		if v.Interlaced {
			vf = append(vf, "bwdif=mode=send_frame")
		}
		if scale {
			vf = append(vf, fmt.Sprintf("scale=w=%d:h=%d:flags=lanczos", ow, oh))
		}
		if p.Tonemap {
			vf = append(vf, tonemapChain(v))
		} else {
			vf = append(vf, "format=yuv420p")
		}
	}

	var keys []string
	for _, b := range p.Bounds[j.start+1 : len(p.Bounds)-1] {
		keys = append(keys, strconv.FormatFloat(b-ss, 'f', 3, 64))
	}
	force := strings.Join(keys, ",")
	if force == "" {
		force = "0"
	}
	fps := v.FPS
	if fps <= 0 || fps > 120 {
		fps = 30
	}
	gop := strconv.Itoa(int(fps * 8))
	rate := vr.Maxrate
	if rate <= 0 {
		rate = maxrate(h)
	}
	// The top rung is constant quality under a cap; the lower rungs are
	// sized for weak networks, so they aim at a bitrate instead.
	nvRate := []string{"-rc", "vbr", "-cq", "20", "-b:v", "0"}
	qsvRate := []string{"-global_quality", "21"}
	x264Rate := []string{"-crf", "20"}
	if vr.Bitrate > 0 {
		br := strconv.Itoa(vr.Bitrate)
		nvRate = []string{"-rc", "vbr", "-b:v", br}
		qsvRate = []string{"-b:v", br}
		x264Rate = []string{"-b:v", br}
	}
	caps := []string{"-maxrate", strconv.Itoa(rate), "-bufsize", strconv.Itoa(rate * 2)}
	out := []string{"-vf", strings.Join(vf, ",")}
	switch j.enc.Name {
	case "h264_nvenc":
		out = append(out, "-c:v", "h264_nvenc", "-preset", "p5", "-tune", "hq", "-profile:v", "high")
		out = append(out, nvRate...)
		out = append(out, caps...)
		out = append(out, "-spatial-aq", "1", "-temporal-aq", "1", "-rc-lookahead", "20", "-bf", "3", "-b_ref_mode", "middle",
			"-g", gop, "-forced-idr", "1", "-no-scenecut", "1")
	case "h264_qsv":
		out = append(out, "-c:v", "h264_qsv", "-preset", "medium", "-profile:v", "high")
		out = append(out, qsvRate...)
		out = append(out, caps...)
		out = append(out, "-g", gop, "-forced_idr", "1")
	default:
		out = append(out, "-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high")
		out = append(out, x264Rate...)
		out = append(out, caps...)
		out = append(out, "-g", gop, "-sc_threshold", "0")
	}
	// Tag the stream BT.709 in the bitstream. The -color_* output options
	// would instead make ffmpeg (7.1+) negotiate a colour conversion into
	// the filter graph, which fails behind scale_cuda's GPU frames.
	return append(out, "-force_key_frames", force,
		"-bsf:v", "h264_metadata=colour_primaries=1:transfer_characteristics=1:matrix_coefficients=1")
}

// Keyframes lists the times (seconds from the start of the file) of the
// video's keyframes, and how far their decode times run ahead of their
// presentation times (B-frame reordering), which a stream copy has to
// trim by. It reads the whole file's packet index, which is disk-bound:
// seconds for most files.
func Keyframes(ctx context.Context, ffprobe, path string, p *medialib.Probe) ([]float64, float64, error) {
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "packet=pts_time,dts_time,flags", "-of", "csv=p=0", path).Output()
	if err != nil {
		return nil, 0, fmt.Errorf("keyframes: %w", err)
	}
	var kf []float64
	delay := 0.0
	for line := range strings.Lines(string(out)) {
		f := strings.Split(strings.TrimSpace(line), ",")
		if len(f) < 3 || !strings.HasPrefix(f[2], "K") {
			continue
		}
		pts, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			continue
		}
		if dts, err := strconv.ParseFloat(f[1], 64); err == nil && pts-dts < 2 {
			delay = max(delay, pts-dts)
		}
		kf = append(kf, pts-p.StartTime)
	}
	// Packets come in decode order; keyframe times are increasing in
	// practice, but sort to be safe with odd muxers.
	slices.Sort(kf)
	return kf, delay, nil
}

package mediastream

import (
	"fmt"
	"testing"

	"projectorremote/internal/medialib"
)

// The projector's decoders, as read from its media_codecs.xml (its Dolby
// and DTS decoder lists are installed but not included, so not active).
var projectorCaps = Caps{
	Video: []VideoCap{{Codec: "h264", MaxWidth: 1920, MaxHeight: 1920}, {Codec: "hevc", MaxWidth: 4096, MaxHeight: 4096, TenBit: true}},
	Audio: []string{"mp3", "aac"},
}

func TestDecideLadder(t *testing.T) {
	kf := []float64{0, 2, 4, 6, 8, 10}
	opt := Options{MaxHeight: 1080, MaxCopyBitrate: 80_000_000, SegmentSeconds: 4}
	for _, tc := range []struct {
		name    string
		v       medialib.VideoStream
		want    string // variants, top first
		tonemap bool
	}{
		{"widescreen 1080p H.264", medialib.VideoStream{Codec: "h264", PixFmt: "yuv420p", Width: 1920, Height: 818, BitDepth: 8}, "copy 1280x546 854x364", false},
		{"4K HEVC 10-bit SDR", medialib.VideoStream{Codec: "hevc", PixFmt: "yuv420p10le", Width: 3840, Height: 2160, BitDepth: 10}, "copy 1920x1080 1280x720 854x480", false},
		{"4K HEVC HDR10", medialib.VideoStream{Codec: "hevc", PixFmt: "yuv420p10le", Width: 3840, Height: 2160, BitDepth: 10, Transfer: "smpte2084"}, "1920x1080 1280x720 854x480", true},
		{"Hi10P anime", medialib.VideoStream{Codec: "h264", PixFmt: "yuv420p10le", Width: 1920, Height: 1080, BitDepth: 10}, "1920x1080 1280x720 854x480", false},
		{"Xvid SD", medialib.VideoStream{Codec: "mpeg4", PixFmt: "yuv420p", Width: 720, Height: 480, BitDepth: 8}, "720x480", false},
		{"720p H.264", medialib.VideoStream{Codec: "h264", PixFmt: "yuv420p", Width: 1280, Height: 720, BitDepth: 8}, "copy 854x480", false},
	} {
		v := tc.v
		p := &medialib.Probe{Duration: 60, Bitrate: 10_000_000, Video: &v,
			Audio: []medialib.AudioStream{{Codec: "dts", Channels: 6, SampleRate: 48000}, {Codec: "ac3", Channels: 6, SampleRate: 48000},
				{Codec: "aac", Channels: 2, SampleRate: 48000}}}
		plan := Decide(p, projectorCaps, opt, kf, 0)
		got := ""
		for i, vr := range plan.Variants {
			if i > 0 {
				got += " "
			}
			if vr.Copy {
				got += "copy"
				continue
			}
			w, h := Fit(v.Width, v.Height, vr.Height)
			got += fmt.Sprintf("%dx%d", w, h)
		}
		if got != tc.want || plan.Tonemap != tc.tonemap {
			t.Errorf("%s: variants %q tonemap %v, want %q %v", tc.name, got, plan.Tonemap, tc.want, tc.tonemap)
		}
		// DTS and AC-3 become AAC; AAC is copied.
		if plan.Audio[0].Copy || plan.Audio[1].Copy || !plan.Audio[2].Copy {
			t.Errorf("%s: audio copy = %v %v %v, want false false true", tc.name, plan.Audio[0].Copy, plan.Audio[1].Copy, plan.Audio[2].Copy)
		}
	}
}

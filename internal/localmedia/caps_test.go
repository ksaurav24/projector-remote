package localmedia

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const vendorCodecs = `<?xml version="1.0" encoding="utf-8" ?>
<MediaCodecs>
    <Include href="media_codecs_google.xml" />
    <Decoders>
        <MediaCodec name="OMX.amlogic.avc.decoder.awesome2" type="video/avc">
            <Limit name="size" min="2x2" max="4096x2304" />
        </MediaCodec>
        <MediaCodec name="OMX.amlogic.avc.decoder.awesome2.secure" type="video/avc">
            <Limit name="size" min="2x2" max="4096x2304" />
        </MediaCodec>
        <MediaCodec name="OMX.amlogic.hevc.decoder.awesome2">
            <Type name="video/hevc">
                <Limit name="size" min="2x2" max="3840x2160" />
            </Type>
        </MediaCodec>
        <MediaCodec name="OMX.google.ac3.decoder" type="audio/ac3" />
        <MediaCodec name="OMX.google.mpeg2.decoder" type="video/mpeg2" />
    </Decoders>
    <Encoders>
        <MediaCodec name="OMX.amlogic.video.encoder.avc" type="video/avc" />
    </Encoders>
</MediaCodecs>`

const googleAudio = `<Included><Decoders>
  <MediaCodec name="c2.android.aac.decoder" type="audio/mp4a-latm" />
  <MediaCodec name="c2.android.mp3.decoder" type="audio/mpeg" />
  <MediaCodec name="c2.android.hevc.decoder" type="video/hevc"><Limit name="size" max="4096x4096" /></MediaCodec>
</Decoders></Included>`

func TestCapsFromCodecList(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "media_codecs.xml")
	_ = os.WriteFile(a, []byte(vendorCodecs), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "media_codecs_google.xml"), []byte(googleAudio), 0o644)
	// Present but not included: Android ignores it, and so must we.
	_ = os.WriteFile(filepath.Join(dir, "media_codecs_dolby.xml"),
		[]byte(`<Included><Decoders><MediaCodec name="c2.vendor.eac3" type="audio/eac3" /></Decoders></Included>`), 0o644)
	c := capsFrom([]string{a})

	if len(c.Video) != 2 {
		t.Fatalf("video caps = %+v", c.Video)
	}
	h264, hevc := c.Video[0], c.Video[1]
	if h264.Codec != "h264" || h264.MaxWidth != 4096 || h264.MaxHeight != 2304 || h264.TenBit {
		t.Errorf("h264 = %+v", h264)
	}
	// The hardware HEVC decoder wins over the bigger software one.
	if hevc.Codec != "hevc" || hevc.MaxHeight != 2160 || !hevc.TenBit {
		t.Errorf("hevc = %+v", hevc)
	}
	for _, want := range []string{"aac", "mp3", "ac3"} {
		if !slices.Contains(c.Audio, want) {
			t.Errorf("audio %v lacks %s", c.Audio, want)
		}
	}
	if slices.Contains(c.Audio, "eac3") {
		t.Errorf("audio %v claims eac3", c.Audio)
	}
}

func TestCapsDefaultWithoutCodecList(t *testing.T) {
	c := capsFrom(nil)
	if len(c.Video) != 1 || c.Video[0].Codec != "h264" || c.Video[0].MaxHeight != 1088 {
		t.Errorf("default caps = %+v", c)
	}
}

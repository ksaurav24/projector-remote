package localmedia

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"projectorremote/internal/mediastream"
)

// Android reads its codec list from these files and whatever they
// <Include> (vendor ones name the hardware decoders; the swcodec APEX the
// software fallbacks). Other media_codecs_*.xml files can sit beside them
// unused: this projector ships Dolby and DTS decoder lists it never
// includes.
var codecRoots = []string{
	"/vendor/etc/media_codecs.xml",
	"/odm/etc/media_codecs.xml",
	"/system/etc/media_codecs.xml",
	"/apex/com.android.media.swcodec/etc/media_codecs.xml",
}

type codecsXML struct {
	Includes []struct {
		Href string `xml:"href,attr"`
	} `xml:"Include"`
	Decoders struct {
		Codecs []struct {
			Name  string `xml:"name,attr"`
			Type  string `xml:"type,attr"`
			Types []struct {
				Name   string     `xml:"name,attr"`
				Limits []limitXML `xml:"Limit"`
			} `xml:"Type"`
			Limits []limitXML `xml:"Limit"`
		} `xml:"MediaCodec"`
	} `xml:"Decoders"`
}

type limitXML struct {
	Name string `xml:"name,attr"`
	Max  string `xml:"max,attr"`
}

// MIME types to the ffmpeg codec names the media server uses.
var videoMimes = map[string]string{"video/avc": "h264", "video/hevc": "hevc"}
var audioMimes = map[string]string{
	"audio/mp4a-latm": "aac", "audio/ac3": "ac3", "audio/eac3": "eac3", "audio/mpeg": "mp3",
	"audio/eac3-joc": "eac3",
}

// DetectCaps reads what this device's decoders handle. A missing or
// unreadable list leaves the conservative default.
func DetectCaps() mediastream.Caps { return capsFrom(codecRoots) }

// capsFrom reads the codec lists at roots and everything they include.
func capsFrom(roots []string) mediastream.Caps {
	video := map[string]mediastream.VideoCap{}
	var audio []string
	seen := map[string]bool{}
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var doc codecsXML
		if xml.Unmarshal(raw, &doc) != nil {
			continue
		}
		for _, inc := range doc.Includes {
			// Includes name a file beside the including one.
			queue = append(queue, filepath.Join(filepath.Dir(p), filepath.Base(inc.Href)))
		}
		for _, c := range doc.Decoders.Codecs {
			type entry struct {
				mime   string
				limits []limitXML
			}
			entries := []entry{{c.Type, c.Limits}}
			for _, t := range c.Types {
				entries = append(entries, entry{t.Name, append(t.Limits, c.Limits...)})
			}
			for _, e := range entries {
				mime := strings.ToLower(e.mime)
				// Secure decoders only play DRM content.
				if mime == "" || strings.HasSuffix(c.Name, ".secure") {
					continue
				}
				if name, ok := audioMimes[mime]; ok && !slices.Contains(audio, name) {
					audio = append(audio, name)
				}
				name, ok := videoMimes[mime]
				if !ok {
					continue
				}
				w, h := maxSize(e.limits)
				// A software decoder can't keep up beyond 1080p on this class
				// of hardware; trust only hardware ones for more.
				if isSoftware(c.Name) {
					w, h = min(w, 1920), min(h, 1088)
				}
				cur := video[name]
				if w*h > cur.MaxWidth*cur.MaxHeight {
					// A 4K HEVC decoder on these SoCs is Main10 capable.
					video[name] = mediastream.VideoCap{Codec: name, MaxWidth: w, MaxHeight: h, TenBit: name == "hevc" && h >= 2160 && !isSoftware(c.Name)}
				}
			}
		}
	}
	caps := mediastream.DefaultCaps()
	if len(video) == 0 && len(audio) == 0 {
		return caps
	}
	caps.Video = nil
	for _, n := range []string{"h264", "hevc"} {
		if v, ok := video[n]; ok {
			caps.Video = append(caps.Video, v)
		}
	}
	if len(caps.Video) == 0 {
		caps.Video = mediastream.DefaultCaps().Video
	}
	for _, a := range []string{"aac", "mp3"} {
		if !slices.Contains(audio, a) {
			audio = append(audio, a)
		}
	}
	caps.Audio = audio
	return caps
}

func isSoftware(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "omx.google.") || strings.HasPrefix(n, "c2.android.")
}

func maxSize(limits []limitXML) (int, int) {
	w, h := 1920, 1088
	for _, l := range limits {
		if l.Name != "size" || l.Max == "" {
			continue
		}
		a, b, ok := strings.Cut(l.Max, "x")
		if !ok {
			continue
		}
		wi, err1 := strconv.Atoi(a)
		hi, err2 := strconv.Atoi(b)
		if err1 == nil && err2 == nil {
			// Limits list the larger dimension either way round.
			w, h = max(wi, hi), min(wi, hi)
		}
	}
	return w, h
}

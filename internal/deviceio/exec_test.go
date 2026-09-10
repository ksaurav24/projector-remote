package deviceio

import "testing"

const sampleDumpsysAudioOutput = `- STREAM_VOICE_CALL (aliased to: STREAM_MUSIC):
   Muted: false
   Min: 1
   Max: 5
   streamVolume:3
- STREAM_SYSTEM (aliased to: STREAM_MUSIC):
   Muted: false
   Min: 0
   Max: 7
   streamVolume:5
- STREAM_MUSIC:
   Muted: false
   Muted Internally: false
   Min: 0
   Max: 100
   streamVolume:73
   Current: 2 (speaker): 73, 400 (hdmi): 25
   Devices: speaker(2)
- STREAM_ALARM (aliased to: STREAM_MUSIC):
   Muted: false
   Min: 0
   Max: 7
   streamVolume:6
`

func TestParseStreamMusicVolume_FindsMusicBlockNotAliasedBlocks(t *testing.T) {
	got, err := parseStreamMusicVolume(sampleDumpsysAudioOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 73 {
		t.Errorf("got %d, want 73", got)
	}
}

func TestParseStreamMusicVolume_ErrorsWhenBlockMissing(t *testing.T) {
	_, err := parseStreamMusicVolume("- STREAM_ALARM:\n   streamVolume:6\n")
	if err == nil {
		t.Fatal("expected error when STREAM_MUSIC block is absent")
	}
}

func TestParsePackageList_ExtractsPackageNames(t *testing.T) {
	input := "package:com.netflix.ninja\npackage:com.google.android.youtube.tv\n\n"
	got := parsePackageList(input)
	want := []string{"com.netflix.ninja", "com.google.android.youtube.tv"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

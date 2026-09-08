package deviceio

import "testing"

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

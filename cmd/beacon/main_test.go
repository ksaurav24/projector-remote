package main

import "testing"

func TestBuildTXT_IncludesSerialWhenGiven(t *testing.T) {
	got := buildTXT("PSAJ611263EN096c")
	want := []string{"serial=PSAJ611263EN096c"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildTXT_EmptyWhenSerialOmitted(t *testing.T) {
	got := buildTXT("")
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

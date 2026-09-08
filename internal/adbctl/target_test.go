package adbctl

import (
	"context"
	"errors"
	"testing"
)

func TestTargetResolver_UsesManualOnceThenDiscovery(t *testing.T) {
	discoverCalls := 0
	r := newTargetResolver("", func(ctx context.Context) (string, int, error) {
		discoverCalls++
		return "5.6.7.8", 3333, nil
	})
	r.setManual("1.2.3.4", 5555)

	got, err := r.resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "1.2.3.4:5555" {
		t.Errorf("got %q, want 1.2.3.4:5555", got)
	}
	if discoverCalls != 0 {
		t.Errorf("expected discover not called while manual target pending, called %d times", discoverCalls)
	}

	got, err = r.resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "5.6.7.8:3333" {
		t.Errorf("got %q, want 5.6.7.8:3333 (discovery, manual consumed)", got)
	}
	if discoverCalls != 1 {
		t.Errorf("expected discover called once, called %d times", discoverCalls)
	}
}

func TestTargetResolver_NoManualUsesDiscoveryImmediately(t *testing.T) {
	r := newTargetResolver("", func(ctx context.Context) (string, int, error) {
		return "9.9.9.9", 1111, nil
	})

	got, err := r.resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "9.9.9.9:1111" {
		t.Errorf("got %q, want 9.9.9.9:1111", got)
	}
}

func TestTargetResolver_InitialManualSeedUsedOnce(t *testing.T) {
	discoverCalls := 0
	r := newTargetResolver("10.0.0.1:3333", func(ctx context.Context) (string, int, error) {
		discoverCalls++
		return "", 0, errors.New("no beacon found")
	})

	got, err := r.resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "10.0.0.1:3333" {
		t.Errorf("got %q, want 10.0.0.1:3333", got)
	}
	if discoverCalls != 0 {
		t.Errorf("expected discover not called for the seeded initial target, called %d times", discoverCalls)
	}
}

func TestTargetResolver_PropagatesDiscoverError(t *testing.T) {
	wantErr := errors.New("no beacon found")
	r := newTargetResolver("", func(ctx context.Context) (string, int, error) {
		return "", 0, wantErr
	})

	_, err := r.resolve(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("resolve error = %v, want %v", err, wantErr)
	}
}

func TestTargetResolver_LaterManualOverridesPendingDiscoveryTurn(t *testing.T) {
	// A manual target set at any point before the next resolve() call
	// takes priority over discovery, even if none was set at construction.
	r := newTargetResolver("", func(ctx context.Context) (string, int, error) {
		t.Fatal("discover should not be called: a manual target was set first")
		return "", 0, nil
	})
	r.setManual("172.16.0.5", 9999)

	got, err := r.resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "172.16.0.5:9999" {
		t.Errorf("got %q, want 172.16.0.5:9999", got)
	}
}

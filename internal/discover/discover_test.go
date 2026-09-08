package discover

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hashicorp/mdns"
)

func startTestBeacon(t *testing.T, port int) {
	t.Helper()
	svc, err := mdns.NewMDNSService("test-projector", ServiceType, "", "test-host.local.", port, []net.IP{net.ParseIP("127.0.0.1")}, []string{"serial=TESTSERIAL"})
	if err != nil {
		t.Fatalf("NewMDNSService: %v", err)
	}
	srv, err := mdns.NewServer(&mdns.Config{Zone: svc})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown() })
}

func TestFind_ReturnsAdvertisedHostPort(t *testing.T) {
	startTestBeacon(t, 3333)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	host, port, err := Find(ctx, time.Second)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if host != "127.0.0.1" {
		t.Errorf("host = %q, want 127.0.0.1", host)
	}
	if port != 3333 {
		t.Errorf("port = %d, want 3333", port)
	}
}

// matchesService's test coverage: on a real LAN, our client's multicast
// socket sees every mDNS answer in flight, not just replies to our own
// query — confirmed against a live device, whose Android TV OS was
// independently re-announcing its own _raop._tcp (AirPlay) service on
// the shared channel, and hashicorp/mdns's client (which doesn't check
// an answer's owner name against what was asked) surfaced that
// unrelated service as if it were a match. matchesService is the fix:
// Find must ignore any entry whose name isn't actually one of ours.
func TestMatchesService_AcceptsOwnServiceType(t *testing.T) {
	if !matchesService("Projector._projector-remote._tcp.local.") {
		t.Error("expected our own service name to match")
	}
}

func TestMatchesService_RejectsUnrelatedServiceType(t *testing.T) {
	// Real example observed from a device's own AirPlay responder,
	// arriving unprompted on the same multicast channel we listen on.
	name := `87:1D:9B:12:CD:25\@Projector._raop._tcp.local.`
	if matchesService(name) {
		t.Errorf("expected unrelated service name %q not to match", name)
	}
}

func TestFind_TimesOutWhenNothingAdvertised(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := Find(ctx, 200*time.Millisecond)
	if err == nil {
		t.Fatal("Find: expected error when no beacon is advertising, got nil")
	}
}

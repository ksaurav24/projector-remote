// Command beacon runs on the projector and advertises its ADB TCP
// endpoint via mDNS/DNS-SD so cmd/server can find it automatically,
// even after its IP changes on every power cycle.
//
// Built as a static Linux/ARM binary (GOOS=linux GOARCH=arm
// CGO_ENABLED=0 — NOT GOOS=android, which requires an NDK C linker
// even for pure-Go code). Runs directly on the device's Linux kernel;
// no Android app framework or root access needed. Launched in the
// background by the on-device check_adb boot hook.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/hashicorp/mdns"
	"projectorremote/internal/discover"
)

// buildTXT returns the DNS-SD TXT record fields for the advertised
// service. serial is optional device-identifying info; omitted from
// the record entirely when blank.
func buildTXT(serial string) []string {
	if serial == "" {
		return nil
	}
	return []string{"serial=" + serial}
}

func main() {
	port := flag.Int("port", 3333, "ADB TCP port to advertise")
	serial := flag.String("serial", "", "optional device serial to include in the TXT record")
	instance := flag.String("instance", "Projector", "mDNS service instance name")
	flag.Parse()

	ips, err := localIPv4Addrs()
	if err != nil {
		log.Fatalf("beacon: %v", err)
	}

	// hostName is just a label here, not independently resolved — ips is
	// supplied explicitly so mdns.NewMDNSService skips its os.Hostname()
	// + DNS-lookup fallback, which doesn't work reliably on Android.
	svc, err := mdns.NewMDNSService(*instance, discover.ServiceType, "", "projector.local.", *port, ips, buildTXT(*serial))
	if err != nil {
		log.Fatalf("beacon: build service: %v", err)
	}
	srv, err := mdns.NewServer(&mdns.Config{Zone: svc})
	if err != nil {
		log.Fatalf("beacon: start mdns server: %v", err)
	}
	defer func() { _ = srv.Shutdown() }()

	log.Printf("beacon: advertising %s on port %d", discover.ServiceType, *port)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	<-ctx.Done()
}

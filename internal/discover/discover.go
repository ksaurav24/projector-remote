// Package discover finds the projector on the LAN via mDNS/DNS-SD,
// browsing for the service cmd/beacon advertises from the device.
package discover

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

// ServiceType is the DNS-SD service type the device-side beacon
// advertises and this package browses for.
const ServiceType = "_projector-remote._tcp"

// serviceNameSuffix is what every instance name of our service type
// ends with, e.g. "Projector._projector-remote._tcp.local.".
const serviceNameSuffix = "." + ServiceType + ".local."

// matchesService reports whether an mDNS entry's instance name belongs
// to our service type. The underlying mdns client's multicast socket
// sees every answer on the shared channel — including unrelated
// services already running on the same device (observed live: an
// Android TV's own AirPlay/_raop._tcp responder) — not just replies to
// our own query, so entries must be filtered by name before use.
func matchesService(name string) bool {
	return strings.HasSuffix(name, serviceNameSuffix)
}

// Find browses the LAN for a projector-remote beacon and returns the
// host and ADB port of the first instance found. It returns an error
// if none answers within timeout or ctx is canceled first.
func Find(ctx context.Context, timeout time.Duration) (host string, port int, err error) {
	entries := make(chan *mdns.ServiceEntry, 4)
	params := mdns.DefaultParams(ServiceType)
	params.Timeout = timeout
	params.Entries = entries

	queryErr := make(chan error, 1)
	go func() { queryErr <- mdns.QueryContext(ctx, params) }()

	for {
		select {
		case e := <-entries:
			if !matchesService(e.Name) {
				continue
			}
			if e.AddrV4 == nil || e.Port == 0 {
				continue
			}
			return e.AddrV4.String(), e.Port, nil
		case err := <-queryErr:
			if err != nil {
				return "", 0, fmt.Errorf("discover: mdns query: %w", err)
			}
			return "", 0, fmt.Errorf("discover: no %s service found", ServiceType)
		case <-ctx.Done():
			return "", 0, ctx.Err()
		}
	}
}

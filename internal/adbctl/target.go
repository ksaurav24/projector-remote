package adbctl

import (
	"context"
	"fmt"
	"sync"
)

// DiscoverFunc looks up the projector's current ADB target on the LAN
// (e.g. via mDNS). The production default wraps discover.Find; tests
// inject a fake.
type DiscoverFunc func(ctx context.Context) (host string, port int, err error)

// targetResolver decides which "host:port" to use for the next
// connection attempt: a pending manual target (set via setManual, or
// seeded at construction from -target) is used exactly once and then
// cleared; otherwise it falls back to discover.
type targetResolver struct {
	mu       sync.Mutex
	manual   string
	discover DiscoverFunc
}

func newTargetResolver(initialManual string, discover DiscoverFunc) *targetResolver {
	return &targetResolver{manual: initialManual, discover: discover}
}

// setManual queues a one-shot manual target for the next resolve call.
func (r *targetResolver) setManual(host string, port int) {
	r.mu.Lock()
	r.manual = fmt.Sprintf("%s:%d", host, port)
	r.mu.Unlock()
}

// resolve returns the target for the next connection attempt, consuming
// any pending manual target. Falls back to discovery otherwise.
func (r *targetResolver) resolve(ctx context.Context) (string, error) {
	r.mu.Lock()
	manual := r.manual
	r.manual = ""
	r.mu.Unlock()

	if manual != "" {
		return manual, nil
	}

	host, port, err := r.discover(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", host, port), nil
}

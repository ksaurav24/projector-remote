package main

import (
	"fmt"
	"net"
)

// filterLANIPv4 extracts usable LAN-facing IPv4 addresses from a set of
// interface addresses, skipping loopback, link-local, and IPv6 entries.
func filterLANIPv4(addrs []net.Addr) []net.IP {
	var ips []net.IP
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
			continue
		}
		ips = append(ips, ip4)
	}
	return ips
}

// localIPv4Addrs returns this host's LAN-facing IPv4 addresses. Used
// instead of relying on hostname resolution (os.Hostname() + DNS
// lookup), which doesn't reliably resolve to a real address on Android.
func localIPv4Addrs() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("beacon: list interface addresses: %w", err)
	}
	ips := filterLANIPv4(addrs)
	if len(ips) == 0 {
		return nil, fmt.Errorf("beacon: no usable LAN IPv4 address found on any interface")
	}
	return ips, nil
}

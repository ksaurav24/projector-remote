package main

import (
	"net"
	"testing"
)

func TestFilterLANIPv4_KeepsRealLANAddress(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("192.168.1.42"), Mask: net.CIDRMask(24, 32)},
	}
	got := filterLANIPv4(addrs)
	if len(got) != 1 || got[0].String() != "192.168.1.42" {
		t.Errorf("got %v, want [192.168.1.42]", got)
	}
}

func TestFilterLANIPv4_SkipsLoopbackAndLinkLocalAndIPv6(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		&net.IPNet{IP: net.ParseIP("169.254.1.5"), Mask: net.CIDRMask(16, 32)},
		&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.CIDRMask(24, 32)},
	}
	got := filterLANIPv4(addrs)
	if len(got) != 1 || got[0].String() != "10.0.0.5" {
		t.Errorf("got %v, want [10.0.0.5]", got)
	}
}

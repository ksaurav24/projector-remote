package com.projectorremote.home

import org.junit.Assert.*
import org.junit.Test

class RemoteAddressTest {
    @Test fun wifiWinsOverVpnAndEthernet() {
        val result = RemoteAddress.choose(listOf(
            "tailscale0" to listOf("100.78.192.93"),
            "eth0" to listOf("192.168.1.10"),
            "wlan0" to listOf("192.168.122.102"),
        ))!!
        assertEquals("http://192.168.122.102:8080", result.url)
        assertFalse(result.tailnet)
    }

    @Test fun dhcpAddressChangesAreReflectedWithoutSavedConfiguration() {
        val before = RemoteAddress.choose(listOf("wlan0" to listOf("192.168.1.2")))!!
        val after = RemoteAddress.choose(listOf("wlan0" to listOf("192.168.1.3")))!!
        assertNotEquals(before.url, after.url)
        assertEquals("http://192.168.1.3:8080", after.url)
    }

    @Test fun ethernetWorksWithoutWifi() {
        assertEquals("http://10.0.0.20:8080",
            RemoteAddress.choose(listOf("eth0" to listOf("10.0.0.20")))?.url)
    }

    @Test fun ignoresLoopbackLinkLocalAndUnrelatedVpnInterfaces() {
        assertNull(RemoteAddress.choose(listOf(
            "lo" to listOf("127.0.0.1"),
            "wlan0" to listOf("169.254.1.2"),
            "tun0" to listOf("10.8.0.1"),
        )))
    }

    @Test fun tailnetFallbackRequiresDifferentPhoneInstructions() {
        val address = RemoteAddress.choose(listOf(
            "wlan0" to emptyList(),
            "tailscale0" to listOf("100.78.192.93"),
        ))!!
        assertTrue(address.tailnet)
        assertEquals("http://100.78.192.93:8080", address.url)
    }

    @Test fun disconnectedHasNoQrAddress() {
        assertNull(RemoteAddress.choose(emptyList()))
    }
}

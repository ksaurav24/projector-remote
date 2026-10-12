package com.projectorremote.home

/** Prefer the LAN so scanning never requires a VPN on the phone. */
internal object RemoteAddress {
    data class Address(val ip: String, val tailnet: Boolean) {
        val url: String get() = "http://$ip:8080"
    }

    fun choose(interfaces: List<Pair<String, List<String>>>): Address? {
        val local = interfaces.sortedBy { (name, _) ->
            when {
                name.startsWith("wlan") -> 0
                name.startsWith("eth") -> 1
                else -> 2
            }
        }.firstNotNullOfOrNull { (name, ips) ->
            if (name.startsWith("wlan") || name.startsWith("eth")) {
                ips.firstOrNull { !it.startsWith("127.") && !it.startsWith("169.254.") }
            } else null
        }
        if (local != null) return Address(local, false)
        return interfaces.firstOrNull { it.first == "tailscale0" }?.second
            ?.firstOrNull()?.let { Address(it, true) }
    }
}

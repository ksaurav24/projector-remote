#!/system/bin/sh
# Bring up Tailscale on this projector. Invoked from
# /data/local/tmp/startup.sh at boot (which vendor init runs via the
# patched /system/bin/check_adb -- see docs/device-guide.md sections 5-6).
#
# Runs as root. Safe to re-run: every step is idempotent.

T=/data/local/tmp/tailscale

# tailscaled writes a log cache under $HOME; unset, it picks "/.cache"
# on the read-only root filesystem.
export HOME="$T"
export TMPDIR="$T"

# 1. Wait for wifi to get an address and a gateway. At boot this script
#    can easily start before DHCP has finished.
GW=""
i=0
while [ $i -lt 60 ]; do
	GW=$(ip route show table wlan0 2>/dev/null | awk '/^default/{print $3; exit}')
	[ -n "$GW" ] && break
	sleep 2
	i=$((i + 1))
done
if [ -z "$GW" ]; then
	echo "$(date): no default gateway after 120s, giving up"
	exit 1
fi
echo "$(date): gateway $GW"

# 2. Android keeps no default route in the main routing table -- it lives
#    in the per-network table (wlan0), reached through netd's fwmark
#    policy rules. Tailscale's netmon reads main, so without this it finds
#    no default route and silently refuses to send UDP (netcheck reports
#    "UDP: false" and no DERP responses). No ip rule consults main on this
#    device, so this entry is inert for actual packet routing.
ip route replace default via "$GW" dev wlan0
echo "$(date): main table: $(ip route show | tr '\n' ' ')"

# 3. DNS: Android has no /etc/resolv.conf and a static Go binary can't
#    use bionic's netd resolver, so tailscaled resolves via 127.0.0.1:53.
#    startup.sh runs deploy/dnsrelay-up.sh for that; tailscaled retries
#    until it's up.

# 4. tailscaled. The state file persists the node key and WantRunning
#    across reboots, so no auth key is needed here -- it reconnects itself.
if ! ps -ef | grep -q '[t]ailscaled'; then
	nohup "$T/tailscaled" \
		--state="$T/state/tailscaled.state" \
		--socket="$T/tailscaled.sock" \
		--tun=tailscale0 \
		--port=41641 \
		>"$T/tailscaled.log" 2>&1 &
	echo "$(date): started tailscaled"
fi

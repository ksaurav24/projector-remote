#!/system/bin/sh
# Start the localhost DNS forwarder that static Go binaries on this
# device need (docs/device-guide.md §7, quirk 2): projector-remote
# resolves NetMirror's hosts through it, tailscaled its control plane.
# Invoked from /data/local/tmp/startup.sh at boot.
#
# The upstream is the LAN gateway, so wait for DHCP first.

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

if ps -ef | grep -q '[d]nsrelay -upstream'; then
	echo "$(date): dnsrelay already running"
	exit 0
fi
echo "$(date): starting dnsrelay -> $GW:53"
exec /data/local/tmp/dnsrelay -upstream "$GW:53"

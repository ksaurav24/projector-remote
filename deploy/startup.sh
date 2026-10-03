#!/system/bin/sh
# Boot-time services. Invoked once per boot by the patched
# /system/bin/check_adb, which vendor init runs on
# property:sys.boot_completed=1. See docs/device-guide.md sections 5-6.

# DNS for static Go binaries; the remote's NetMirror catalog and
# Tailscale both need it, and it shouldn't depend on either.
[ -x /data/local/tmp/dnsrelay-up.sh ] && \
  nohup /data/local/tmp/dnsrelay-up.sh \
  > /data/local/tmp/dnsrelay.log 2>&1 &

[ -x /data/local/tmp/projector-remote ] && \
  nohup /data/local/tmp/projector-remote -listen :8080 \
  > /data/local/tmp/projector-remote.log 2>&1 &

# Tailscale. Its own script waits for DHCP, then adds the default route
# Android leaves out of the main table before starting tailscaled
# (DNS comes from dnsrelay, above).
[ -x /data/local/tmp/tailscale/tailscale-up.sh ] && \
  nohup /data/local/tmp/tailscale/tailscale-up.sh \
  > /data/local/tmp/tailscale/boot.log 2>&1 &

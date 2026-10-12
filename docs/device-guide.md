# Device Guide: Running Go Servers on the Projector

Operational reference for getting onto the projector over ADB and
running/deploying Go binaries on it, including making one start at
boot. Written from first-hand work on this exact unit — every command
and gotcha below was actually run against the device, not assumed.

## 1. Device facts

- Model/product: `t950s_be311` ("jixin" brand Android TV/projector box).
- Android 13, SDK 33, kernel 5.15.119, **armeabi-v7a** (32-bit ARM) —
  not arm64.
- Bootloader unlocked, `userdebug` build. `adb shell` normally runs as
  `shell` (uid 2000), which is enough to build, push, and start ordinary
  binaries from `/data/local/tmp`. Run `projector-remote` as root,
  however: its real-time picture controls force a SurfaceFlinger
  repaint after HWC geometry writes, and that transaction is rejected
  for `shell`. See §9 and `docs/projector-settings.md`.
- ADB-over-TCP is persistent across reboots. The boot patch requests
  **port 3333**, but this unit has also repeatedly come up on **5555**
  (`/system/bin/check_adb` was patched — see §6 — to unconditionally
  enable it every boot; stock firmware disabled ADB by default). The
  because Android's persisted wireless-debugging setting can win the
  startup race. The device's **IP changes on every power cycle**
  (DHCP). Try both ports after a reboot.
- No dedicated `recovery` partition exists on this device, and
  fastbootd support is unconfirmed. Treat any write to `/system` or
  `/vendor` as effectively unrecoverable without a prior backup (see
  §6) — there's no confirmed flash-your-way-out-of-trouble path.
- Known device quirk, unrelated to this guide but worth knowing:
  `com.jixin.apppush` (a system app) had a boot-triggered mechanism
  that fetched and executed scripts from a remote FTP server. It was
  disabled this session (`pm disable-user com.jixin.apppush`). If
  future work needs it re-checked: `pm list packages -d | grep jixin`.

## 2. Getting ADB access

```
adb connect <device-ip>:3333
adb connect <device-ip>:5555              # if 3333 refuses
adb devices -l                      # confirm it shows up as "device"
adb -s <device-ip>:<working-port> shell   # once multiple targets exist
```

Since the IP changes every power cycle, find the current one via your
router's DHCP client list, or via this repo's mDNS beacon
(`cmd/beacon` + `internal/discover`) — **note**: end-to-end discovery
was tested and failed on this dev network specifically because the
WiFi AP doesn't reflect client-originated multicast back to other
wireless clients (a common AP limitation, not a code bug — see
`internal/discover`'s tests and `cmd/server`'s `-target`/`set_target`
manual-override path, which exists precisely for this case).

`adb root` itself is not required. Pushing and ordinary execution from
`/data/local/tmp` work as plain `shell`; use the available `su 0`
wrapper when `projector-remote` needs root for picture repainting or
NetMirror token access.

## 3. Cross-compiling a Go binary for this device

**Critical gotcha, verified by an actual build failure:** do **not**
use `GOOS=android`. Go's `android` target always requires cgo
(external) linking — an NDK C toolchain — even for 100% pure-Go code
with no cgo calls:

```
$ GOOS=android GOARCH=arm CGO_ENABLED=0 go build ...
android/arm requires external (cgo) linking, but cgo is not enabled
```

Since the device just runs a normal Linux kernel underneath, the fix
is to target plain Linux instead — this produces a static binary that
runs directly on-device with no NDK, no Android app framework, no ART
involvement at all:

```
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o mybinary ./cmd/whatever
```

Verified end-to-end: built, pushed, `chmod`'d, and executed on this
exact device successfully. Any pure-Go dependency (no cgo) works fine
this way — confirmed with `github.com/hashicorp/mdns` (pulls in
`github.com/miekg/dns` and `golang.org/x/net`, all pure Go).

## 4. Deploying and running a server manually

```
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o mybinary ./cmd/whatever
adb push mybinary /data/local/tmp/mybinary
adb shell chmod 755 /data/local/tmp/mybinary
adb shell "nohup /data/local/tmp/mybinary [args] > /data/local/tmp/mybinary.log 2>&1 &"
```

For `projector-remote`, start it through root so zoom and corner mapping
repaint immediately:

```
adb shell "su 0 sh -c 'nohup /data/local/tmp/projector-remote -listen :8080 > /data/local/tmp/projector-remote.log 2>&1 &'"
```

Use an atomic `.new` upload and retain the previous binary when updating a
running installation; the complete sequence is in
`docs/projector-settings.md` under Verification.

Check it's alive and read its logs:

```
adb shell "ps -ef | grep '[m]ybinary'"     # bracket trick, see below
adb shell "cat /data/local/tmp/mybinary.log"
```

**Gotcha:** don't kill it with `pkill -f /data/local/tmp/mybinary`.
The pattern text also appears in the *invoking* `adb shell "..."`
command's own argv (since that string is literally part of the
command line you're running), so `pkill -f` matches and kills its own
invoking shell — this happened for real mid-session (exit code 143,
`adb: device offline` right after). Use the classic bracket-escape
grep trick instead, which avoids self-matching:

```
adb shell "ps -ef | grep '[m]ybinary' | awk '{print \$2}' | xargs -r kill -9"
```

## 5. Making it start at boot

The "normal" Android way — add a `service` stanza to a vendor `.rc`
file — needs write access to `/vendor` or `/system`, which on this
device means clearing a block-level read-only flag on the dynamic
partition device-mapper node, which is invasive on the **live root
filesystem** (see §6 for why, and why we didn't do it that way).

**Use the boot hook that's already wired up instead.** This project
already made `/system/bin/check_adb` reboot-persistent — it's a
652-byte script that vendor init (`/vendor/etc/init/hw/init.amlogic.rc`)
already runs exactly once per boot:

```
on property:sys.boot_completed=1
    start checkadb
```

Rather than re-patching `check_adb` itself every time something new
needs to start at boot, keep its raw-patched content minimal and have
it launch a plain script living on `/data` (ordinary writable
storage — no partition surgery needed to edit it):

```sh
#!/system/bin/sh
setprop service.adb.tcp.port 3333
settings put global adb_enabled 1
settings put global wifi_adb_enabled 1
start adbd
[ -x /data/local/tmp/startup.sh ] && /data/local/tmp/startup.sh &
```

Then `/data/local/tmp/startup.sh` (or wherever you put it) can be
freely edited via a plain `adb push` — no special patching — to launch
`mybinary`, the mDNS beacon, or anything else, since `check_adb`
re-invokes it every boot. This is the recommended path for adding new
boot-time behavior going forward.

**Confirmed live and working (2026-09-08):** the hook line above is
now actually present on the device (added via the §6 procedure) and
`/data/local/tmp/startup.sh` launches `cmd/server`'s binary
(`/data/local/tmp/projector-remote`). Verified end-to-end across a
real reboot: `check_adb`'s patch survived, `startup.sh` ran, the
server process came up on its own with no manual step, and it
answered HTTP requests. Current `startup.sh`:

```sh
#!/system/bin/sh
[ -x /data/local/tmp/projector-remote ] && \
  nohup /data/local/tmp/projector-remote -listen :8080 \
  > /data/local/tmp/projector-remote.log 2>&1 &
```

The absolute byte offset used for the `check_adb` patch (system_a's
start-in-super, 1,048,576, plus its data block's offset within
system_a, block 211890 × 4096-byte blocks = 867,901,440) was
**868,950,016** at patch time. Re-derive it fresh via §6's steps
rather than reusing this number blind if `check_adb`'s content, size,
or block placement ever changes.

**Port quirk observed after this reboot:** the device came back up
reachable on `:5555`, not the `:3333` `check_adb` explicitly sets via
`setprop service.adb.tcp.port 3333`. `:3333` refused the connection
entirely for several minutes after `sys.boot_completed`. Likely a
persisted `service.adb.tcp.port`/wireless-debugging property from an
earlier `adb tcpip 5555` racing with `check_adb`'s own property set
during boot. Try both ports when reconnecting after a reboot.

If `check_adb` doesn't have the trailing hook line for some other
device/session (check first with
`adb shell cat /system/bin/check_adb`), see §6 for exactly how to add
it safely — including the disk-space lesson below.

## 6. Reference: how `check_adb` was made persistent

Keep this section if `check_adb`'s content ever needs to change again.
Every step below was actually run and verified on this device.

**Why a normal edit doesn't work:** `check_adb` lives inside `system_a`,
a *logical* partition inside the physical `super` partition
(`/dev/block/by-name/super`), not a standalone raw partition. AVB's
hashtree verification is globally disabled on this unit (`vbmeta`
header flag `HASHTREE_DISABLED`, confirmed via `avbtool info_image`),
so filesystem content isn't checked at boot — but the dynamic-partition
device-mapper node backing `system_a` (`/dev/block/dm-0`, mounted as
`/`) carries its own **block-level** read-only flag, independent of
AVB. `mount -o remount,rw /` doesn't clear it, and `blockdev --setrw`
reports success but the flag reverts immediately — it's baked into the
dm **table**, and this device's `dmctl` only supports `create`/`delete`
of a dm device, not a live table swap. Deleting and recreating the dm
device backing the **live root filesystem** (with `/apex/*` and
everything else mounted through it) is too risky to attempt live.

**What actually worked: patch the raw bytes on the `super` block
device directly**, bypassing the mounted path (and its RO flag)
entirely, using a same-size replacement so no ext4 metadata (inode
size, extents, block bitmap) needs to change — only the file's
already-allocated data block content:

1. Back up first — always. Full recovery set (`super`, both boot
   slots, `vendor_boot`, `init_boot`, `vbmeta`, `vbmeta_system`,
   `dtbo`, `misc`) is ~2.85GB total; `/data` has ~10GB free.
   `adb pull /dev/block/by-name/<partition>` for each.
   **Pull to real disk, not a tmpfs-backed path** (e.g. not a
   `/tmp`-based scratch dir on a Linux host if that host's `/tmp` is
   tmpfs) — a `super` pull alone is ~2.6GB, and filling a small tmpfs
   doesn't just fail the copy, it can starve the host's own shell/tool
   processes of temp space and hang unrelated commands too (observed
   firsthand: an interactive session's shell stopped responding to
   *any* command, even `echo`, until the tmpfs was freed).
2. Find `system_a`'s extent within `super`:
   `adb shell lpdump /dev/block/by-name/super` → e.g. `system_a`
   starts at sector 2048, spans 2812656 sectors (byte offset
   1,048,576, length matches
   `blockdev --getsize64 /dev/block/mapper/system_a`).
3. Extract it from the backup: `dd if=super.img of=system_a.img bs=512
   skip=2048 count=2812656`.
4. Locate the target file's block:
   `debugfs -R "stat /system/bin/check_adb" system_a.img` → single
   `EXTENTS:` block number. Get the ext4 block size with
   `dumpe2fs -h system_a.img` (4096 on this device). Byte offset
   within `system_a.img` = block number × block size.
5. Absolute offset within `super` = system_a's start-in-super byte
   offset (1,048,576) + the offset from step 4.
6. Build the replacement content **exactly equal in byte length** to
   the original (pad with trailing `#` comment lines if shorter) —
   this is what avoids needing any ext4 metadata change.
7. **Verify offline first**, on the local backup copy, before touching
   the device: `dd` the replacement into a copy of `system_a.img` at
   the offset from step 4, then confirm with
   `debugfs -R "stat ..."` (same size/inode/extent),
   `debugfs -R "cat ..."` (content matches), and `e2fsck -fn` (clean,
   no corruption).
8. Only then, push the replacement bytes to the device and write them
   directly to the raw partition:
   `dd if=<replacement> of=/dev/block/by-name/super bs=1 seek=<absolute offset> count=<size> conv=notrunc`.
9. Verify by reading the same raw byte range back
   (`dd if=/dev/block/by-name/super bs=1 skip=<offset> count=<size>`,
   compare hashes) — **not** by `cat`-ing the mounted file
   immediately. Already-cached pages for that file from earlier
   `stat`/`cat`/`pull` in the same session will show stale content
   until the cache is dropped (`echo 3 > /proc/sys/vm/drop_caches`,
   confirmed this exact staleness firsthand) or the device reboots.
   The on-disk (and therefore next-boot) content is what actually
   matters.

## 7. Tailscale on this device

Tailscale runs natively here, in full **kernel TUN mode** — not
userspace-networking, and not the Android app. The official static
`linux/arm` build works as-is via §3's "target plain Linux" trick.
Verified end-to-end on this unit (2026-09-26): `UDP: true`, a direct
(non-DERP) connection to another tailnet node, nearest DERP Bengaluru
at ~110ms, and the `cmd/server` web UI answering HTTP 200 over the
tailnet in ~0.1s.

What's already in place, no work needed: `CONFIG_TUN=y` is compiled in,
`/dev/tun` exists (`crw-rw---- system:vpn`, with `/dev/net/tun`
symlinked to it), `/system/xbin/su` gives root (`su 0 <cmd>` — note
*not* `su -c`), and `ip`/`iptables`/`ip6tables`/`unshare` are all
present. `tailscaled` needs root, but only for the TUN device and
routing — no partition surgery, nothing written to `/system`.

**Two Android-specific quirks will silently break it.** Both cost real
debugging time; neither produces an error message that points at the
actual cause.

### Quirk 1: no default route in the `main` routing table

Android does not put a default route in `main`. It lives in a
per-network table (`wlan0` here), reached through netd's fwmark policy
rules — `ip rule show` on this device lists ~14 rules and no
`lookup main` at all.

Tailscale's netmon reads `main` to find the default route. Not finding
one, it concludes it has no usable network and **silently refuses to
send UDP**. The symptoms are misleading:

```
* UDP: false
* IPv4: (no addr found)
* Nearest DERP: unknown (no response to latency probes)
```

plus scattered `dial tcp <ip>:443: connect: network is unreachable`
errors in `tailscaled.log`, which look like a firewall or a routing
block. They are not — plain outbound TCP/UDP works fine from root the
whole time (`toybox nc <derp-ip> 443` succeeds), which is what makes
this so confusing to chase.

Fix — add the default route to `main` explicitly:

```
ip route replace default via <gateway> dev wlan0
```

This flipped `netcheck` to `UDP: true` with a public IPv4 mapping
immediately. Because no `ip rule` consults `main` on this device, the
entry is **inert for actual packet routing** — nothing about Android's
own networking changes. It exists purely so netmon can see it. It does
not survive a reboot, so it has to be re-added at boot.

### Quirk 2: no `/etc/resolv.conf`, so static Go binaries can't resolve DNS

Android resolves DNS through netd via bionic
(`android_getaddrinfofornet`). A **static Go binary has no access to
that**, and this device has no `/etc/resolv.conf` and no `net.dns*`
properties for Go's resolver to read either (`/etc` is a symlink to
`/system/etc`, which is read-only, and `resolv.conf` simply doesn't
exist there). Go therefore falls back to its built-in default
nameservers and fails:

```
lookup controlplane.tailscale.com on [::1]:53: read udp ...: connection refused
```

Note this applies to **any** static Go binary on this device that
resolves hostnames, not just Tailscale — worth knowing before writing
another one. `cmd/server` is one: it resolves NetMirror's hosts for
the catalog and playback (§8).

Go's fallback address is the fix: it tries `127.0.0.1:53` and
`[::1]:53`, so serving DNS *there* works with **no filesystem changes
at all**. `cmd/dnsrelay` is a ~60-line UDP forwarder that listens on
both and relays to the LAN gateway.

Rejected alternative, for the record: overlay-mounting a `resolv.conf`
over `/system/etc` inside a private mount namespace
(`unshare -m` + overlayfs, both supported here —
`CONFIG_OVERLAY_FS=y`). It needs a bind-mount of `/system/etc` to a
staging dir first, since overlayfs refuses a lowerdir equal to its own
mountpoint. It works, but mounting over the directory that holds the
system CA store is a bad trade for one file. The localhost resolver is
strictly simpler and touches nothing.

TLS is not a problem despite `/etc/ssl` being absent — Tailscale
embeds its own roots for the control plane, confirmed by a successful
`https://controlplane.tailscale.com` DERPMap fetch.

### Deploying it

```
curl -LO https://pkgs.tailscale.com/stable/tailscale_<ver>_arm.tgz
tar xzf tailscale_<ver>_arm.tgz
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o dnsrelay ./cmd/dnsrelay

adb push dnsrelay deploy/dnsrelay-up.sh deploy/startup.sh /data/local/tmp/
adb shell 'chmod 755 /data/local/tmp/dnsrelay /data/local/tmp/dnsrelay-up.sh /data/local/tmp/startup.sh'

adb shell 'su 0 mkdir -p /data/local/tmp/tailscale/state'
adb push tailscale_<ver>_arm/tailscaled tailscale_<ver>_arm/tailscale \
  deploy/tailscale-up.sh /data/local/tmp/tailscale/
adb shell 'su 0 chmod 755 /data/local/tmp/tailscale/*'
```

`dnsrelay` lives outside the Tailscale directory on purpose: the
remote's NetMirror catalog needs DNS too, and shouldn't stop working
when Tailscale doesn't come up.

Authenticate once, reading the key from a file so it never lands in
`ps` output or shell history:

```
adb shell 'su 0 env HOME=/data/local/tmp/tailscale \
  /data/local/tmp/tailscale/tailscale \
  --socket=/data/local/tmp/tailscale/tailscaled.sock up \
  --auth-key=file:/data/local/tmp/tailscale/.tskey \
  --hostname=projector --accept-dns=false --accept-routes=false'
```

then delete the key file. `tailscale up` prints **nothing on success** —
check `tailscale status`, don't read silence as failure.

`--accept-dns=false` is deliberate: `tailscaled` can't manage DNS on
this device anyway (it logs `dns: OScfg: {}`), so MagicDNS names won't
resolve *from* the projector. Reaching the projector *by* its tailnet
name from elsewhere works normally.

Lock the directory down afterwards — it holds the node key, and the
control socket is otherwise usable by any app on the device:

```
adb shell 'su 0 chmod 700 /data/local/tmp/tailscale /data/local/tmp/tailscale/state'
```

With that, `adb shell` as plain `shell` (uid 2000) can no longer touch
it; use `su 0` for the CLI.

### Boot-starting it

Uses §5's existing hook. `deploy/startup.sh` launches, in order:

1. `deploy/dnsrelay-up.sh`: waits up to 120s for DHCP to hand out a
   gateway (at `sys.boot_completed` it usually hasn't yet), then runs
   `dnsrelay` pointed at it (quirk 2).
2. `projector-remote`.
3. `deploy/tailscale-up.sh`: waits for the gateway the same way,
   re-adds the `main`-table default route (quirk 1), and starts
   `tailscaled`, which retries DNS until `dnsrelay` is up.

(Until 2026-09-28 `tailscale-up.sh` started `dnsrelay` itself; the
previous versions are kept on the device as `*.prev`.)

No auth key is needed at boot: `$T/state/tailscaled.state` persists
both the node key and `WantRunning=true`, so `tailscaled` reconnects on
its own. Set `HOME` for it — unset, it tries to cache logs in
`/.cache` on the read-only root.

**Not yet verified across a real reboot** (unlike §5's
`projector-remote` hook, which was). The wiring is identical in shape,
but the first power cycle is still the real test — check
`/data/local/tmp/tailscale/boot.log` afterwards.

## 8. The NetMirror player

The remote can browse NetMirror's catalog and play titles on the
projector through its own player app (`player/`, Media3 ExoPlayer);
see `docs/netmirror-api.md` for the API side. Things about this device
that it depends on, all found the hard way:

- **Go needs to be told where Android keeps CA certificates.** A
  `GOOS=linux` build only looks in Linux locations (`/etc/ssl/certs`
  and friends), which don't exist here, so every HTTPS request fails
  with `x509: certificate signed by unknown authority`. `cmd/server`
  sets `SSL_CERT_DIR=/system/etc/security/cacerts` at startup unless
  it's already set. Any other Go binary that makes HTTPS calls here
  needs the same.
- **The NetMirror login can come from the phone or the app.** Playback
  needs the `usertoken` NetMirror issues after its OTP step, and it
  expires. When it does, playback fails with "NetMirror needs you to
  log in again" and the phone's Now Playing screen shows a login card
  with NetMirror's code picture. Entering the code there makes the
  server call `otp.php` from the projector and save the token to
  `/data/local/tmp/projector-remote-token` (set by `-token`). The
  server also reads the token the NetMirror app saves when you log in
  there, from its MMKV store
  `/data/data/app.netmirror.newtv/files/mmkv/mmkv.default`. Only root
  can read that file: fine when started from the boot hook (root), and
  the server falls back to `su 0 cat` when run by hand as `shell`.
  Whichever token was issued later wins. See `netmirror-api.md` §5.
- **The screensaver covers anything started under it.** When idle the
  projector "dreams" (`com.jixin.screensaver`), and an activity launched
  with `am start` opens *behind* the dream. The server sends
  `input keyevent KEYCODE_WAKEUP` before launching the player, which
  ends the dream (and does nothing when already awake).
- **Watch history** (positions, continue watching, track preferences)
  is `/data/local/tmp/projector-remote-state.json`, set by `-state`.

Build and install the player:

```
cd player
./gradlew assembleDebug          # JDK 21; `mise install` in player/ provides it
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

It has no launcher icon; the server starts it
(`am start -n com.projectorremote.player/.PlayerActivity`) and talks to
it over `ws://127.0.0.1:8080/player`, which accepts local connections
only.

More about this device that playback depends on:

- **Cleartext is allowed in the player.** The laptop media library
  (`docs/media-library.md`) serves plain HTTP inside the tailnet, at an
  address only known at run time, so `network_security_config.xml`
  permits cleartext generally; Android 14+ blocks it otherwise
  (`ERROR_CODE_IO_CLEARTEXT_NOT_PERMITTED`).
- **Only some of the shipped decoders are active.** `/vendor/etc` holds
  `media_codecs_amlogic_audio_ddp.xml` (AC-3/E-AC-3) and
  `..._audio_dtshd.xml`, but `media_codecs.xml` doesn't `<Include>` them,
  so ExoPlayer has no Dolby or DTS decoder: those tracks are listed as
  unsupported and play silent. The server follows the includes when it
  reports decoders to the media library, which then converts such audio
  to AAC. Active video decoders: `c2.amlogic.avc` (to 1920),
  `c2.amlogic.hevc` (to 4096, 10-bit), VP9 (to 2048), MPEG-2/4.
- **No HDR.** The display reports no HDR types (`dumpsys display`), so
  HDR sources are tone-mapped before they get here.
- **ADB over Tailscale** (`adb connect <projector tailnet IP>:5555`)
  keeps working when DHCP moves the LAN address.

## 9. Picture settings and real-time geometry

The remote can control the projector's OEM zoom, focus, manual
keystone, four-corner mapping, projection orientation, and automatic
correction features. The complete provider/method inventory,
four-corner property layout, orientation transforms, WebSocket
protocol, caching strategy, timings, and diagnostic commands are in
`docs/projector-settings.md`.

Two operational details matter here:

1. Start the server as root. Geometry writes are fast, but forcing the
   static projected frame to repaint uses root-only SurfaceFlinger
   transaction 1004.
2. The complete vendor settings read is slow (11.161 seconds in the
   latest cold measurement). The server warms it once at startup and
   serves a maintained in-memory snapshot on later PWA connections.
   Log line `projector settings cache ready` confirms completion.

## Related files in this repo

- `README.md` — build/run instructions for `cmd/server` (the control
  server, which runs directly on the device).
- `docs/projector-settings.md` — picture-setting providers, geometry,
  remote protocol, real-time update behavior, and diagnostics.
- `docs/superpowers/specs/2026-09-08-projector-remote-design.md` —
  overall architecture.
- `cmd/beacon/` — the mDNS/DNS-SD advertiser built and tested using
  everything in this guide; its `main.go` doc comment repeats the
  `GOOS=linux` requirement inline.
- `internal/discover/` — the host-side mDNS browse/resolve client.
- `cmd/dnsrelay/` — the localhost DNS forwarder from §7, needed by
  any static Go binary on this device that resolves hostnames.
- `deploy/startup.sh`, `deploy/dnsrelay-up.sh`, `deploy/tailscale-up.sh`
  — the on-device boot scripts from §5 and §7, kept in the repo so
  what's running on `/data/local/tmp` is reviewable and re-pushable.
- `player/` — the Android player app from §8.

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
  `shell` (uid 2000) — enough for everything in this guide (`/data`
  and `/data/local/tmp` are writable/executable by `shell`, no root
  needed to deploy or run a server).
- ADB-over-TCP is persistent across reboots on **port 3333**
  (`/system/bin/check_adb` was patched — see §6 — to unconditionally
  enable it every boot; stock firmware disabled ADB by default). The
  device's **IP changes on every power cycle** (DHCP) — the port does
  not.
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
adb devices -l                      # confirm it shows up as "device"
adb -s <device-ip>:3333 shell       # once multiple targets exist
```

Since the IP changes every power cycle, find the current one via your
router's DHCP client list, or via this repo's mDNS beacon
(`cmd/beacon` + `internal/discover`) — **note**: end-to-end discovery
was tested and failed on this dev network specifically because the
WiFi AP doesn't reflect client-originated multicast back to other
wireless clients (a common AP limitation, not a code bug — see
`internal/discover`'s tests and `cmd/server`'s `-target`/`set_target`
manual-override path, which exists precisely for this case).

`adb root` may or may not be available/needed — not required for
anything in this guide (pushing/running a binary from
`/data/local/tmp` works as plain `shell`).

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

If `check_adb` doesn't already have that trailing hook line (check
first with `adb shell cat /system/bin/check_adb`), see §6 for exactly
how to add it safely.

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

## Related files in this repo

- `README.md` — build/run instructions for `cmd/server` (the control
  server, normally run on a host machine, not the device).
- `docs/superpowers/specs/2026-09-08-projector-remote-design.md` —
  overall architecture.
- `cmd/beacon/` — the mDNS/DNS-SD advertiser built and tested using
  everything in this guide; its `main.go` doc comment repeats the
  `GOOS=linux` requirement inline.
- `internal/discover/` — the host-side mDNS browse/resolve client.

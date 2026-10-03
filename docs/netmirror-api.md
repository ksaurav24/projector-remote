# NetMirror API and Navigation Notes

Reverse-engineered reference for `app.netmirror.newtv`, the sideloaded
streaming app on the projector. Captured 2026-09-28 by decrypting the
app's HTTPS with mitmproxy (see "How this was captured" below). Purpose:
figure out what a remote can drive from outside the app (shortcuts,
remote catalog browsing, start-on-projector playback).

Everything here was observed from real traffic on this unit. Values like
CDN hostnames and tokens rotate, so treat specific numbers as examples.

## 1. What the app actually is

NetMirror is a **single-activity React Native app**:

- One exported activity: `app.netmirror.newtv/.MainActivity`
  (`MAIN` + `LAUNCHER` + `LEANBACK_LAUNCHER`). Nothing else is exported.
- React Native confirmed by its storage: `databases/RKStorage` (RN
  AsyncStorage), `files/mmkv/mmkv.default` (MMKV), and
  `assets/index.android.bundle` (the JS bundle) in the APK. It uses
  `react-navigation` (native screen stack); a `checkOtpScreen` component
  exists in the bundle. The desktop-Firefox User-Agent with a
  ` /OS.GatuNewTV v1.0` suffix is what its API client sends.
- All in-app navigation (home, OTT switch, search, detail, player, the
  OTP/PIN screen) is internal JS/`react-navigation` state. There are **no
  per-screen activities** and **no deep-link intent filters, URI schemes,
  or RN `Linking` handlers**. The only external entry point is launching
  `.MainActivity` to its home screen.

This one fact drives every answer in section 7.

**On the app's OTP/6-digit PIN:** the server only issues a `usertoken`
to a client that has passed the OTP step, and playback needs that
token. Browsing works without it. The OTP isn't secret: NetMirror
publishes it in an image the OTP screen shows, and any client can
submit it to `otp.php` for a token (section 5, verified 2026-09-29).

## 2. Hosts and infrastructure

| Host | Role |
|------|------|
| `tv.imgcdn.kim` | API backend (`/newtv/*.php`), Cloudflare-fronted |
| `imgcdn.kim` | Poster / artwork images |
| `net52.cc` | Web origin; required as `Referer` for images and streams |
| `s12.freecdn39.top` | Video/audio HLS segment CDN (host number rotates) |
| `pv.subscdn.top` | Subtitle CDN |
| `m.media-amazon.com` | Prime artwork (upstream, for `pv` catalog) |

Third-party ad / tracking calls the app also makes (not needed for the
remote, listed for awareness): `*.api.leiniao.com` (CTV ad SDK),
`open.cctvcloud.cn`, `mobiledetects.com/checknewtv.php` (device
fingerprint), `ipapi.co` + `myip.ipip.net` (geo), and
`www.googleapis.com/androidantiabuse`.

## 3. Browse / metadata routes

All on `https://tv.imgcdn.kim/newtv/`. All are `GET`, return JSON, and
require the header from section 5.

| Route | Purpose | Notable response fields |
|-------|---------|-------------------------|
| `navbar.php` | Nav config | logos, `ott` mode |
| `main.php` | Home feed | `slider`, `post[]` (rows of `ids`), `recentplay`, `watchlist`, `usertoken` |
| `ott.php` | OTT skin list | `ott[]` = `nf` (Netflix), `pv` (Prime), `hs` (Hotstar), each with logo/bg |
| `post.php?id=<ID>` | Title detail | `title`, `year`, `ua`, `runtime`, `hdsd`, episode + more-like poster bases, `usertoken` |
| `search.php?s=<query>` | Search | `searchResult[]` of `{id, t}`, or `"No Result Found!"` |
| `recentplay.php` | Resume list | `{key:"pv_recentplay", value:"<csv of ids>"}` |
| `episodes.php?id=<seasonID>&page=<n>` | One page (10) of a season's episodes | `episodes[]`, `nextPageShow`, `nextPage` |
| `otp.php` | OTP login (section 5) | `pub_msg_img.img` (shows the code); with an `otp` header, `status` + `usertoken` |

`?s=<season>&series=<id>` on `episodes.php` does not work; it's `id=`.

**Series** (verified 2026-09-28). `post.php` for a series returns
`type:"t"`, `season[]` (`{s:"Season 1 (7 EP)", id, selected}`), the
first page of `episodes[]` for the **latest** season (`selected:true`),
and `nextPageShow` (1 = more pages), `nextPage`, `nextPageSeason`.
Movies return `type:"m"` and `episodes:[null]` (Netflix) or no
`season` key at all (Prime). An unknown ID still returns
`status:"ok"`, just with `title` and `type` null.

Each episode is `{id, t, ep, ep_desc, info[]}`, and `info` differs per
platform: Netflix `["E1","S1","38m"]`, Prime
`["Jun 23, 2026","U/A 16 ","38m"]`, Hotstar `["E1:S1","3 Jul 2026","36m"]`
(Hotstar also omits `ep_desc`). The runtime is always the last entry.

**Images.** Responses carry URL templates with `------------------` in
place of the ID, and the paths differ per platform, so use the
templates rather than hardcoding: `main.php` has `imgcdn_h` (wide
cards, row layout `"h"`) and `imgcdn_v` (tall, layout `"v"`, the Top 10
rows); `post.php` has `main_poster` (backdrop), `ep_poster` (episode
stills) and `morelike_poster`. Examples: `/poster/341/<ID>.jpg`
(Netflix wide, 341x192), `/poster/v/<ID>.jpg` (Netflix tall, 284x398),
`/pv/341/<ID>.jpg`, `/pv/c/<ID>.jpg` (Prime backdrop, ~1.7MB),
`/hs/v/166/<ID>.jpg` (Hotstar, 166x224, used for both layouts),
`/epimg/150/`, `/pvepimg/341/`, `/hsepimg/` (episodes).
Images load with any `Referer`, or none, so a browser can show them
directly (verified; the app sends `net52.cc` but it isn't checked).

## 4. Playback routes

1. `GET /newtv/player.php?id=<ID>` →
   `{status:"ok", video_link, referer:"https://net52.cc", title, ep, ep_title}`.
   `video_link` is the HLS master with the stream token already baked in.
2. `GET /newtv/hls/<ott>/<ID>.m3u8?in=<streamtoken>` with
   `Referer: https://net52.cc` → HLS master playlist (subtitle tracks +
   video/audio variants). **This is the only call that consumes the token.**
3. Segments: `s12.freecdn39.top/files/<ID>/1080p/*` (video),
   `/a/<n>/*` (audio tracks). **Segments are disguised** as `.js`,
   `.woff2` or `.jpg` files, served with matching fake content types
   (`font/woff2`, `image/jpeg`), an anti-scraping trick; underneath
   they are plain MPEG-TS (first byte `0x47`). Media3's HLS extractor
   ignores the unknown types and sniffs the content, so it plays them
   as-is. The manifests are normal `.m3u8`. Subtitles:
   `pv.subscdn.top/subs/<ID>/<lang>.m3u8` + `.vtt`.
4. **Playlists understate the media's length.** The `#EXTINF` total can
   be a minute or so short of the real media, so playback runs past the
   stated duration; left alone, Media3 raises a stuck-player timeout
   (`ERROR_CODE_TIMEOUT`) 60s later. The player here treats the stated
   duration as the end.

### Token propagation and its two teeth

The token is injected into URLs by the server, not computed by the
player. Fetching the tokenized master (`?in=<token>::t`) makes the server
rewrite every child (variant/audio/segment) URL to carry a signed token:

```
https://s12.freecdn39.top/files/<ID>/1080p/1080p.m3u8?in=<PUBLIC_IP>::<hash>::<unixtime>::ni
```

- **IP-bound:** the token embeds the *public IP of whoever fetched the
  master* (observed: the projector's `103.31.40.16`). A tokenless or
  wrong-session fetch gets `?in=unknown::ni` and the CDN rejects it. So
  the master and the media segments must be fetched from the **same public
  IP**.
- **Time-limited:** the token carries a unix timestamp; a fresh
  `player.php` resolve is needed per play, and a long title may need a
  re-resolve mid-playback if the signed URLs expire.

Because any standard HLS player just follows the manifest, the token
rides in the URLs automatically — the player needs no token logic. "Any
player works" is therefore true, provided (a) it is handed the freshly
tokenized `video_link`, and (b) playback happens from the same public IP
that resolved it.

### Track selection (audio / subtitles / quality)

The master is a standard multi-variant HLS playlist, so audio, subtitle,
and quality selection are **player features, not NetMirror features** —
fully switchable live by any real HLS player. For the captured Prime
title the master declared:

- **Audio:** 6 tracks (`TYPE=AUDIO` group `aac`) — Bengali, Hindi,
  Kannada, Malayalam, Tamil, Telugu.
- **Subtitles:** 16 WebVTT tracks on `subscdn.top` — English, Hindi[CC],
  Tamil, Telugu, Kannada, Malayalam, Bengali, Indonesian, Malay, Turkish,
  Arabic, Thai, Chinese (simp/trad), Japanese, Korean.
- **Quality:** 2 variants — 1920x816 @ 1 Mbps ("1080p"), 1280x544 @
  600 kbps ("720p", the default).

Switching tracks needs **no re-auth**: every track URL is already
tokenized inside the one loaded manifest, so a switch just pulls a
different track's segments under the same token.

## 5. Auth strategy

Anonymous. No account, **no cookies, no server-side session**; the only
login is a shared OTP that unlocks playback (item 4).
State is carried entirely by request headers the client sets itself:

1. **`x-requested-with: NetmirrorNewTV v1.0`** on every API call (UA also
   ends in ` /OS.GatuNewTV v1.0`). Any client sending this gets data. This
   is the entire "are you the app" check.

2. **`ott: <nf|pv|hs>`** header selects which platform catalog every call
   operates on (see section 6). Client-controlled, sent on each request.

3. **`Referer: https://net52.cc`** required for image and stream (HLS)
   requests.

4. **`usertoken`**: a stream token, shape `<hex32>::<hex32>::<unixtime>::ni`,
   where `<unixtime>` is when it was issued. The client sends it **as a
   request header** (`usertoken: <value>`) to `player.php`, which
   returns the master-playlist URL with the token as `?in=<variant>::t`.
   Tokens expire: one issued 2026-09-28 05:47 was rejected by
   2026-09-29 22:28 (exact lifetime unknown).

   Without a valid token, `main.php`/`post.php` answer
   `status:"otp"`, `usertoken:""`, and `player.php` answers
   `status:"otp"` with a `video_link` that has **no** `?in=` token,
   which the CDN rejects.

### Getting a token: `otp.php`

The OTP step is one route, `GET /newtv/otp.php`, with the usual
`x-requested-with` header (`ott` is not needed):

- **Without an `otp` header** it returns the prompt:
  `{"otp":"", "pub_msg_img":{"img":"https://tv.imgcdn.kim/otp2.jpg", ...}}`.
  The image spells out the current code (it has read "111111 is OTP
  for Login").
- **With an `otp: <code>` request header** it checks the code.
  Right: `{"otp":"111111", "status":"ok", "usertoken":"<token>", ...}`.
  Wrong: `{"status":"error", "error_msg":"Invalid OTP, Please Enter Valid OTP", ...}`.
  The code is not accepted as a query parameter or form field, only as
  the header.

A token from `otp.php` works from any client. Call it from the machine
that will play, since the stream URLs it unlocks are signed to the
caller's public IP (section 4).

The app does the same and keeps the result in its MMKV store
(`files/mmkv/mmkv.default`, key `usertoken`). Reusing that token from a
non-app client also works (verified 2026-09-28). **Reading MMKV
correctly:** the file starts with a little-endian `uint32` giving the
length of the valid data, and MMKV appends each write. So the current
value is the *last* one inside that length. Bytes past it are
leftovers from before the last compaction and can hold an older,
expired token. Scanning the whole file picks that one up.

The projector-remote server supports both: `POST /api/catalog/login`
calls `otp.php` from the projector and saves the token
(`internal/netmirror.TokenFile`), and `internal/netmirror.MMKVToken`
reads the app's. Playback uses whichever was issued later.

See section 4 for how the token then propagates into the media URLs — it
is **not** something a player computes. So: browse/search/detail =
`x-requested-with` + `ott` headers. Play = call `player.php` (with
`usertoken` + `ott` headers) for a ready-to-use, tokenized `video_link`,
then fetch with `Referer: net52.cc`.

## 6. OTT model — a client header (confirmed)

OTT choice (`nf` / `pv` / `hs`) is set by the **`ott` request header**,
which the client controls. There is no server-side session and no cookie:

- Sending `ott: nf`, `ott: pv`, or `ott: hs` on `main.php` (etc.) returns
  that platform's catalog — verified from a fresh client with no prior
  state. `nf` and `hs` use numeric IDs; `pv` uses 26-char alphanumeric
  (`0OML6W0QFI45I4D5HK2UTCGGFQ`).
- The `ott` value must match the content ID's platform. A `pv` ID with
  `ott: nf` fails (`player.php` builds a wrong `hls/nf/...` URL →
  `"Video ID Missing."`); the same ID with `ott: pv` resolves and plays.
- `main.php?ott=hs` as a *query param* is ignored — it must be the
  **header**, not the query string. (This was the earlier red herring.)
- `ott.php` just returns the list of available platforms for the picker
  UI; selecting one only changes which `ott` header the WebView then
  sends. There is no separate "set OTT" server call.

So a remote can switch OTT freely at any time by choosing the header
value on its next request — nothing to persist or negotiate.

## 7. What a remote can and cannot drive — shortcut feasibility

Direct answers to the two questions that motivated this doc.

### "Shortcut to change the OTT from the remote?"

- **Via an Android intent / deep link into the running app: not
  possible.** The app has one activity and no intent filters, so the only
  thing an external app (or `am start`) can do is launch it to its home
  screen. It cannot be told "open Prime".
- **Via the API: yes, trivially.** OTT is just the `ott` request header
  (section 6). A remote sends `ott: nf|pv|hs` on its calls and gets that
  platform's catalog immediately — no session, no persistence, switchable
  per request. This makes a per-OTT shortcut on the remote easy.

### "See all series on the remote, pick one, it starts on the projector?"

Fully achievable, and it bypasses the app entirely:

- **List/browse on the remote: yes** — call `main.php` / `search.php` /
  `post.php` with `x-requested-with` + the desired `ott` header. Works for
  any platform, switchable freely.
- **Start it on the projector: yes** — but resolve + play must happen
  from the projector (same public IP as the token; see section 4):
  1. The **projector** calls `player.php?id=<ID>` (with `usertoken` +
     `ott` headers; the token comes from `otp.php` or the app's MMKV,
     section 5) → reads the tokenized `video_link`.
  2. The projector plays that HLS URL in a player it controls (with
     `Referer: https://net52.cc`).

  Architecture: the remote UI sends only the **content ID + OTT** to the
  projector; the projector resolves and plays, so the token's baked-in IP
  matches the box doing playback. A remote resolving the token on a
  different public IP would have its segments rejected.

- **Audio / subtitle / quality from the remote UI: yes, live** — use a
  controllable player on the projector (mpv with its JSON IPC socket:
  `set aid`/`set sid`/`set vid`; or ExoPlayer `TrackSelectionParameters`).
  The remote UI lists the manifest's tracks (section 4) and sends a
  switch command to the projector; no re-auth needed. This is the main
  reason to prefer a self-controlled player over the two options below.

### Recommended approach for the shortcut feature

Treat NetMirror as a **headless HTTP catalog + HLS source**, not an app
to remote-control:

- The projector-remote server calls the `.php` API directly
  (`x-requested-with` + `ott` headers) to power a browse/search UI on the
  remote.
- OTT switching is just the `ott` header value — expose `nf`/`pv`/`hs` as
  a toggle on the remote.
- On selection, resolve `player.php` → `video_link` and play the HLS
  stream on the projector with an external player
  (`Referer: https://net52.cc`). No WebView, no dpad scripting, no deep
  links, no token to manage.

### What was built

This is now implemented: `internal/netmirror` (API client and token),
`internal/catalogapi` (the phone's `/api/catalog/*`), `internal/playback`
(resolving, next episode, resume, and driving the player), and
`player/` (the ExoPlayer app on the projector). Device-specific setup
is in `device-guide.md` §8.

## 8. How this was captured

mitmproxy 12 on the laptop, projector's global HTTP proxy pointed at it,
mitmproxy CA added to the device trust store via a non-persistent
bind-mount over `/system/etc/security/cacerts` (this device's `/system`
is block-level read-only; see `device-guide.md` §6). The WiFi AP isolates
wireless clients, so the proxy connection was routed over Tailscale
instead of the LAN. NetMirror does **not** pin certificates, so TLS
decrypted cleanly. Full setup steps are in the session memory notes
(`projector-mitmproxy-setup`).

# Media library (laptop)

**Browse → Library** plays video files stored on a laptop. A media server
(`cmd/mediaserver`) runs on the laptop next to the files and does all the
work: indexing, metadata, subtitle conversion, and encoding to HLS. The
projector finds it over Tailscale, relays the catalogue to the phone, and
its player fetches the HLS stream straight from the laptop, the same way
it plays NetMirror and anime streams. The Library tab is shown only while
the media server is reachable.

```
Phone (PWA) ──LAN──▶ projector server ──tailnet──▶ laptop media server
                     (catalogue JSON,               (index, ffmpeg, HLS,
                      artwork relay)                 subtitles, artwork)
                          │                                ▲
                          ▼                                │
                     player app ────────── HLS over the tailnet
```

## Running it

Needs `ffmpeg`/`ffprobe` and Tailscale on the laptop, and the same tailnet
as the projector. The projector needs the current player APK (it allows
plain-HTTP streams, which the laptop serves inside the tailnet; an older
player fails with `ERROR_CODE_IO_CLEARTEXT_NOT_PERMITTED`) and a server
built from this tree.

```bash
go build -o ~/.local/bin/projector-media ./cmd/mediaserver
cp deploy/projector-media.service ~/.config/systemd/user/
echo 'TMDB_API_KEY=<v4 read access token or v3 key>' > ~/.config/projector-media.env
chmod 600 ~/.config/projector-media.env
systemctl --user enable --now projector-media
journalctl --user -u projector-media -f      # what it's doing
```

The service runs `projector-media -media ~/Media`; edit `ExecStart` for
another folder. It can also be run by hand with the same flags. The
projector checks for it every 30 seconds, so the Library tab appears
shortly after the laptop comes online and disappears when it sleeps.

Flags:

| Flag | Default | |
| --- | --- | --- |
| `-media` | `~/Media` | The library folder |
| `-listen` | `:8770` | Only loopback and tailnet addresses are answered; there's no login |
| `-cache` | `~/.cache/projector-media` | Index, metadata, subtitles, thumbnails and segments |
| `-cache-gb` | `30` | Disk for cached HLS segments; least recently watched go first |
| `-tmdb-key` | `$TMDB_API_KEY` | TMDB v3 key or v4 read token. Without one, titles come from names, `.nfo` files and local artwork |
| `-tmdb-lang` | `en-US` | Metadata language |
| `-max-height` | `1080` | Resolution cap for transcodes (copies keep the source's) |
| `-max-copy-mbps` | `80` | Heavier files are transcoded so Wi-Fi keeps up |
| `-encoder` | best available | `h264_nvenc`, `h264_qsv` or `libx264` |

On the projector, `cmd/server` finds the laptop with `tailscale status`
(`-tailscale`, `-tailscale-socket`) and probes port `-media-port` (8770)
on each online computer. `-media-server http://100.x.y.z:8770` skips
discovery.

## Folder layout and names

The folder is read once at startup and then watched; nothing is listed
per request. Added, renamed and deleted files show up within seconds. A
file is indexed only after it has gone 5 seconds without writes, so
downloads in progress don't appear half-written. The probe results are
cached, so a restart only re-reads files that changed.

Shows are grouped by folder, falling back to the name in the file:

```
TV Shows/Breaking Bad/Season 1/Breaking.Bad.S01E01.720p.BluRay.x264.mkv
TV Shows/Doctor Who/Series 4/05 The Fires of Pompeii.mkv
TV Shows/Dark (2017)/S03/Episode 4.mkv
Breaking.Bad.S02.1080p.BluRay/Breaking.Bad.S02E05.mkv      (season pack)
Anime/Frieren/[SubsPlease] Sousou no Frieren - 05 (1080p) [A1B2C3D4].mkv
Movies/Inception (2010)/inception.2010.1080p.bluray.mkv
The.Matrix.1999.2160p.UHD.BluRay.REMUX.HDR.mkv
```

- Episode numbers: `S01E02`, `s1e2`, `S01.E02`, `1x02`, `Season 1 Episode 2`,
  `Episode 12`, `Ep12`, `E12`, ` - 12` (fansub), a leading `01`/`101` inside
  a season folder, and multi-episode `S01E01E02`/`S01E01-E03`.
- Season folders: `Season 1`, `Season01`, `S01`, `Series 2`, `Specials`
  (season 0, listed last), other languages' words for season, and
  misspellings within two letters (`Seson 2`, `Saeson 2`).
- Case, dots, underscores, `&`/`and`, a leading `The` and accents don't
  split a show. Release tags (`1080p`, `WEB-DL`, `x265`, group names in
  brackets) are dropped from titles; years in `()` or `[]` are kept as
  the year.
- Skipped: `Sample`/`Extras`/`Featurettes`/`Trailers` folders, `*sample*`
  files, hidden folders.
- Duplicate copies of one episode or movie: the higher resolution wins.
- Episodes without a number follow the numbered ones in name order.
  Absolute anime numbering (`One Piece - 1085`) is mapped to TMDB's
  seasons for episode names.

Metadata, in priority order: Kodi `.nfo` files (`tvshow.nfo`, `movie.nfo`,
`<video>.nfo`, including a TMDB ID or URL in them), then TMDB, then the
parsed names. Artwork: `poster.jpg`/`folder.jpg`/`cover.jpg`,
`fanart.jpg`/`backdrop.jpg`, `<video>-poster.jpg`, `<video>-thumb.jpg`,
`season01-poster.jpg`, then TMDB, then a frame grabbed from the video.

## Streaming

The projector reads its decoders once at startup from Android's
`media_codecs.xml` and the files it `<Include>`s (the list Android
itself uses; this projector also ships Dolby and DTS decoder lists that
aren't included, so aren't active). Each play request carries them, so
the laptop decides per file and device. On this projector that's H.264
up to 1920 and HEVC (10-bit) up to 4096, with AAC and MP3 audio; the
display reports no HDR support.

- **Copied** when the projector decodes it: H.264, or HEVC (10-bit
  when the decoder handles 4K), SDR, within the decoder's size and under
  `-max-copy-mbps`. No re-encoding, so quality is the source's.
- **Transcoded** otherwise (MPEG-4/Xvid, VC-1, AV1, VP9, 10-bit H.264,
  HDR, too large): H.264 High on NVENC (`p5`, constant quality 20, spatial
  and temporal AQ, B-frames), decoded and scaled on the GPU where NVDEC
  supports the codec. HDR10 and HLG are tone-mapped to SDR (Hable) after
  a GPU downscale. Interlaced video is deinterlaced (`bwdif`).
- **Lower qualities** are offered beneath the top one, fitted into 16:9
  boxes so wide films keep their shape: 1080p at 6 Mb/s (under a 4K
  copy), 720p at 3 Mb/s, 480p at 1.2 Mb/s, each only below a larger
  source. The player switches between them as the Wi-Fi allows; the
  remote's quality picker can pin one.
- **Audio**: every track is a separate HLS rendition, so switching
  language in the remote's player doesn't restart anything. Codecs the
  projector decodes (here AAC and MP3) are copied; AC-3, E-AC-3, DTS,
  TrueHD, FLAC, Opus and the rest become 48 kHz AAC (stereo 192k, 5.1
  384k).

Segments are made on demand, starting wherever the player asks: one
ffmpeg process per video quality in use, and one for all the audio
tracks, so a quality switch only starts the new quality. A seek beyond
what's been made restarts ffmpeg there. ffmpeg pauses once it's 3
minutes ahead of the player, stops after 45 seconds of no requests, and
stops for an older stream when a new one starts. Finished segments stay
on disk (up to `-cache-gb`), so a rewatch or a seek back is instant.

Seeking depends on segments from different ffmpeg runs lining up exactly.
`internal/mediastream/ffmpeg.go` documents how that is achieved, and
`TestSeekAlignment` checks it for MKV, MP4 and MPEG-TS sources, copied and
transcoded, every quality, and on NVENC where there's an NVIDIA GPU.
Copied streams match to the frame. Transcoded audio under a copied video
can differ by one AAC frame where segments from two runs meet.

Throughput between laptop and projector is whatever the Wi-Fi gives.
Both on one busy 2.4 GHz access point (2026-10-07), the projector
fetched 3.5 Mb/s from the laptop and 1.8 Mb/s from the internet; the
lower qualities exist for that.

Each segment starts with its own MPEG-TS continuity counters. ffmpeg's
HLS demuxer, which joins segments into one stream, reports "Packet
corrupt" at every boundary; players that parse segments separately,
ExoPlayer included, aren't affected.

## Subtitles

Text subtitles are prepared once per file and loaded by the player
beside the stream:

- Embedded SRT, mov_text and the like become WebVTT; ASS/SSA keeps its
  styling and positioning.
- Sidecar files next to the video (`Movie.en.srt`, `Movie.eng.forced.ass`,
  `Movie.en.sdh.srt`, `Subs/Movie/2_English.srt`, `Subs/*.srt` for a
  single-video folder). Language, forced and SDH come from the name.
  Files that aren't UTF-8 are read in the language's usual legacy code page
  (e.g. CP1251 for Russian, CP1256 for Arabic).
- Forced and default tracks are marked default.

Bitmap subtitles (PGS from Blu-rays, VobSub from DVDs) can't be sent
beside an HLS stream, so they're not offered.

The first play of a file waits for its keyframe index and subtitle
extraction, which read the whole file: usually a few seconds, longer for
large files on a hard disk. Files added while the server runs are
prepared in the background, and so is the next episode whenever one
plays.

## API

The media server's own endpoints, used by the projector:

```http
GET /api/info
GET /api/home
GET /api/search?q=
GET /api/title?id=
GET /api/season?id=&page=
GET /api/resolve?id=&caps={json}     → {url, mimeType, subtitles}
GET /hls/{stream}/master.m3u8, /hls/{stream}/{v0|v1…|a0|a1…}.m3u8, /hls/{stream}/{rendition}/{n}.ts
GET /sub/{file}/{track}
GET /art/file/{path}, /art/thumb/{id}
```

The projector exposes the library as the `local` catalogue on its
existing `/api/catalog/*` routes, `GET /api/catalog/sources` says whether
it's up, and artwork is relayed through `/api/local/art/`. IDs are
`tv-<hash>` and `mv-<hash>` for titles, `<show>:<season>` for seasons and
`<show>:<season>:<episode>` for episodes. They come from the normalized
names, so watch history survives renames that keep the same name.

## Troubleshooting

- **No Library tab.** `curl http://<projector>:8080/api/catalog/sources`
  says whether the projector sees the laptop; the projector's log has a
  `localmedia: media server ... at ...` line when it finds one. From the
  laptop, `curl http://<laptop tailnet IP>:8770/api/info` must answer (a
  firewall must allow 8770 on `tailscale0`).
- **"The stream stopped working" at once.** The player APK predates the
  cleartext change; reinstall it (README, Build).
- **A track or quality is missing.** `journalctl --user -u
  projector-media` shows each stream's plan (`video copy + 720p + 480p`)
  and any ffmpeg error. The projector logs the decoders it detected at
  startup (`main: decoders for the media library`).
- **Stutters.** The player steps down to the lower qualities on its own
  once it has measured the network; the remote's quality picker can pin
  one from the start.

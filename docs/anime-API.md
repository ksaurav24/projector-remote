# Anivexa reference and port boundary

The Go anime aggregator uses
[Anivexa-API](https://github.com/walterwhite-69/Anivexa-API) as its provider
reference. The port keeps its AniList identity-first model and the
search → provider match → episode/audio resolution → direct stream flow.
Provider aliases are matched strictly; ambiguous results are rejected rather
than selecting the first search hit. Returned playback media is limited to
direct HLS or MP4, with subtitles, referer, and required headers where
available.

## Provider coverage

Thirteen of Anivexa's fourteen provider modules are wired into the Go fallback
chain:

| Provider | Go adapter | Notes |
| --- | --- | --- |
| AniNeko | scraper | exact title matching; HLS extraction |
| AniKoto | direct-site adapter | AJAX episode/server flow; direct HLS/MP4 only |
| ReAnime | API adapter | self-hosted base URL required |
| AniZone | scraper | HLS and subtitle extraction |
| AnimeGG | scraper | MP4/HLS extraction |
| AniBD | API/player-page adapter | HLS extraction |
| Senshi | MAL mapping + VidCloud | audio selection; HLS/MP4 |
| Kickassanime (`kaa`) | API adapter | exact/scored match with ambiguity rejection |
| AnimeDunya | MAL mapping + page data | HLS/MP4 and subtitle links |
| AniDB.app | AniList/MAL identity verification | embed pages that expose a direct HLS source |
| AnimeNoSub | WordPress search + episode pages | embed pages that expose a direct HLS source |
| AnimeOnsen | authenticated API | short-lived session held in memory; HLS/MP4, subtitles |
| AniWaves | direct-site AJAX adapter | direct HLS/MP4 only |

`mkissa` is not enabled. Its reference flow requires dynamic GraphQL query and
crypto discovery, an interactive CAPTCHA handoff, and several independent
player extractors. Those pieces are not implemented in this Go port, so it is
not advertised as a functioning provider. Likewise, embed-only and DASH
results are rejected instead of being sent to the client player.

## Compatibility routes

The server exposes these Anivexa-style paths alongside the projector's
`/api/catalog/*` interface:

```http
GET /map/:anilistId
GET /episodes/:anilistId
GET /episodes/:provider[/:provider...]/:anilistId
GET /watch/:provider/:anilistId/sub|dub/:provider-:episode
GET /stream/reanime/:anilistId/sub|dub/:episode
```

`/map` returns AniList and MAL identity plus title metadata. `/episodes` uses
AniList's released episode count as the canonical list; it does not claim
provider-specific availability. `/watch` resolves one named provider and
returns only direct playable stream data. Provider resolution never changes
catalogue/history IDs.

The Go implementation intentionally does not port download routes, FFmpeg,
Redis, disk caching, Vercel/edge deployment glue, the Anivexa proxy worker, or
Anivexa's interactive CAPTCHA page. Resolved stream URLs are not cached.

## Matching and safety behavior

- Search candidates from aliases are merged before exact normalized-title
  matching. A collision between distinct provider records is an ambiguity,
  not a reason to pick whichever result appeared first.
- AniDB.app uses AniList ID, or MAL ID only when the page has no AniList ID.
- MAL-backed providers reject an explicit MAL mismatch.
- Episodes must be positive and no greater than AniList's released episode
  count. A provider that uses a cumulative or season-offset numbering scheme
  must prove that mapping; the resolver does not guess offsets.
- Only valid HTTP(S) HLS and MP4 URLs reach playback. Subtitle URLs are
  independently validated, and dub requests never silently fall back to sub.
- Requests use bounded timeouts and response bodies. Provider failures are
  isolated by the fallback chain; a provider outage does not alter AniList
  identity or watch-history keys.

Provider endpoints can change independently of this service. Offline fixtures
cover provider parsing, strict matching, audio selection, and the compatibility
watch response; run `go test ./...` before deployment.

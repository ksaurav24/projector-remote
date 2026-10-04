# Anime module

Browse anime through **Browse → Anime**, search by title, and open a release
for its episodes. Choose **Subtitled** or **English dub**, then select an
episode to play it on the projector. Stream providers are selected by the
Go server. The PWA and Android player never choose or match providers.

Install the rebuilt player APK together with the rebuilt server. This player
version adds MP4 and external subtitle support; an older APK only handles HLS.
Build and deployment commands are in [README](../README.md#build).

## Provider configuration

The server resolves configured providers concurrently under a 40-second
deadline and uses the first playable result. Slow or broken providers do not
delay a healthy source. Other attempts are cancelled once a source wins.
Transient GET failures are retried once within that deadline; successful title
matches are cached for an hour, and episode-route lists for ten minutes.
Individual GET attempts have an eight-second timeout, leaving room for a
retry. Timeouts are reported separately from missing sources. Unavailable audio and embed-only results are
rejected. Playback recovery excludes the failed provider.

| Provider | Server flag | Default |
| --- | --- | --- |
| AniNeko | `-anime-anineko` | `https://anineko.to` |
| AniKoto (direct site) | `-anime-anikoto` | `https://anikototv.to` |
| ReAnime API | `-anime-reanime` | Empty; skipped until configured |
| AniZone | `-anime-anizone` | `https://anizone.to` |
| AnimeGG | `-anime-animegg` | `https://www.animegg.org` |
| AniBD | `-anime-anibd` | `https://epeng.animeapps.top` |
| Senshi | `-anime-senshi` | `https://senshi.to` |
| Kickassanime (`kaa`) | `-anime-kaa` | `https://kaa.lt` |
| AnimeDunya | `-anime-animedunya` | `https://anime-dunya.com` |
| AniDB.app | `-anime-anidbapp` | `https://anidb.app` |
| AnimeNoSub | `-anime-animenosub` | `https://animenosub.to` |
| AnimeOnsen | `-anime-animeonsen` | `https://www.animeonsen.xyz` |
| AniWaves | `-anime-aniwaves` | `https://aniwaves.ru` |

AniList supplies catalogue metadata. Its GraphQL endpoint is configurable
with `-anime-metadata` (default `https://graphql.anilist.co`). When its
whole-word search has no matches, Kitsu provides partial-title results through
`-anime-search` (default `https://kitsu.io/api/edge/anime`). Only explicitly
mapped AniList IDs are returned, so playback and bookmarks keep their identity.

ReAnime has no public API deployment documented in the supplied reference.
Run [ReAnime-API](https://github.com/n1yshi/ReAnime-API) on a reachable server
and pass its base URL with `-anime-reanime http://YOUR_API_HOST:8787`.
AniKoto is ported as a direct-site adapter, not the separately hosted
AniKotoAPI. No Python or Node runtime is required on the projector: the HTML
extractors run in Go, and API adapters call the configured HTTP services.

On 2026-10-03, development-machine smoke checks fetched a real AniZone HLS
manifest with five subtitle tracks and the beginning of an AnimeGG MP4 for
Cowboy Bebop episode 1. AniNeko returned a database error, and AniKoto's
published API URL returned 404. ReAnime was not configured. These checks
confirm extraction and media access from this machine; playback on the
projector still needs a device check after installing the APK and server.

## Catalogue contract

Anime uses the existing `/api/catalog/*` endpoints and WebSocket `play`
command, with `ott: "anime"`. Responses use the same normalized fields as
the existing movie/TV catalogue.

| Identifier | Example | Meaning |
| --- | --- | --- |
| Title | `1` | AniList release ID |
| Season | `1:sub` or `1:dub` | Audio edition of that release |
| Episode | `1:sub:3` | Release, audio edition, episode number |

Provider slugs and stream URLs never become history keys. Sub and dub have
separate episode progress so resuming retains the selected edition.
Direct TV/ONA prequels and sequels appear as separate season releases, with
audio choices nested under each release. Opening a later release selects that
release; next-episode playback retains audio when moving to its sequel.
Spin-offs, alternate adaptations, films and specials remain separate. Anime
movies use the same audio controls.

Episode names, summaries and matching artwork come from AniZip, configurable
with `-anime-episodes` (default `https://api.ani.zip/mappings`). Release-local
numbers remain the playback identifiers. Missing metadata falls back to numbered
episodes, so an episode-metadata outage does not prevent playback.

Examples, using the projector's HTTP address:

```http
GET /api/catalog/home?ott=anime
GET /api/catalog/search?ott=anime&q=Cowboy%20Bebop
GET /api/catalog/title?ott=anime&id=1
GET /api/catalog/season?ott=anime&id=1%3Adub&page=1
```

Start an episode over the existing `/ws` connection:

```json
{"type":"play","ott":"anime","titleId":"1","seasonId":"1:dub","episodeId":"1:dub:3"}
```

Episode lists use AniList's episode count, limited to released episodes when
its next-airing metadata is available. Pages contain up to 50 episodes.
Episode names are currently `Episode N` with the release's cover image.
Counts describe the release, not guaranteed availability at every provider;
a dub may be missing or behind the subtitled release. When AniList has no
usable episode count, no episodes are offered. Upcoming releases cannot play.

## Adapter boundaries and recovery

- `internal/catalog`: shared response types, media contract, and catalogue
  router. Existing `nf`, `pv`, and `hs` requests continue to use NetMirror.
- `internal/anime`: AniList metadata client, stream `Provider` interface,
  ordered fallback service, and thirteen Anivexa provider adapters. `mkissa`
  is excluded because its CAPTCHA, dynamic crypto discovery, and extractor
  stack are not ported. The exact route compatibility surface is documented
  in [anime API references](anime-API.md).
- `internal/playback`: session, resume, autoplay, and recovery. On an anime
  media error it tries providers after the failed one and reloads the same
  canonical episode at the reported position. Exhaustion ends with an error;
  starting playback again begins a fresh provider chain.
- Android player: accepts URL, MIME type, request headers, referer, and
  subtitle configurations. HLS quality variants and subtitle tracks use the
  existing player controls. A direct MP4 has only its selected source quality.

Scraper adapters match normalized AniList titles and aliases exactly, rejecting
ambiguous matches. MAL-backed adapters verify the external MAL ID when
available. The adapters do not guess season offsets or substitute the first
search result; some titles therefore remain unavailable even if a provider
carries them under an unrelated name or cumulative season numbering. Several
providers are sub-only, and unavailable dub never silently falls back to sub.

Autoplay stays within the chosen audio edition. The last subtitled episode
never starts episode one of the dub. NetMirror keeps its existing token-refresh
recovery behavior and OTP login.

AniList metadata is cached for five minutes in a bounded in-memory cache.
Resolved stream URLs are not cached. Metadata failures are separate from stream
provider failures: provider fallback cannot replace an unavailable AniList
catalogue. Provider failures are logged on the server; clients receive a concise
retry message.

## Verification

```bash
go test -race ./...
npm --prefix web run build
npm --prefix web run lint
(cd player && ./gradlew testDebugUnitTest assembleDebug)
```

Optional network checks (provider uptime can change):

```bash
ANIME_LIVE_TEST=1 go test ./internal/anime -run TestLive -v
```

Offline fixtures cover the provider flows currently tested, stream type
validation, audio selection, subtitle normalization, ambiguous matching,
timeout/cancellation, ordered fallback, pagination, compatibility watch
responses, and GraphQL errors. Playback regression tests
cover provider recovery at the saved position, resume in the same audio edition,
and stopping autoplay at the release finale.

See [anime API references](anime-API.md) for the upstream implementations and
provider priority used here.

# Chess reference

## Configuration

| Environment | CLI flag | Behavior |
| --- | --- | --- |
| `SERVICE_PORT` | `--port` | Private listener, default `8080`. |
| `ORIGIN` | `--origin` | Exact signed-in public origin, required. |
| `SHARE_ORIGIN` | `--share-origin` | Required exact public origin with a different hostname. |
| `LANDING_ORIGIN` | `--landing-origin` | Cloud-home link, normally signed-in Home. |
| `ACCESS_GATEWAY_URL` | `--access-gateway-url` | Required private Gateway key-discovery origin. |
| `POSTGRES_URI` | `--postgres-uri` | Direct database URI; takes precedence over discovery. |
| `POSTGRES_URI_FROM` | `--postgres-uri-from` | Scoped broker discovery alternative. |
| `POSTGRES_PASSWORD_FILE` | `--postgres-password-file` | Database password file. |
| `BROKER_TOKEN_FILE` | `--broker-token-file` | Scoped discovery token. |
| `BROKER_CA_FILE` | `--broker-ca-file` | Verified broker TLS CA. |
| `LOG_LEVEL` | `--log-level` | DEBUG, INFO, WARN or ERROR; default INFO. |

Database migrations use Goose's PostgreSQL session lock. Startup then imports
the embedded puzzle bundle transactionally under an advisory lock, refusing any
changed content in an existing edition/slot. Repeated startup preserves progress
and historical attempt rows. Match creation, moves, resignation, link changes
and deletion use durable request receipts. An identical authorized retry returns current state;
changed payloads under the same key conflict. Puzzle moves carry full client
state: an ordered decision history plus every resulting board. The server
replays that history against immutable proof nodes, checks all FENs, paths and
completion flags, and retains every mandatory defense. Only an all-lines mate
writes the signed-in user’s monotonic completion bit; no puzzle attempt,
session, partial progress or receipt is stored. Identical retries produce the
same result. Reloading starts a fresh board without clearing completed progress.

## HTTP and capabilities

The signed-in host routes `/matches`, `/matches/archive`, `/matches/new`,
`/create` and `/puzzles/*`. Every request must carry the
Gateway's valid request-bound v4.public assertion, UUID and matching name.
Authentication belongs to Gateway; Chess never owns cloud login cookies.

New game is the first navigation item, with normal, Fischer random and custom
setup choices. Sessions and Archive show the latest stored position on each
match card. Their navigation labels replace redundant page headings, as on the
Puzzles overview. Puzzle collections use plain sections with horizontal dividers;
number previews work on hover or keyboard focus. Match moves and access-link
management live beside the board, with Game menu beside the match title.

Public match titles show the host and guest names. The host name comes from
verified Gateway identity at creation; older matches acquire it on their owner's
next match visit. Until then, their public title uses “Host” as the fallback.

The public host admits only `/g`, `/guest-sessions`, `/play/*`, `/watch/*` and
`/static/*`; owner routes, puzzles, callbacks and probes return 404 there. Public
routes ignore identity headers. Probes use exact `/healthz` and `/readyz` on a
private IP/localhost Host. Readiness checks only PostgreSQL, with two-second
deadlines, ten-second cycles and thirty-second result expiry.

Playing links use `/g#<256-bit-token>`. An early external script scrubs the
fragment immediately. Only pressing Continue exchanges the token by POST and
activates a pending match. PostgreSQL stores only its digest. The raw token is
shown once after creation/replacement; a lost response requires explicit link
replacement. An idempotent retry never silently rotates it. Host-only HttpOnly
guest cookies are distinct per match and expire absolutely after 30 days.
Opening a still-current playing link can create a fresh session. Every request
rechecks the link generation and revocation. Match data never expires with it.

Viewing links use independent random `/watch/<token>` capabilities. They are
retrievable by authorized players, rotate separately and never permit moves.
Revocation affects page reads, polling, presence and exports. A UUID alone grants
no public read access. Both grants have no time-based expiry. Never log raw
playing tokens, viewing URLs, cookies, request bodies or backend errors.

All POSTs require the exact configured Origin, bounded request bodies and current
authorization. Native success uses redirects where appropriate; move HTMX
responses update only the state fragment. Stale writes return 409, invalid or
non-prescribed moves 422, unavailable storage 503 and unavailable capabilities
404. The bridge preserves the board on errors and stops after access expires.

## Rules, editor and exports

Runtime moves are strict UCI. Castling is stored as king-to-own-rook; ordinary
chess also accepts king-destination notation. Chess960 uses canonical gestures
and explicit accessible castle buttons, including stationary-piece castles.
The editor retains six FEN fields, spare pieces, removal, Default/Clear and PGN
validation. PGN errors preserve submitted fields and create no partial match.
Standard-start PGN imports recover opening recognition from durable canonical
moves. Nonterminal reported results are archived as `imported_result`.

Matches use a persistent board plus replaceable state. Polls are approximately
one second while waiting/spectating and five seconds on the player's turn;
unchanged responses are 204. Hidden tabs pause; failures back off to thirty
seconds. Revisions prevent late responses from rewinding a position. Menus,
promotion dialogs, orientation, annotations and sibling puzzle boards survive
routine state changes. HTMX panel navigation retains the shell; Back/Forward
reloads fresh authorized state rather than a stored history snapshot.

FEN/PGN/GIF endpoints live beneath their authorized match. GIFs use
local raster artwork, include the initial and every played position, and carry
the artwork attribution in a GIF comment. Database snapshots are released before
rendering. Limits are explicit and never silently truncate a game:

| Resource | Bound |
| --- | --- |
| POST form | 300 KiB |
| Guest display name | 80 Unicode characters |
| FEN | Strict six fields with bounded counters |
| PGN | 256 KiB, 2,048 mainline plies, 64 tags, variation depth 16 |
| Persisted game history | 2,048 plies; resign/export remain available at the bound |
| Sessions/Archive | 25 per page, maximum 50 in store API; stable timestamp/UUID cursors |
| Puzzle collection | Slots 0–255, never reassigned within an edition |
| Puzzle proof | At most one two/three-reply fork globally, at most three complete lines |
| Default preparation | Mate depth 5, 500,000 search nodes, 5 seconds, 2 MiB per proof |
| GIF | 512 played plies plus initial frame, 256×256 pixels, 32 MiB output, two simultaneous renders |
| Viewer activity | Heartbeat 15 seconds, count lease 45 seconds; no effect on retention |

The script policy permits only local external scripts. chessboard.js requires
local jQuery and dynamic style attributes; CSP allows those style attributes
while retaining strict script restrictions. No runtime CDN, engine or solver is
used. Feedback tones respect browser audio permission and reduced motion.

## Release

1. Review/commit the independent Chess repository and its supporting infra work.
   Publish the real `razzie-cloud/chess` repository/commits and GHCR image under
   an explicit tag. No remote or image is published by local implementation.
2. The local parent submodule uses `git@github.com:razzie-cloud/chess.git`.
   Publish its referenced component commit before publishing the parent pin.
   Never use a local file URL or track application files as parent source.
3. Select `CHESS_IMAGE_TAG` in the ignored operator environment after verifying
   image publication. Prepare DNS/TLS for both Chess hostnames and back up the
   complete new PostgreSQL database plus old RazChess Valkey state.
4. At separately authorized cutover, explicitly stop/remove the legacy RazChess
   service before routing `chess.<DOMAIN>` to the core service. Stack deployment
   does not prune it automatically. The legacy extra YAML is removed from source
   discovery, but its broker grant is retained for the rollback window.
5. Verify SSO, guest/view routes and readiness on the actual environment. The core
   service targets two replicas sharing PostgreSQL without sticky routing.

Rollback routes the old hostname back to the preserved legacy service and keeps
new PostgreSQL data intact. New matches/progress do not sync back. No legacy room
IDs or Redis games are imported or silently assigned to cloud users; an offline
ownership importer is separate work if preservation is required. Schema rollback
needs its own tested migration procedure.

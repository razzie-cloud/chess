# Chess design and implementation plan

Status: implemented locally in the independent `chess/` submodule, with platform
integration prepared. Remote/image publication and production cutover remain
separate operations. See the
[phased implementation evidence](2026-11-24-chess-implementation.md).
Prepared on 24 November 2026.

Build `github.com/razzie-cloud/chess` as an independent Go service and parent
submodule. Preserve RazChess's chessboard.js board, known-opening display, game
creation/editor tools, and exports while replacing its WebSocket sessions with
POST moves, HTMX updates, and authoritative PostgreSQL state. Add authenticated
match creation with a guest opponent, permanent match retention with owner
lookup/deletion and an archive, and a separate collection-based puzzle experience
with precomputed single-fork solutions and personal completion bitsets.

This document is the consolidated implementation contract for guest matches and
prescribed-move puzzles.

## Product contract

| Area | Selected behavior |
| --- | --- |
| Project | Repository/service/submodule `chess`; module `github.com/razzie-cloud/chess`; image `ghcr.io/razzie-cloud/chess`. |
| Match creator | Signs in through Razzie Cloud, enters the opponent's display name, and selects the creator's own White or Black color. |
| Guest opponent | Needs no cloud account. A private playing link authorizes the opposite color. The name is a display label, not an account lookup. |
| Ownership | Creator's verified Gateway UUID controls match management and one side. The guest capability controls only the other side. |
| Moves | Ordinary POST requests containing UCI and concurrency metadata; the server owns legality, turn, color, and outcome. |
| Refresh | HTMX updates a game panel while its chessboard.js instance remains mounted. No WebSocket or SSE connection. |
| Rules | `github.com/malbrecht/chess` for board rules and notation; explicit application logic for game outcomes and puzzle solutions. |
| Persistence | PostgreSQL through Goose and go-rel, following Access Gateway. No Chess Redis/Valkey dependency. |
| Match retention | Matches remain until the creator explicitly deletes them. Completed matches appear in an indexed archive, separate from pending/active session lookups. |
| Viewing | A separate rotatable read-only capability authorizes viewing; the match ID alone grants no public access. |
| Puzzles | A dedicated signed-in area with collections containing no more than 256 puzzles each. |
| Puzzle moves | Follow the prescribed move from offline proof, or play any legal immediate checkmate. |
| Puzzle defenses | Enumerate every legal enemy move. The entire solution may contain at most one fork, with two or three replies shown on parallel boards; all other enemy decisions must have exactly one forced reply. |
| Puzzle completion | The user wins every required branch; the server atomically sets that puzzle's bit in the user's collection progress. |
| Openings | Preserve local ECO-based opening identification and display, including the most specific recognized opening after leaving the book. |

The initial puzzle objective is forced checkmate within a declared number of
player moves, matching the existing mate-puzzle material. Unbounded mate searches
and material-advantage objectives are outside this version. FEN-only puzzle input
uses a bounded preparation search to calculate the shortest forced mate;
the tool must not guess a proof depth from the position.

## Original functionality and intentional behavior changes

Keep all eleven menu entries from the
[original game page](https://github.com/razzie/razchess/blob/master/assets/index.html).
Their authorization and destinations adapt to matches and personal puzzles.

| Menu entry | New behavior |
| --- | --- |
| New game | Open signed-in standard-match setup. |
| New Fischer random game | Open setup for a valid Chess960 position. |
| Play a puzzle | Open the puzzle collections page. |
| Create custom game | Open the signed-in editor, initialized from the current position when available. |
| Flip board | Change only this browser's orientation. |
| Copy session link | Copy a read-only viewing link; guest playing credentials have a separate creator-only control. |
| Copy FEN | Copy the complete server-authoritative FEN for the selected board. |
| Copy PGN | Copy the match or selected puzzle branch, with setup tags and result. |
| Toggle viewers | Toggle a recent-viewer count without changing shared state. |
| Download as GIF | Download the match or selected puzzle branch. |
| Resign | A match player resigns their own color; in puzzles this ends only the current attempt without changing solved bits. |

On guest pages, account-required menu destinations remain ordinary links to the
authenticated application and sign-in flow. They do not gain guest management
endpoints. Resign is disabled for spectators. Each multi-board puzzle action has
a clearly selected branch so exports and clipboard actions are unambiguous.

The editor retains spare pieces, drag-to-remove, default/clear controls, active
color, castling rights, en-passant state, FEN entry, and PGN import from the
[original editor](https://github.com/razzie/razchess/blob/master/assets/create.html).
Preserve halfmove/fullmove counters and support explicit rook-file castling
rights for Chess960. Preserve highlights, local square annotations, and sound
feedback, subject to browser audio permission.

The old server could play forced moves on behalf of either color. Two-player
matches now require each player to submit every move for their own side. Puzzle
defenses are applied automatically from the compiled tree, including all
alternatives rather than selecting a single defense. Arbitrary room IDs no
longer create games on access, and resource creation uses POST.

## Service boundary and code organization

Use the actual sibling-module Go toolchain at implementation time. The parent
has no Go module or workspace. Keep source and service design/implementation
records in the Chess repository; keep platform coordination changes in the parent.

| Location in the future Chess repository | Responsibility |
| --- | --- |
| `cmd/chess` | Startup, config, graceful shutdown, container healthcheck command. |
| `cmd/puzzleprep` | Offline puzzle onboarding, proof search, collection validation, deterministic compiled seed output. |
| `internal/config` | Environment/flag parsing and public-origin validation. |
| `internal/app` | Routes, authorization, origin checks, request/response handling. |
| `internal/game` | UCI normalization, legal moves, outcomes, Chess960, FEN/PGN handling. |
| `internal/opening` | Immutable ECO data and canonical move-prefix lookup. |
| `internal/puzzle` | Compiled solution format, validation, branch transitions, preparation logic shared with the tool. |
| `internal/store` | go-rel models and transaction-scoped operations. |
| `internal/db` | Embedded append-only Goose migrations. |
| `internal/web` | Escaped Go templates, local libraries/assets, board bridge and styles. |
| `internal/brokerdiscovery` | Synchronized first-party scoped credential discovery and tests. |
| `internal/gatewayidentity` | Synchronized signed Gateway identity verification and tests. |
| `internal/health`, `internal/logging` | Existing platform health/logging conventions, adapted without unrelated dependencies. |

Long-lived process state is limited to assets, immutable opening data, database
pools, and bounded public-key/readiness caches. Matches, guest grants, viewer
leases, puzzle definitions, attempts, and progress are in PostgreSQL. No game
registry, per-match goroutine, local-only lock, sticky session, or background
solver is required. Immutable decoded puzzle caches, if introduced, must be
keyed by content identity and never own attempt state.

## Creator and guest match lifecycle

### Match setup

The signed-in creator selects standard, Chess960, or custom position/PGN; enters
the opponent's display name; and selects the creator's color. Validate and escape
the bounded display name, without requiring it to resemble a Gateway username.
The opposite color belongs to the guest seat.

Create the match and its guest link in one transaction, initially waiting for
the guest. Show the playing link with a copy button and explain that anyone
holding it can play the guest side. Do not email or message it automatically.
The first successful guest-link exchange for a pending match activates it.
Exchanging a link for a completed match permits reading without reactivating it.
White moves first, which may be either the creator or guest. Imported games continue from their
stored side to move. Already completed imports go directly into the archive and
remain viewable/exportable. They do not become active matches.

### Session lookup, archive and deletion

The signed-in Sessions page lists only the creator's pending and active matches,
with bounded search by match ID/opponent label, status filters, and cursor
pagination. The Archive page provides the same owner-scoped lookup for completed
matches. Completion sets `completed_at` and moves the match into the archive UI
in the outcome transaction. Archive is a query over the same durable match and
move rows; no copying between tables, separate archive worker or loss of existing
links is required. Partial indexes keep completed matches out of active lookups.

Pending, active and archived matches never expire or get deleted automatically.
The creator can explicitly delete a match from either list or its detail page,
after confirmation explaining that history, exports and both public access
links will be removed. Deletion locks the match, rechecks ownership and expected
revision, and atomically removes its moves, grants, guest sessions, presence and
match mutation receipts. A competing move either commits first or sees deletion;
it cannot recreate the match. Repeating a successful deletion succeeds without
side effects; other users receive no confirmation of a match's existence.
Keep the minimal creator/request creation receipt as a deletion tombstone so a
delayed creation retry cannot recreate a deliberately deleted match. It contains
no move history or playing credential. Guest and viewer pages stop polling and
show that the match is no longer available. Cloud admins have no cross-user
session lookup or deletion privilege.

### Guest capabilities

Use at least 256 bits of cryptographic randomness for a playing token. Store its
digest, match/seat binding, generation, and revocation state; never log
the raw token. Its display name does not establish a real-world identity.

The playing URL is `/g#<token>` on the public host. Its GET renders a minimal
bootstrap page without receiving the fragment. An external local script reads
the fragment into memory and immediately removes it with `history.replaceState`.
An explicit Continue as opponent button performs a same-origin POST to exchange
the token for an opaque, database-backed guest session, then uses
`location.replace` to open the clean match URL. No cloud registration or manual
account acceptance is required. A preview GET or script execution without that
button press does not activate the match. Check POST origin and exact host,
bound bodies, and serve all guest resources locally. Use
`Referrer-Policy: same-origin` on form pages to preserve native POST Origin
headers while suppressing cross-origin referrers. The playing token never enters HTTP URLs,
polling URLs or rendered form attributes; it is posted in a bounded request body
and removed from memory after exchange. Reloading an unexchanged, scrubbed page
asks the guest to reopen the original playing link.

The playing link has no time-based expiry and is reusable until replacement,
revocation or match deletion, allowing recovery on another browser. Guest
sessions have a 30-day absolute lifetime; reopening the still-valid playing link
creates a fresh session without changing the match. Sessions are host-only,
HttpOnly, Secure in production, and SameSite protected. Keep sessions for
different matches distinct so opening a second match does not remove access to
the first. Every read/mutation rechecks the active link generation, match, seat,
revocation and session expiry in PostgreSQL. Session expiry never deletes a match.

The raw playing token is shown when created; it cannot be recovered from its
digest. A creator who loses it uses Replace playing link, which atomically
revokes the previous link and its sessions before returning a new link. Existing
guest browsers receive an access-expired message and stop polling. Revocation
and a competing move lock the match in the same order, making their ordering
explicit rather than relying on a credential cache.

### Viewing and permissions

Provide a separate read-only viewing URL `/watch/<view-token>` with at least
256 random bits, unrelated to the match UUID or playing token. Store the current
opaque view token in `match_view_links`, with its match binding, generation and
revocation state. Unlike a playing token, this read-only token is retrievable by
the authorized creator/guest so the menu can copy the current viewing URL after
reload. Viewers copy their current viewing URL. Never log capability URLs or
include view tokens in session search results. A match UUID alone grants no
public read or export access. No public match index or user listing is exposed.

The creator can replace/revoke the view link independently of the playing link.
View links have no time-based expiry. Every viewer page, poll, heartbeat and
export rechecks the current token/generation and revocation against PostgreSQL.
Replacement invalidates the old URL without changing the match UUID, moves or
guest sessions. Revocation disables public viewing and the guest's copy-view-link
control until the creator creates a new view link. A valid guest can still read
through `/play/{id}` and can obtain a replacement viewing URL while viewing is
enabled. Replacing only a playing link does not revoke an existing viewing link;
the creator's controls explain this distinction.

| Action | Creator | Guest seat | Viewer |
| --- | --- | --- | --- |
| View match and export | Yes | Yes | Yes, with viewing link |
| Move creator's color | Only on its turn | No | No |
| Move guest's color | No | Only on its turn | No |
| Resign | Own color | Own color | No |
| Replace/revoke playing link | Yes | No | No |
| Replace/revoke viewing link; delete match | Yes | No | No |
| Start matches and personal puzzles | Signed-in flow | Requires cloud sign-in | Requires cloud sign-in |

Creator queries require the verified UUID and ownership even for cloud admins.
The guest surface ignores supplied identity headers and accepts only its scoped
capability/session. Neither browser may supply a trusted acting color.

## Host and HTTP contract

Use two explicitly configured public origins on one private service listener:

| Surface | Local | Production |
| --- | --- | --- |
| Signed-in app | `http://chess.localhost:<LOCAL_PORT>` | `https://chess.<DOMAIN>` |
| Guest play and viewing | `http://shared-chess.localhost:<LOCAL_PORT>` | `https://shared-chess.<DOMAIN>` |

The signed-in host follows Files/Notes: strip incoming identity headers, use
ordinary Gateway `/forward_auth` for every path including `/_auth/callback`,
copy UUID/name/token, and verify the PASETO in Chess. Validate issuer, exact
audience, request method/URI, lifetime, and matching UUID/name; never fall back to
unsigned headers or expired keys. Gateway owns all cloud sign-in sessions.

The public host admits only explicit guest/view/static route families. Strip all
identity headers, skip Gateway auth only there, and return 404 for other paths.
The app also enforces the host/route boundary; routing errors must not expose
owner or puzzle handlers. It must not derive trusted public origins from
unvalidated Host or forwarded headers. Private probes have a separate explicit
listener/host policy following sibling services.

Representative route contract, finalized with templates before implementation:

| Method and route | Surface and behavior |
| --- | --- |
| `GET /`, `GET /matches`, `GET /matches/archive`, `GET /matches/new` | Signed-in Sessions lookup, completed-match archive and setup. |
| `POST /matches` | Create a standard, Chess960, or custom match. |
| `GET /matches/{id}`, `GET /matches/{id}/state` | Owner page and HTMX state fragment. |
| `POST /matches/{id}/moves`, `POST /matches/{id}/resign` | Creator actions. |
| `POST /matches/{id}/guest-link` | Replace/revoke the opponent's playing link. |
| `POST /matches/{id}/view-link` | Create/replace/revoke the independent viewing link. |
| `POST /matches/{id}/delete` | Confirmed creator-only deletion from Sessions or Archive. |
| `GET /create`, `POST /create/validate` | Signed-in editor and bounded import validation. |
| `GET /g`, `POST /guest-sessions` | Public fragment-token bootstrap and explicit token exchange. |
| `GET /play/{id}`, `GET /play/{id}/state` | Public host, guest-session-authorized game. |
| `POST /play/{id}/moves`, `POST /play/{id}/resign` | Guest-seat actions. |
| `GET /watch/{view-token}`, `GET /watch/{view-token}/state` | Independent read-only view capability. |
| `POST /matches/{id}/presence`, `POST /play/{id}/presence`, `POST /watch/{view-token}/presence` | Authorized presence heartbeats. |
| `GET /puzzles`, `GET /puzzles/{collection}` | Signed-in collections and personal progress. |
| `GET /puzzles/{collection}/{slot}` | Puzzle description and start/resume page. |
| `POST /puzzles/{collection}/{slot}/attempts` | Start/restart an owned attempt. |
| `GET /puzzle-attempts/{id}`, `GET /puzzle-attempts/{id}/state` | Owned attempt page and fresh state. |
| `POST /puzzle-attempts/{id}/branches/{branch}/moves` | Submit the prescribed UCI for one live branch. |
| `POST /puzzle-attempts/{id}/end` | End an attempt without changing completion. |
| Explicit FEN/PGN/GIF GET endpoints under each authorized resource | Downloads use the same read authorization as the displayed board. |
| `GET/HEAD /healthz`, `GET/HEAD /readyz` | Private probes; never broker provisioning. |

Authenticated pages, token bootstrap/exchange responses, game state, personal
progress and capability-scoped exports are `Cache-Control: no-store`. Static
assets may be cached independently. Where one URL negotiates a full page and an
HTMX fragment, return the appropriate `Vary: HX-Request` header as well.

GETs do not create games, mark puzzles solved, or heartbeat viewers. Native form
success uses redirects; HTMX success returns matching fragments or an explicit
navigation response. Stale state uses 409, invalid/disallowed moves 422, denied
access 403/404 as appropriate, and unavailable dependencies 503. Handle these
responses deliberately in the external HTMX bridge. Identity/session failures
must show a usable sign-in/access-expired state, not swap a login page into a
chessboard.

## PostgreSQL model and transactions

Use pgx's `database/sql` driver, `github.com/go-rel/postgres`, and
`github.com/go-rel/rel`. Goose owns schema migrations, using the PostgreSQL
session locker demonstrated in
[Gateway's migration setup](../../../access-gateway/internal/db/goose.go).
Use go-rel for ordinary model operations and parameterized SQL for row locks,
bit functions, and atomic updates on the same transaction/connection. Do not
mix an independent connection acquisition into a held transaction.

| Table | Authoritative content |
| --- | --- |
| `matches` | UUID, creator UUID/color, guest display name, mode, initial/current FEN, revision, pending/active/completed state, result/reason, PGN tags, created/updated/completed timestamps. No retention deadline. |
| `match_moves` | Match, sequential ply, canonical UCI, SAN, resulting FEN, actor kind, timestamps. Unique match/ply. |
| `match_guest_links` | Match/guest seat, token digest, generation and revocation. No time-based expiry. |
| `match_view_links` | Match, unique random view token, generation and revocation. Independent from the match ID and playing grant; no time-based expiry. |
| `guest_sessions` | Opaque session digest, match/link binding, guest seat, expiry. |
| `match_viewers` | Match, random per-tab viewer identifier, authorized role/scope, last heartbeat. |
| `mutation_receipts` | Scoped actor/resource/request or puzzle-transition identifier, request digest, committed resource ID/revision and outcome for replay protection. Creation receipts survive explicit match deletion as minimal tombstones. No raw playing tokens. |
| `puzzle_collections` | Permanent collection/edition identity, title, description, publication metadata. |
| `puzzles` | Collection/slot, initial FEN, solver color, calculated mate length, immutable compiled solution, content identity, fork count and complete-line count. |
| `puzzle_attempts` | UUID, owner UUID, puzzle identity, revision, active/solved/ended state, timestamps. |
| `puzzle_branches` | Attempt, parent/child relationship, compiled node/path, current position, remaining move budget, pending/expanded/solved state. |
| `user_puzzle_progress` | User UUID, collection identity, non-null zero-initialized `solved BIT(256)`, updated timestamp; unique user/collection. |

Add foreign keys and indexes for ordered moves, capability lookup, session expiry,
viewer leases and attempt ownership. Use partial owner/status/order indexes for
pending/active lookups and separate owner/completion/order indexes for archive
queries, with stable ID tie breakers for pagination. Gateway UUIDs are external
identifiers without cross-database foreign keys. Keep move history and initial
position durable; current FEN is their transactionally maintained snapshot.
Collection definitions/progress are durable. Retain mutation receipts with their
resource, except the minimal creation tombstones retained after explicit match
deletion. Expiring authentication/presence records never removes game data.

### Match mutation protocol

A move form carries `uci`, `revision`, and a random `request_id`. The server:

1. Validates host, origin, identity/capability, body limits, and field syntax.
2. Starts a transaction and locks the match row with `SELECT ... FOR UPDATE`.
3. Rechecks ownership or the current guest grant, session expiry, and seat.
4. Recognizes an already committed request from that actor before rejecting its
   old revision or a now-completed game. Authorization is still required for
   replay. A changed payload under the same request ID is rejected.
5. Requires an active match, the expected revision and the correct acting color.
6. Reconstructs the required rules state and validates the UCI through the shared
   rules package; derives SAN, next FEN, highlights, and outcome server-side.
7. Appends the move and updates match state/revision and receipt atomically.
8. Commits before returning the latest complete state fragment.

Creation uses a unique creator/request ID and canonical setup digest, committed
with the match, both initial grants and receipt. Serialize duplicate creation
keys before insertion; a changed setup under the same key is rejected. A retry
returns the committed match ID, or its deletion outcome, without creating another
match. The raw playing token is returned only by the original successful
creation/replacement response. If that response was lost, show the existing
match with "Created successfully; replace the playing link to obtain a new
link." Never pretend a digest can reconstruct the secret or silently rotate a
link on replay. An intentional replacement uses a fresh request ID and explicit
confirmation that existing guest access will be revoked. View-link responses
can return the stored current read-only token after reauthorization.

Resignation, guest activation, link changes and deletion also use durable replay
outcomes checked before stale-state rejection. Each actual activation, move,
resignation or grant change increments the match revision once; heartbeats and
replays do not. Guest exchange while already active does not reset play or
advance the revision. Resignation derives the caller's own color and requires
an active match, but is valid out of turn. Revoking a grant leaves the existing
pending/active/completed state unchanged. Use consistent ordering: match row
before its grant/session/receipt rows; creation alone serializes its request key
before a match row exists. A repeated guest exchange may issue a fresh session
cookie after a lost response, but cannot activate the same match twice.

An aborted browser POST can still commit. Preserve its request ID until a fresh
read or replay resolves the result, rather than resubmitting under a new ID.
Two different moves based on the same revision have one winner. Read/export
snapshots must not mix a new current FEN with an older move history; use a single
consistent snapshot, and release it before expensive GIF rendering.

## Chess rules and notation

The HTTP boundary accepts strict coordinate UCI and rejects null moves,
malformed promotions, trailing input, and excessively long values. The library's
permissive algebraic parser is not the input contract.

`malbrecht/chess` represents castling in canonical UCI as king-to-own-rook.
Normal chess may accept conventional king-destination input and normalize it.
Chess960 uses an unambiguous king-to-rook gesture/accessible castle control;
resolve input against legal moves and never infer an ambiguous castle from a
two-square king displacement. Store canonical UCI; apply both pieces from the
authoritative resulting FEN. Cover stationary king/rook castling cases. See the
[library move implementation](https://github.com/malbrecht/chess/blob/master/move.go).

Generate Chess960 positions with opposite-colored bishops and the king between
rooks, mirrored armies, and explicit rook-file rights. Use a uniform index among
the 960 valid starts and test all starts structurally. Do not copy the original
time-seeded placement algorithm.

Application adjudication handles checkmate, stalemate, resignation, fivefold
repetition, the 75-move rule, and established insufficient-material cases from
the prior game behavior. Checkmate takes precedence over the move-count draw.
Repetition compares actual position identity, side, rights, and relevant legal
en-passant availability; counters alone and raw placement FEN are insufficient.
Imported FEN starts a new history; prior repetitions cannot be recovered from it.
No draw-offer/claim UI is added in this version.

FEN validation checks eight complete ranks, allowed pieces, exactly one king per
color, counters, side, pawn ranks, king/check consistency, and castling/en-passant
fields. Do not rely on the library parser alone or claim proof that an arbitrary
custom position is reachable from the initial position.

PGN import accepts one bounded game's main line, explicitly parses movetext,
validates every move, and preserves relevant tags/result. Bound variation depth,
input bytes and move count; return errors without creating partial matches.
The editor need not edit variations/comments. Export SAN with correct numbering
for a Black-to-move setup and include `SetUp`, `FEN`, variant and result tags as
appropriate. Escape metadata correctly.

Require exactly one final movetext result marker (`*`, `1-0`, `0-1` or
`1/2-1/2`). If the Result tag is absent, derive it from that marker; if present,
it must agree. Reject moves following an automatically terminal position and
reject a result contradicting the terminal board/history, including `*` on an
already finished game. A decisive/drawn result on a nonterminal board is accepted
as an externally reported result with reason `imported_result`; do not invent
resignation/agreement evidence. Such an import is archived immediately. Only a
nonterminal game marked `*` can continue. Export the stored authoritative result
consistently in both the Result tag and movetext.

Import validation returns 422 with a specific escaped message, such as "Result
tag says 1-0, but movetext ends 0-1" or "Move 23 continues after checkmate."
Keep the submitted FEN/PGN and editor draft for correction, focus the error
summary, and create no partial match. Dependency failures use the ordinary
recoverable 503 message; never expose backend errors. Validate these paths with
both native forms and HTMX responses.

## Known opening identification

Known-opening display is a required parity feature, not optional enrichment.
The original update code uses an ECO book only when the game began at the
standard position, and its book retains the deepest recognized prefix. Preserve
that behavior. Sources: [original updates](https://github.com/razzie/razchess/blob/master/pkg/razchess/update.go)
and [ECO lookup](https://github.com/razzie/chess/blob/master/opening/eco.go).

Carry the original ECO records and attribution into the new repository after
checking their license/provenance. Normalize each book line by replaying it
through `malbrecht/chess` into canonical UCI. Generate and commit a deterministic
local data artifact; no network lookup, browser engine, or old rules library is
needed at runtime. Validate all bundled lines and fail preparation on malformed
records. Preserve deterministic precedence where multiple titles label one line.

Build one immutable prefix trie per process. Given a standard-start move history,
follow the matching prefix and return its most specific labeled opening. Keep
that label after a later move leaves the book; replace it when a deeper known
variation is reached. Treat this as move-order recognition, without promising
new transposition matching.

Derive the label from persisted moves for full pages, owner/guest/view fragments,
and standard-start imported PGNs. A restart or replica switch must not lose it.
Display the escaped opening title and optional ECO code near turn status, with
reserved space that avoids board layout shifts. Do not label Chess960 or custom
FEN positions as standard openings merely because the board looks familiar.
PGN can include locally derived ECO/opening tags without trusting submitted tags
as evidence for the on-page classification.

Required coverage includes recognized early lines, deeper variations, departure
from book, no match, restart/reconstruction, imported PGN, and suppression for
nonstandard starts. Verify the displayed label for both players and spectators
after HTMX updates. Replacing the original rules dependency must not silently
remove its opening data.

## Stable HTMX game panels

Each match panel contains a persistent board container, move form, native menu
popover, and promotion dialog, alongside a replaceable server-rendered state
region. The latter carries revision, FEN, legal move metadata, turn/outcome,
opening, history and viewer state. A state swap never replaces the board node,
menu, or open dialog.

Poll the state endpoint roughly once per second while waiting for an opponent
or spectating, and at a lower frequency on the player's own turn for remote
resignation/revocation changes. Attach presence/count metadata separately from
game revision. Return 204 when the requested game and viewer state are unchanged;
the poll timer remains active. A changed response updates state and calls the
existing chessboard.js instance only when its position actually changed.

Use external lifecycle hooks rather than inline evaluated trigger expressions.
Allow one poll at a time, stop/drop polls during mutations, and abort a stale GET
when a mutation starts. Before swapping any response, reject an older revision
or one for a panel that has since navigated to another resource. Presence-only
updates at the same revision do not animate pieces or replay sounds.

Keep a legal local drop provisionally placed while awaiting the POST, disable
further interaction, and reconcile to the server result. Illegal/rejected moves
restore the authoritative position. Update castling, captures and promotion from
the complete FEN, not an assumed single-piece animation. A delayed response must
not undo a later move. Remote moves animate once; reconnection across several
moves reconciles directly to the latest position. Honor reduced motion.

Promotion uses `dialog.showModal()` with four named piece buttons. Escape/cancel
sends no move and restores focus/position. Disable or invalidate stale promotion
choices after a revision change. The menu uses a native popover with normal
links/buttons and keyboard navigation. Keep both outside state-swap targets.

Pause polling in hidden documents, fetch immediately on return, and back off
after failures while retaining the board and a small connection-status message.
On navigation, destroy removed boards and release observers/listeners/timers.
Orientation, local annotations, selected history, focus, and scroll survive
routine polls. Do not move keyboard focus on background updates.

Same-app navigation uses explicit HTMX targets with real links and fresh
Back/Forward restoration. Guard unsaved custom-position/PGN drafts before panel
navigation as well as before unload. Downloads, cross-origin guest links and SSO
remain normal browser navigation.

## Puzzle collections and offline preparation

### Stable collection slots

Collections have titles/descriptions and permanent edition identities. Each
puzzle occupies an immutable slot `0..255`; use a non-null collection reference,
`slot SMALLINT NOT NULL CHECK (slot BETWEEN 0 AND 255)` and a unique
collection/slot key. Together these allow at most 256 puzzles, independent of
application validation. Existing slots are never reassigned or renumbered.

Keep curated source manifests and compiled outputs in the Chess repository.
The preparation tool can ingest the legacy newline-FEN file with an explicit
collection identity and optional search ceiling. Report accepted/rejected source entries and
their reasons; do not silently discard puzzles or auto-pack more than 256 into
one collection. Compile the original bundled material and publish only entries
that meet the proof and single-fork requirements. Keep stable source-to-slot
mapping after initial publication.

Published position, solver color, objective, prescribed moves, and defense tree
are immutable. Correcting any of them creates a new collection edition so old
completion bits and resumed attempts retain their meaning. Metadata-only title
corrections do not invalidate progress. Publication/import runs transactionally,
uses an advisory lock for replica-safe idempotent seeding, and compares content
digests instead of silently overwriting definitions. Operators publish prepared
content through the offline onboarding tool; a browser collection editor is
outside scope. Prepared collections are data seeds, separate from append-only
schema migrations. After migrations, startup imports the bundled seed artifact
under the publication lock before becoming ready. Repeat startup is a no-op for
matching definitions. A changed definition under an existing edition/slot fails
with a classified startup error; it never overwrites progress or attempts.
Omitting an old edition from a later seed bundle does not remove published data.

### Onboarding tool and database seeds

`cmd/puzzleprep` supports adding a FEN to a source manifest, compiling selected
entries, and verifying the final seed artifact without a database. Each source
entry records collection/edition, permanent slot, FEN, solver color, mate limit,
title, provenance/license and explicit draft/publish selection. Adding an entry
checks syntax and slot availability and writes a draft; it does not renumber
existing entries or publish an unproven puzzle. Batch legacy-FEN import follows
the same review process and calculates mate length within a bounded search.

Example planned workflow:

```sh
go run ./cmd/puzzleprep add -manifest puzzles/source.json -collection mates-3-v1 -slot 0 -mate 3 -fen '<six-field FEN>'
# Review the draft's metadata and select it for publication in the manifest.
go run ./cmd/puzzleprep compile -input puzzles/source.json -output internal/db/seeds/puzzles.json -report puzzles/preparation-report.json
go run ./cmd/puzzleprep verify -input internal/db/seeds/puzzles.json
```

The report identifies each source entry and its acceptance/rejection reason,
proven mate depth, fork location/reply count, complete-line count, total solver
decisions and resource usage. Show the offending position/path for a second
fork or excessive replies so an operator can understand why it cannot publish.
Reject reasons distinguish invalid input, no proven mate, a second fork,
more than three replies, and search/output budget exhaustion. Draft entries are
reported separately. Any selected entry that fails makes compilation exit
nonzero and leaves the previous output intact; there is no silently partial
seed. Operators explicitly leave rejected entries as drafts or correct them.

Successful output is deterministic, versioned, digest-checked JSON embedded in
the Chess service as the database seed artifact. CI recompiles selected entries,
checks for drift, and verifies the entire artifact. The tool never contacts
production, creates credentials or changes a live database. Seed loading is
transactional and replica-safe as defined above. There is no runtime proof search.

### Preparation algorithm

Require a valid nonterminal starting position with the solver to move; a puzzle
cannot begin already completed. Use bounded, deterministic AND/OR search over
`malbrecht/chess` legal moves.
An attacker node needs one proven winning continuation. A defender node needs
every legal reply to lead to a win. The declared budget counts the solver's
moves and decreases only when that player moves. Check terminal outcomes before
declaring the budget exhausted. Stalemate, draws and solver checkmate are not
success.

First calculate the shortest forced mate without fork limits. Reject a puzzle
if no strategy at that exact depth satisfies the board limits; never substitute
a longer solution. Store that calculated depth, not the search ceiling.
Search the smallest mate budget first, then canonical UCI order, to select one
prescribed player move deterministically at each reachable player decision.
Several moves may win in chess; gameplay accepts the selected move and any legal
immediate checkmate. Other moves receive "Not the solution move", never a false
statement that they are illegal or necessarily losing. An alternative immediate
mate saves the actual played move and resulting FEN and solves only that branch;
it needs no continuation in the prepared tree and does not change edition digests.

After each candidate player move, enumerate the complete legal defense set.
A fork means an enemy decision with more than one legal reply. The complete
selected solution tree may have at most one such decision, with two or three
replies. Before that decision and on every resulting line afterward, all other
nonterminal enemy decisions must have exactly one legal reply. A later split
on even one child line is a second fork and makes that candidate unsuitable.
The fork allowance is global across the whole tree; do not reset it for a new
child board. A four-reply decision is also unsuitable.

During constrained AND/OR search, a selected player node inherits its child's
fork count; a defender node has count `(reply_count > 1 ? 1 : 0)` plus the sum
of the counts of every required child. Reject a candidate when that total
exceeds one or reply count exceeds three. Try another provable candidate in
the deterministic order if available; never prune legal defenses to fit the
limit. There are therefore at most three complete solution lines, independent
of mate depth. If preparation cannot prove a suitable strategy within configured
depth/node/time/output limits, exclude the puzzle with an explicit reason.
A search-budget failure means unproven, not proven losing. A source can be
selected for publication only after a suitable complete strategy is proven.

Respect applicable draw/history state. If memoization is useful, its key must
include the remaining solver/fork budgets and relevant history, not only board
FEN.
Transposed positions can share analysis but keep distinct gameplay paths.

The output stores a format version, stable slot, canonical initial FEN, solver
color, mate limit, content digest, and a tree of prescribed UCI moves, complete
enemy replies, resulting positions, terminal successes, fork count and
complete-line count. Replay and validate the entire tree on compilation/import:
exactly one permitted player move at each live player node, 1..3 replies at each
nonterminal enemy node, complete
legal reply coverage, at most one fork in the whole tree, at most three complete
lines, valid paths, bounded size, and checkmate at all successful leaves. An
independent validation pass recomputes these counts rather than trusting stored
metadata. Keep compiler-version metadata for reproducibility without exposing
the solution to browser pages.

No solver, UCI engine subprocess, external chess API, or proof search runs on a
move request. Runtime work is a compiled-node lookup plus ordinary legal-move
validation. Malformed/unproven compiled data is rejected before publication.

## Puzzle gameplay and personal progress

The puzzle area requires a verified cloud identity. Show collection cards with
personal solved totals, a per-collection puzzle grid, and start/resume/replay
controls. Unfinished and solved are distinct states. A new attempt never clears
a prior completion bit. No guest match credential can read or mutate personal
puzzle progress.

The solver's color is fixed by the puzzle, and all boards initially face that
color. A board starts at the current solver decision. A correct move produces:

| Defense state | Interface transition |
| --- | --- |
| Enemy has no moves and is checkmated | Mark the branch solved. |
| Enemy has no moves without check | Invalid compiled puzzle; fail closed, never award completion. |
| One legal enemy reply | Apply it to the same board and enable the next solver decision. |
| Two or three legal enemy replies | Create one child board for each reply and retire the parent into a branch summary. |

Only one enemy decision in the whole puzzle may fork. After the first split,
each child board continues with forced enemy replies until mate; no child may
split again. There are at most three playable boards and three complete lines.
A parent is solved only when all its required children are solved; the puzzle
is solved only when the root is solved. Never complete a puzzle after just the
first successful line. Do not merge different branch histories solely because
their positions coincide. Unexpected runtime data requiring another split fails
closed with a clear puzzle-data error and awards no progress.

Keep unresolved boards visible in a responsive grid, stacked on mobile. Clearly
label the defense/SAN path and remaining objective on each board. Compact solved
boards without removing their proof/completion indicator. Show active branches
and completed lines rather than a misleading fixed denominator that changes
when the single fork is revealed. Preparation reports total solver decisions
as well as the three-board bound and enforces explicit mate-depth/node/output
budgets; do not truncate required lines at runtime.

Give each live board a stable branch ID and independent instance. HTMX replaces
only affected branch state and explicitly inserts/removes board containers when
the tree expands. Existing siblings keep their DOM, focus and orientation.
Puzzle defenses happen in the response to the player's move; no fake remote
player or delay is needed. Refresh state on resume/focus and reconcile concurrent
tabs; continuous opponent polling is unnecessary for a single-user attempt.

A puzzle POST identifies the owned attempt/branch, immutable puzzle content
digest, exact played path up to the current solver decision, proposed UCI and
expected attempt revision. The path contains the canonical moves already played,
including automatic enemy replies, and uniquely identifies the compiled source
node. The logical mutation key is attempt,
branch and source path, with the proposed UCI as its payload. This makes a retry
of the same position-and-sequence transition idempotent without depending on a
new browser-generated request ID. FEN alone is insufficient because positions
can repeat. A supplied starting FEN or move sequence is never authoritative:
compare the content identity and path with the published puzzle and owned
attempt. Never accept an arbitrary client history as evidence of completion.

Lock the owned attempt first, verify the content identity and look up a committed
transition receipt before rejecting an old revision or an expanded/solved branch.
An identical already-accepted transition returns the latest complete attempt
state, even after the final solving move. A different UCI for the accepted
source path is rejected with a clear conflict message and never changes state.
For a new transition, require the live branch/path, expected revision and exact
prescribed legal UCI, then update branches/revision and the unique transition
receipt in one transaction. Reject node jumps, other users' paths and
client-supplied completion flags. Incorrect moves change no state and award no
progress. Concurrent new moves in different tabs/branches may receive 409 and
must reconcile before retrying. Start/restart/end operations use scoped request
IDs and durable replay outcomes, as match management does.

When the last required branch completes, set attempt state and the progress bit
in the same transaction. Lock order is attempt, branch rows in stable order,
then user/collection progress. Update bits in SQL rather than overwriting a
Go-side snapshot. For example:

```sql
INSERT INTO user_puzzle_progress (user_id, collection_id, solved)
VALUES ($1, $2, set_bit(B'0'::bit(256), $3, 1))
ON CONFLICT (user_id, collection_id) DO UPDATE
SET solved = set_bit(user_puzzle_progress.solved, $3, 1);
```

Parameter `$3` is the validated permanent slot, in `0..255`. A collection slot
must exist before an update is possible. Unused bits stay zero. PostgreSQL
`get_bit` and `set_bit` use zero-based leftmost indexing; `bit_count` gives the
solved total. A 256-bit payload is 32 bytes, in addition to database type/row/index
overhead. References: [bit string types](https://www.postgresql.org/docs/current/datatype-bit.html)
and [bit operations](https://www.postgresql.org/docs/current/functions-bitstring.html).

Do not depend on Go integer endianness or four signed bigint columns. Use
go-rel for ordinary progress metadata and explicit bit SQL for membership/counts
and completion updates. Fresh collection reads derive accurate progress; cache
invalidation cannot be required to preserve a completion.

## Exports and resource lifecycle

FEN/PGN copy uses authoritative state and a selectable-text fallback when the
Clipboard API is unavailable. Guest/view downloads have the same read scope as
their game. A puzzle export is a selected played branch, never the unpublished
solution tree. Preserve setup/variant metadata and result.

GIF export includes the initial position and played moves, with last-move/check
highlights where practical. Render from a consistent snapshot using embedded
piece/board assets; no runtime system font or browser screenshot dependency.
Prefer standard Go image tooling; measure bounded frame memory before retaining
the original streaming encoder or introducing another dependency. Set explicit
input/frame/output/concurrency budgets, handle cancellation, and never hold a
database lock while rendering or silently omit later moves. Decode exported GIFs
in tests and check frame positions for capture, castling and promotion.

Viewers are recently active tabs, not identified people. Heartbeat approximately
every 15 seconds and expire a lease after approximately 45 seconds. The display
can be approximate during browser throttling. Presence changes do not advance
the chess revision. Authorization is required before accepting a lease, and
heartbeat writes must not let stale grants revive access.

Presence measures recent activity only. Viewer leases never control match
retention or archive membership. Background cleanup may remove expired guest
sessions and stale presence rows in bounded, replica-safe batches, but never
matches, moves, published puzzle definitions, attempts or completion bits.
Explicit creator deletion and automatic completion-to-archive behavior follow
the Sessions/Archive contract above. Database backups must include active and
archived matches; there is no match idle-timeout setting or automatic sweep.

## Frontend and operational conventions

Embed PicoCSS, HTMX, chessboard.js, its required jQuery dependency, supplied cloud
logos, piece images and licenses. Remove the original Alpine/jQuery UI/Tailwind
and WebSocket/JSON-RPC stack. No SPA or frontend build pipeline. Keep scripts
external, HTMX evaluation/indicator style injection/history snapshots disabled,
and templates escaped. Test chessboard.js's DOM style changes under the real CSP
before finalizing the smallest necessary style allowance; do not weaken script
restrictions or add CDN access to make the board work.

Signed-in headers use the shared local account-header CSS, shield, public Gateway
account link and verified administrator badge. Public guest/view shells omit
cloud account identity. Use Home as the signed-in cloud-logo destination and a
distinct Chess service link, following the [brand contract](../../../docs/brand/README.md).

Provide accessible UCI/from-to entry and castle controls alongside dragging,
touch support, dialog focus restoration, keyboard operation, visible labels and
polite status announcements. Verify both themes at 320, 390, 768 and 1440 pixels,
including long guest names, opening titles, multiple boards, and HTMX error
responses. Inspect close-up screenshots for baseline/icon/badge alignment as
well as overflow. Preserve reduced motion and browser audio restrictions.

Use private GET/HEAD liveness and cached readiness, with PostgreSQL as the
required dependency, bounded two-second checks, ten-second check intervals and
thirty-second staleness following sibling implementations. Broker provisioning
is startup discovery, never a readiness probe. JSON slog on stderr uses validated
`LOG_LEVEL`, quiet healthy requests, request IDs/static route names/classified
failures, and no raw backend errors, token URLs, session cookies or request bodies.
Ensure proxy logging also suppresses guest credentials.

Use bounded startup/discovery/migrations, HTTP header/read/write/idle limits,
request deadlines, and graceful shutdown. Build an unprivileged Distroless
runtime with exec-form binary healthchecks and embedded assets. Include only
commands/assets needed by the service; puzzle preparation runs in development/CI.

## Broker and deployment integration

Configuration has matching environment and CLI flags:

| Setting | Contract |
| --- | --- |
| `SERVICE_PORT` | Private HTTP port, conventionally 8080. |
| `ORIGIN` | Exact signed-in public origin. |
| `SHARE_ORIGIN` | Different exact guest/view public origin. |
| `LANDING_ORIGIN` | Signed-in cloud-home link. |
| `ACCESS_GATEWAY_URL` | Private key discovery origin; required for the protected app. |
| `POSTGRES_URI`, `POSTGRES_PASSWORD_FILE` | Direct database configuration. |
| `POSTGRES_URI_FROM` | Broker provisioning alternative; direct URI takes precedence. |
| `BROKER_TOKEN_FILE`, `BROKER_CA_FILE` | Scoped broker token and verified TLS trust. |
| `LOG_LEVEL` | Standard validated logging level. |

Provision `/v1/instances/postgres/chess/uri?client=chess`. Add a `chess` client
allowed only adapter `postgres`, instance `chess`, in development/production
policy. Reuse independent first-party discovery/tests: bounded retries/body size,
no redirects/auth fallback, and no secret URL/error logging. Production follows
the existing TLS/secret lifecycle. No Chess broker Valkey allocation is needed.

| Owner/files | Required change |
| --- | --- |
| Chess repository | Source, assets/licenses, migrations, prepared puzzle/ECO data, AGENTS/README/reference docs, Dockerfile, CI and image publication workflow. |
| Parent `.gitmodules` and gitlink | Register the independent repository; never track application source as parent files. |
| Root Compose/Caddy/environment | Build service, private networks, broker token mount, both origins, authenticated route and narrow public route. |
| `docker-compose.none.yml` | Explicitly reset Chess auth mounts and token settings in legacy broker mode. |
| `dev/broker-clients.yml` and local-auth fixtures | Dedicated PostgreSQL client/token provisioning. |
| `infra/docker-stack.yml`, `.env.example`, `Makefile` | Core Chess service, `CHESS_IMAGE_TAG`, both host policies, readiness, networks, broker settings/secrets. |
| `infra/broker-clients.yml` | Scoped `chess` client without altering unrelated grants. |
| `infra/cmd/infra-helper/main.go`, `render.go`, `deploy.go` and tests | Add Chess to actual client/service mapping, rendered discovery/reset handling and app-readiness acceptance; inspect all existing Files/Notes special cases. |
| `scripts/update-image-tags.sh` and fixtures | Include Chess's remote/tag key in complete resolution and atomic environment updates. |
| `infra/landing-apps.json`, landing presets if needed | Add a Chess card using a supported preset or a small first-party icon addition; version Swarm app-list config names on change. |
| Gateway origin settings | Explicit local Chess allowlist entry; verify production wildcard/exact origin. Public guest host has no authenticated app route. |
| Parent/infra/service docs | Update app map, launch/check commands, public capability scope, storage/backups, release steps and old-RazChess assumptions. |
| `infra/extra/razchess.yml` and related tests/docs | Remove the conflicting legacy service definition at replacement time; retain unrelated extra-app behavior. |

The infra helper currently has explicit app maps, readiness lists, reset cases
and extra-file-count fixtures. YAML edits alone are insufficient. Change these
in their owning infra repository and run its isolated helper tests. Do not
incidentally alter the existing ahead-of-remote infra commit.

If the new remote is unavailable, prepare an independent local repository and
leave parent registration pending. Do not invent a file submodule URL or publish
a parent pin to an unavailable component commit. Publish supporting component
commits/images before parent pins or operator image selections. Two replicas
are the target once shared-state and mixed-request acceptance passes; no distinct
node placement or sticky routing is required.

## Implementation phases

Each phase produces a reviewable slice with its own acceptance checks. Opening
parity and offline puzzle proof are prerequisites for the complete interface
and platform integration.

### Phase 0 Contracts and compatibility

Confirm the independent repository location and inspect the pinned original
source/assets/licenses. Prove the selected `malbrecht/chess` version builds with
the actual toolchain and supports required FEN/PGN/castling cases. Characterize
the original opening dataset and normalization. Exercise chessboard.js with
native dialogs/popovers and the intended CSP in a temporary harness. Finalize
host, route, schema and input/resource budgets before expanding implementation.
Prototype the constrained proof search on representative mate-in-2/3/4/5 inputs
and screen the original puzzle source for the single-fork rule. Record accepted
counts, rejected positions/reasons, maximum complete lines and solver decisions.
Require at least one proven fixture with no fork and one with a two- or three-way
fork before building the collection UI; insufficient source material requires
an explicit content decision rather than weakening the rule silently.

Exit: focused compatibility fixtures pass; asset provenance is recorded; a
chosen mate-in-N manifest and proof prototype demonstrate usable seed content
within the single-fork rule. This phase does not create/push a remote or deploy
anything without task authorization.

### Phase 1 Service and durable foundation

Create the independent Go module, embedded shell/assets, configuration,
logging/health/startup/shutdown, PostgreSQL adapter and initial Goose schema.
Add scoped broker discovery and Gateway verifier copies with synchronized tests.
Implement host separation and transaction helpers without unused service layers.

Exit: build/vet/unit tests pass; disposable PostgreSQL proves concurrent startup
migrations, transaction rollback, scoped reads and restart persistence. Direct
URI and authenticated discovery configurations both work in fixtures.

### Phase 2 Chess rules and known openings

Implement canonical UCI validation, Chess960 generation/castling, FEN/PGN
import/export, outcome/repetition logic and persisted move reconstruction.
Normalize and embed the original ECO data; implement and render deepest-prefix
opening titles/codes. Keep only the new rules library in runtime game logic.

Exit: rule/notation fixtures pass, PGN result/tag/terminal-state disagreements
return actionable errors without losing drafts, all accepted ECO lines replay,
named opening fixtures retain their title after book departure, and imported
standard games recover the same opening after restart. Chess960/custom starts
omit false labels.

### Phase 3 Match setup and guest authorization

Implement owner setup/color/name selection, pending-to-active lifecycle, playing
fragment-link bootstrap/exchange, per-match guest sessions, independent rotatable
viewing links, own-side POST moves/resignation, receipts and guest-session expiry.
Add owner-scoped Sessions/Archive lookup with partial indexes, automatic archive
membership on completion, and confirmed explicit deletion. No match expires.
Authorizing a request must use current PostgreSQL grant state under the defined
lock order. No registration UI appears in guest play.

Exit: a signed-in creator and an anonymous browser can play only their respective
colors; a viewer cannot move; duplicate/stale requests, revocation/move races,
spoofed headers and cross-origin submissions are rejected safely. Creation/link
response loss has explicit recovery, view rotation invalidates old URLs, and a
match ID alone grants no public access. Sessions and Archive are paginated and
owner-scoped; deletion/move races and creation retries after deletion are safe.

### Phase 4 Persistent board and live match interface

Implement the chessboard bridge, state fragments/polling, pending-move feedback,
revision guards, native promotion/menu, move history, opening/status display,
orientation/highlights/sounds, presence and responsive/accessibility controls.

Exit: the board DOM survives at least 30 unchanged polls, delayed responses cannot
rewind state, promotion/menu/focus survive unrelated updates, and both browsers
observe remote moves and outcomes without flicker. Owner/guest/viewer layouts
pass light/dark mobile/desktop inspection and live capability/SSO checks.

### Phase 5 Puzzle preparation and curated collections

Build the preparation command and immutable manifest/tree format. Add bounded
AND/OR mate proof, deterministic prescribed moves, full defense coverage,
single-fork filtering, output validation, stable slots and collection seeding.
Finish onboarding add/compile/verify commands and per-entry reports. Screen
bundled/external puzzle fixtures and report rejects; embed the reviewed seed
artifact and import it transactionally after migrations.

Exit: deterministic compilations reproduce the same tree; every published leaf
is verified mate; no legal defense is omitted; a second fork anywhere, four
replies and unproven/oversized trees are rejected with positions/reasons. Every
published puzzle has at most three complete lines; a 257th slot cannot publish.
Onboarding preserves rejected drafts, failed compilation preserves prior output,
and repeated/concurrent seed imports neither duplicate nor overwrite content.

### Phase 6 Branching gameplay and completion bitsets

Implement signed-in collection/grid pages, owned resumable attempts, exact-move
POST enforcement, branching board lifecycle, all-lines completion and atomic
`BIT(256)` progress updates. Add branch selection for exports and retry/resume
behavior. Keep the compiled solution out of client payloads.

Exit: forced-line and single two/three-way fork fixtures behave correctly, all
resulting lines require completion, any non-prescribed move leaves state
unchanged, another user cannot access an attempt, and parallel completions
preserve both bits.
No child can fork again. Identical path/move retries return current state even
after branch retirement or final completion; forged/repeated-position paths are
denied. Slots 0/255, repeated solves and collection editions have explicit coverage.

### Phase 7 Complete original feature parity

Finish custom editor/import controls, all menu destinations/permissions,
clipboard fallbacks, selected-branch FEN/PGN/GIF export, viewer toggle, and
draft/navigation lifecycle. Finish Sessions/Archive search and deletion controls,
link recovery/revocation messages and import error summaries. Preserve every
listed original menu item and known-opening display across all applicable screens.

Exit: a menu-by-menu acceptance checklist is complete for owner, guest, viewer
and puzzle contexts. GIFs decode to expected positions; exports cannot expose
other matches, puzzle owners or unrevealed solution branches.

### Phase 8 Platform integration and release preparation

Add the parent submodule integration, local Compose/Caddy/public host routing,
broker policies/tokens and tokenless override. Update Swarm service/environment,
infra helper maps/probes/reset handling, image-tag selection, landing card and
documentation. Add CI for checks, disposable PostgreSQL, preparation artifacts,
browser fixtures and image build. Prepare the legacy-service replacement steps.

Exit: offline Compose/core/extra-stack validation and helper fixtures pass;
local SSO/callback plus guest play works; no public route reaches management or
personal puzzles. Release artifacts and remaining operator steps are explicit.

### Phase 9 Whole-system acceptance and release handoff

Run the full verification matrix, including two independently constructed
service instances sharing PostgreSQL. Alternate requests between them, restart
one during a match/branching attempt, inject delayed responses and verify no
state, opening title or completion is lost. Finish both-theme screenshots and
record exact commands, passes, skips, preparation rejection counts and limits.

Exit: no required parity/security/progress check remains unaccounted for. Hand
off component commits, parent pins, publication prerequisites, DNS/routes,
backups, cutover and rollback instructions. Publishing/deploying remains a
separate authorized operation; a plan or successful config check does not run it.

## Verification matrix

| Area | Required evidence |
| --- | --- |
| Rules | Normal/Chess960 castles, promotions including underpromotion, en passant, pinned pieces, checks, mate/stalemate, draw precedence/repetition, malformed FEN/PGN, Black-to-move imports, Result tag/marker/terminal-state conflicts, reported nonterminal results, retained import drafts and actionable native/HTMX errors. |
| Opening parity | Recognized line/deeper variation/book departure, no-match state, full ECO replay, imported PGN, both player/view surfaces, restart and HTMX refresh. |
| Ownership | Wrong creator UUID, wrong guest seat/match, spectator POST, spoofed signed headers, expired keys/cookies, revoked grants, own-color resignation, owner-only session lookup/deletion. |
| Guest exchange | No registration, preview/script execution without Continue cannot activate, fragment/history scrubbing, refresh/new browser recovery, distinct simultaneous matches, 30-day session expiry without game deletion, revocation invalidates sessions, no playing token in HTTP URLs/logs/referrers. |
| View capabilities | Match ID alone denied; view token independent of playing token; replace/revoke invalidates old polls/heartbeats/exports without changing guest play; current viewing URL copy after reload; no raw capability logging. |
| Match lifecycle | Pending/active/completed matches survive arbitrary inactivity and presence cleanup; completed matches enter the archive atomically; indexed active/archive search with stable pagination and cross-user isolation; confirmed delete cascades and invalidates links without changing puzzle progress. |
| Transactions | Competing moves, duplicate IDs and changed payloads, ambiguous commit replay, lost creation/replacement responses, replay after grant changes, move/resignation/revocation/deletion races, deleted-creation tombstones, rollback, one-connection pool, consistent exports. |
| Puzzle compiler | Known mate/no-mate/draw positions, unique/deterministically chosen moves, all defenses, zero/one fork with at most three complete lines, any second fork rejected including on one child, four replies rejected, promotion/castling puzzles, budgets, deterministic output and malformed-tree rejection. |
| Puzzle onboarding/seeds | Add FEN with explicit slot/mate/provenance, draft selection, per-entry reports, invalid selected entry preserves previous output, deterministic embedded seeds, idempotent/concurrent import, conflicting content fails without overwriting existing progress. |
| Puzzle runtime | Non-prescribed but legal move rejected, stale branch/node jump denied, all children required, no second split, identical path/move retries after expansion/final completion, changed payload and forged history denied, two-tab conflicts, partial progress resume and cross-user isolation. |
| Bitsets | Slots 0/255, slot 256 rejected, max collection cardinality, zero unused bits, concurrent upserts, repeat solve, new edition, attempt-end and match deletion do not clear bits. |
| Browser | Persistent board nodes, unchanged/old/presence-only responses, promotion cancellation, menu/focus/scroll, errors/offline/hidden tabs, live SSO expiry and guest revocation. |
| Presentation | Both themes at 320/390/768/1440 widths; guest names/openings/controls aligned; multi-board overflow and close-up screenshot review; keyboard/touch/reduced motion. |
| Exports | Authorized snapshot, setup/result tags, GIF decode/frame count/positions, cancellation/resource bounds, selected branch only. |
| Integration | Authenticated/private and narrow public host policies, broker token/TLS and explicit none mode, helper mapping/probes, release-tag selection, replica restart without affinity. |

Inside the Chess module run `gofmt` on changed Go, `go build ./...`,
`go vet ./...`, `go test ./...`, and `go test -race ./...`. PostgreSQL integration
uses a disposable loopback `TEST_DATABASE_URL` and isolated schemas. Run browser
fixtures under production CSP as well as live local Gateway/guest acceptance;
fixtures alone do not prove SSO or concurrency. Build the Docker image and verify
its binary readiness probe when a local daemon is available.

For integration edits run `docker compose --env-file .env.example config --quiet`
and validate the explicit tokenless override. Validate Swarm with `make config`
using selected operator tags or documented placeholders in an isolated test
environment, and `make config-extra` after legacy removal. Run isolated infra
helper tests and the affected tag-selection fixtures. Validate any changed shell
with `bash -n`. None of these checks may deploy, switch Docker contexts, or use
production databases.

## Release replacement and rollback

The legacy `infra/extra/razchess.yml` currently owns `chess.<DOMAIN>` and depends
on a single replica plus broker-managed Valkey. The new core Chess service must
not contend with it for the hostname. At authorized cutover, stop/remove the old
service explicitly, remove its definition from discovered extras, and route the
hostname to the new service. Deployment does not prune an obsolete Swarm service
automatically. Prepare DNS/TLS for `shared-chess.<DOMAIN>` as well.

Back up the new PostgreSQL database, including capabilities, active/archived
matches, puzzle definitions, attempts and progress. Keep the old Valkey data and
old service configuration during the verification/rollback window. Do not remove its broker
grant until no old service/rollback path requires it. Other broker clients and
Valkey instances remain untouched.

Legacy room IDs, shared-move permissions and serialized Redis games are not
automatically migrated. If existing rooms must survive, implement an explicit
offline importer with a documented ownership/guest-link assignment step before
cutover; do not silently assign old rooms to cloud users. This is separate work.

Rollback routes the old hostname back to the preserved legacy service and keeps
the new PostgreSQL data intact. New Chess matches/progress do not synchronize
back to RazChess. Future schema downgrades require their own tested migration
procedure; restarting an old binary is not a schema rollback.

## Scope and remaining validation

This version includes every original menu item, known-opening display, signed-in
creation, independent guest/view capabilities, permanent match retention,
Sessions/Archive lookup and explicit deletion, Chess960/custom games, exports,
puzzle onboarding and database seeds, prepared single-fork mate puzzles,
collections and completion bitsets. It does not add
ratings, clocks, chat, draw offers, a server chess bot, material-advantage puzzle
analysis, browser collection editing, or legacy connector/bot WebSocket support.

Before implementation can claim compatibility, the preparation tool must screen
the original puzzle set, ECO data provenance/normalization must be checked, and
the real chessboard.js/CSP/native-control combination must pass browser checks.
The number of original puzzles that satisfy the new branching rules is unknown.
The original planning change ran no service tests. Implementation and disposable
local verification are recorded separately in the implementation evidence;
no publication, production deployment, production migration or live data import
is authorized by this plan.

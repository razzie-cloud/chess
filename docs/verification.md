# Chess implementation verification

Completed locally on 24–25 November 2026. The
[phased implementation record](plans/2026-11-24-chess-implementation.md)
maps the design's phases to the delivered source. This file records service
checks; [platform verification](platform-verification.md) records Compose,
Swarm, broker policies, helper fixtures, tag selection and Caddy validation.
[Live acceptance](live-acceptance.md) separately records real Gateway sessions,
virtual passkeys, anonymous guest play, round-robin replicas, restarts and sign-out.

## Viewer sidebar separation — 26 November 2026

The optional recently-active-tabs count now uses the shared `sidebar-section`
style, giving it the same divider and padding as the opening and move-history
sections. The single template change applies to full pages and HTMX state
fragments; hiding the count removes its separator as well.

A temporary browser acceptance fixture rendered the production page in both
themes at 320/390/768/1440 pixels. It checked the visible border and text padding,
toggle-off layout, overflow and a real move response replacing the state while
preserving the count's visibility, separation and mounted board. Four screenshots
were saved and visually inspected; see
[dark desktop](screenshots/viewers-dark-1440.png) and
[light mobile](screenshots/viewers-light-390.png). No JavaScript or CSP errors
occurred. The temporary fixture was removed from the checkout.

Build, vet, full tests and the full race suite passed with a disposable loopback
PostgreSQL container and headless Brave. Both complete suites passed 187
tests/subtests; the opt-in live Gateway test skipped. Parent/component whitespace
checks passed. Unrelated generated screenshots were restored and the temporary
database was removed. No live SSO acceptance or deployment ran.

## Chess960 refresh — 26 November 2026

The Fischer random creator opens with a random valid starting position. A
Refresh button at the top right uses the exact Gateway refresh SVG (with an
icon-only narrow-screen presentation). Its signed-in GET endpoint returns only
position metadata and a hidden Scharnagl index. HTMX swaps that fragment and
updates the existing board; the header, form, opponent name, color, flipped view,
request ID, focus and URL/history remain in place. Refresh excludes the current
position. Creation uses the selected index, validated by the existing Chess960
generator, instead of selecting another layout.

The refresh and Create buttons are disabled while a request is pending. Failed
requests keep the current position and show a retry message; removing the page
aborts pending refresh work. Older clients can still omit the index for random
creation. The optional index is omitted from receipt JSON when absent so older
creation digests and retries remain valid. No database migration was required.

Verification used the isolated Go cache, headless Brave/Chromium and a new
loopback-only disposable PostgreSQL container:

- `gofmt` on changed Go files; `go build ./...` and `go vet ./...` passed.
- The complete regular and race suites passed 187 tests/subtests each; the
  opt-in live Gateway acceptance test skipped. No race was reported.
- HTTP coverage checks valid initial/refresh positions, twelve successive
  different positions, no-store fragment responses and signed-in/public-host
  boundaries. Store checks cover exact selected starts, rejected malformed or
  out-of-range indices, repeat receipts, changed-selection rejection, legacy
  random creation and unchanged legacy receipt digests.
- Browser coverage includes both themes at 320/390/768/1440 pixels, refresh
  after HTMX navigation, form/node retention, color/flipped orientation,
  keyboard focus, temporary disabled controls, unchanged URL/history and GET
  queries containing only the current position index. An intentionally blocked
  request retained the position and a retry succeeded. Real match creation
  matched the selected layout. Existing custom/normal creator and navigation
  regressions passed. No unexpected JavaScript or CSP errors occurred.
- The four Chess960 desktop/mobile screenshots were refreshed and visually
  reviewed; see [light desktop](screenshots/setup-chess960-light-1440.png) and
  [dark mobile](screenshots/setup-chess960-dark-390.png). Shared account-header
  equality, exact refresh-icon equality and parent/component whitespace checks
  passed. Unrelated generated screenshots were restored.

The temporary PostgreSQL container was removed. No live SSO acceptance, push,
image publication or deployment ran.

## Match creators — 26 November 2026

The custom-game creator now follows the reviewed mockup: a board column beside
position source and match details, with Board editor / FEN / PGN choices and
collapsed castling, en-passant and move-counter controls. Normal and Fischer
random creators use the same frame with read-only board previews, color choices
and links to the other creators. Chess960 shows a labeled example; its actual
random position is still chosen by the store when the match is created.

Source selection is applied server-side. Inactive PGN drafts cannot override
board/FEN input, and selecting an empty PGN returns an input error rather than
falling back to FEN. Validation and creation errors retain the selected source,
FEN, PGN, opponent name and color. FEN preview preserves malformed text for
correction instead of rewriting it through numeric controls. Reset/Clear also
reset position rules; the board follows the chosen player color and can be
flipped independently. Dragging and spare-piece tools are limited to the board
source. Existing routes, ownership, CSP and signed identity checks are retained.

Commands ran inside `chess/` with `GOCACHE=/tmp/razcloud-chess-ui-cache`, a new
`postgres:18-alpine` container on a random loopback port, random per-test schemas,
and `CHROMIUM_EXECUTABLE_PATH=/usr/bin/brave`:

- `gofmt` on the changed Go files, `go build ./...`, `go vet ./...`.
- `go test ./... -count=1` and `go test -race ./... -count=1`: 184 test/subtest
  passes in each, with the opt-in live Gateway test skipped. No race reported.
- Final focused regular and race checks of `TestBrowserMatchCreators`, the
  `TestSetup*` cases and `TestCreateEmptySelectedPGNDoesNotFallBackToFEN`: all
  ten checks passed after the final FEN preview refinement.
- All three creators in light/dark at 320/390/768/1440 pixels (24 layouts):
  heading count, loaded assets, overflow against document client width, desktop
  column alignment, color/orientation, flip, source panels, keyboard selection,
  collapsed rules, reset/clear, validation/replay, inactive drafts, input errors
  and real creation of all three game types with playing links. Existing browser
  regressions cover stable HTMX shell navigation and the unsaved-draft guard.
  No unexpected JavaScript or CSP errors occurred.
- Twelve new production screenshots were saved; representative desktop/mobile
  views in both themes were visually inspected. See
  [normal desktop](screenshots/setup-standard-light-1440.png),
  [Chess960 mobile](screenshots/setup-chess960-dark-390.png), and
  [custom desktop](screenshots/setup-custom-dark-1440.png).
- Shared account-header equality and parent/component Git whitespace checks.

The temporary database was removed. Existing staged mockup work was preserved;
unrelated screenshots regenerated by the full suite were restored. No live
Gateway/provider acceptance, image publication, push or deployment ran.

## Stateless puzzle play — 26 November 2026

The current puzzle flow supersedes the attempt/session behavior recorded in the
original implementation evidence below. Collection overviews include all puzzle
numbers, done markers and lazy initial-board previews on hover or keyboard
focus. Opening a puzzle immediately mounts a playable board. Its only persistent
personal state is the completion bit; reloading begins at the initial position.

Every JSON move carries the complete ordered decision history and every board.
The server rebuilds the state from the immutable proof and compares the exact
board set before accepting another decision. Responses carry every updated board
and the next undone destination. Dropped/duplicated defenses, forged FENs,
completion flags, budgets and paths are rejected. Any legal immediate mate is
still accepted. A completed defense remains locked at its last position with
the checked king highlighted red; the personal bit is set only when all defenses
are complete. Puzzle start/end/resume, receipt and export routes were removed;
historical database tables remain unchanged and unused.

Focused checks used disposable loopback PostgreSQL, Chromium and the isolated
Go cache. `go test ./internal/puzzle ./internal/store ./internal/app -run
'Test(VerifyPuzzle|Puzzle|EveryPublishedPuzzle|BrowserPuzzle)' -count=1 -v`
passed all 21 focused checks. They covered every bundled puzzle's full-state replay, adversarial board/history
claims, alternate immediate mates, required defense completion, independent
replicas/retries, concurrent bits 0/255, separate editions and owner-scoped
progress. Browser checks cover collection/detail HTMX transitions, keyboard
and hover preview hooks, immediate play, rejected provisional moves, complete
request state, persistent sibling board/dialog nodes, locked red-king boards,
next-undone navigation and cleanup. Collection layouts run in both themes at
320/390/768/1440 pixels; locked-board captures cover 390/1440. Responsive checks
wait for chessboard.js to finish resizing. Headless keyboard acceptance brings
the page to the front before programmatic focus.

Production screenshots of collection tooltips and partial/completed defenses
were captured and visually inspected in both themes at desktop/mobile sizes.
Viewport-only tooltip captures avoid full-page screenshot resizing hiding the
fixed preview. A visual check caught scrollbar clipping on mobile; placement now
uses the document's client width. Final integrated build/vet/full/race and live
Gateway checks are recorded separately by the coordinating change. This slice
did not publish images or deploy anything.

## Final service checks

Commands ran inside `chess/`, using Go 1.27.4 and an isolated writable build
cache (`GOCACHE=/tmp/razcloud-chess-final-cache`). PostgreSQL tests used a newly
created local `postgres:18-alpine` container with an ephemeral loopback-only
published port and random per-test schemas. `TEST_DATABASE_URL` selected only
that disposable server. `CHROMIUM_EXECUTABLE_PATH=/opt/brave-bin/brave` enabled
the production-CSP browser fixtures.

```sh
gofmt -l cmd internal tests tools
go build ./...
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
```

Formatting, build and vet passed. Both final complete suites passed all 140
checks across 17 packages, including the custom-FEN castling and lock-wait
expiry regressions. No race was reported. The opt-in live test skips in these
ordinary suites and passed
separately through its disposable live harness. The five browser fixture tests
ran rather than skipping.

A regular run exposed a fixture timing issue: its second promotion drag could
arrive before chessboard.js finished the first snapback animation. The fixture
now waits for observable animation completion before dragging. Five consecutive
focused promotion/castling runs passed after that synchronization fix.
The subsequent complete regular suite passed all 140 checks as well.

The following artifact commands passed, including byte-for-byte comparisons:

```sh
go run ./cmd/puzzleprep compile -input puzzles/source.json -output /tmp/razcloud-chess-final-puzzles.json -report /tmp/razcloud-chess-final-preparation.json
cmp internal/db/seeds/puzzles.json /tmp/razcloud-chess-final-puzzles.json
go run ./cmd/puzzleprep verify -input internal/db/seeds/puzzles.json
go run ./cmd/ecoprep -input internal/opening/source/eco.tsv -output /tmp/razcloud-chess-final-eco.tsv
cmp internal/opening/eco.tsv /tmp/razcloud-chess-final-eco.tsv
```

All 3,397 original ECO records normalize without rejection. The reviewed puzzle
bundle was subsequently rebuilt with 206 calculated mate-in-two positions at
consecutive slots 0–205; 14 unsupported original positions are excluded. No proof or opening artifact drift
was found. See [puzzle preparation](../puzzles/README.md) and
[rules compatibility](rules-compatibility.md) for detailed counts and provenance.

## Required behavior and evidence

| Area | Verified behavior |
| --- | --- |
| Foundation | Strict independent origins; mandatory Gateway verification; authenticated broker discovery; four concurrent migration runs; rollback, restart and one-connection operation; cached private probes and redacted logs. |
| Rules | All 960 unique starts; ordinary, canonical and stationary-piece castles; custom-FEN Chess960 mode persistence; promotion/underpromotion; en passant/pins; checkmate/stalemate, 75-move precedence, fivefold identity and insufficient material. |
| Imports/openings | Bounded FEN/PGN; Black-to-move numbering; missing/conflicting result markers/tags; moves after terminal positions; reported nonterminal results; preserved native/HTMX error drafts; standard imported opening labels and nonstandard suppression. |
| Match authority | Wrong owners and signed admins denied; current generation/seat checked before receipt replay; own-color/turn enforcement; viewer mutations denied; native Origin/host/body checks; signed-header spoofing/expiry denied. |
| Transactions | Competing moves and creation keys; identical and changed-payload retries; move vs revocation/deletion; archive completion; permanent inactive data; explicit cascades and deleted-creation tombstones; stable pagination and literal search wildcards. |
| Guest/view lifecycle | Fragment scrubbing and explicit Continue; preview cannot activate; distinct per-match cookies; replacement/revocation and absolute expiry; independent retrievable view grants; no UUID-only access; authorized exports and replica reconstruction. |
| Puzzle preparation | Full defense coverage; deterministic prescribed strategies; true second forks and four replies rejected; all leaves independently replay to mate; promotion/castling and draw budgets; stable slots, rejected drafts, failed-output preservation, concurrent immutable seeding. |
| Puzzle runtime | Owned attempt/path/content checks; legal but non-prescribed moves rejected; no client completion flag; all children required; retired/final retries accepted; changed payloads/forged paths denied; parallel bits 0/255, repeat solves and new editions; start/end/restart receipts. |
| Browser lifecycle | Thirty unchanged polls retain board/menu/focus; remote moves and stale responses; provisional/rejected moves; native promotion cancellation and underpromotion; accessible castles; history, annotations, flip/resize, selected branches, mounted siblings, 409 reconciliation, ended controls and draft-protected HTMX navigation. |
| Exports | Owner/guest/view and owned played-branch scope; authoritative FEN/PGN; GIF decode checks every expected capture/castle/promotion/en-passant position and frame count; cancellation, saturation and resource bounds; persistent artwork attribution. |
| Live system | Actual Caddy forward auth/callback, Gateway-issued assertions and virtual passkey registration; independent anonymous guest; two real Chess processes sharing PostgreSQL and round-robin routing; replica restarts during a match and branching attempt; durable completed progress; real sign-out disables owner controls without invalidating the guest. |

The expiry review caught a PostgreSQL timing edge: `now()` is fixed at
transaction start. A guest request queued behind a match lock could otherwise
pass its absolute deadline before authorization. The shared predicate now uses
`clock_timestamp()`. A regression synchronizes on PostgreSQL's actual lock wait,
expires the session, then releases the lock; reads, new moves and receipt replays
all deny access and preserve state. All three cases failed before the fix.

Custom FEN positions retaining nonstandard castling rights now keep Chess960
semantics even without an imported Variant tag. Canonical king-to-rook input,
persisted mode and exported Variant survive reconstruction; king-destination
aliases are rejected for those positions.

## Original menu checklist

The match template supplies the same eleven entries to owners, guests and
viewers, with server permissions checked independently. The puzzle template
supplies those entries against an explicitly selected played branch.

| Entry | Match contexts | Personal puzzle |
| --- | --- | --- |
| New game | Normal authenticated setup destination; guest/viewer links lead through SSO. | Same setup destination. |
| New Fischer random game | Setup selects Chess960; server chooses a valid uniform start. | Same setup destination. |
| Play a puzzle | Authenticated collection destination. | Collection destination. |
| Create custom game | Current authoritative FEN initializes editor. | Selected branch FEN initializes editor. |
| Flip board | Local orientation only. | Selected board only. |
| Copy session link | Independent read-only capability, or an explicit disabled-viewing message. | Owned attempt URL; message explains account requirement. |
| Copy FEN | Authoritative FEN, with clipboard fallback. | Selected played branch FEN. |
| Copy PGN | Authorized match snapshot and stored result/setup. | Authorized selected path only. |
| Toggle viewers | Recent authorized tab leases, independent of game revision. | Explains that the personal attempt has no public viewers. |
| Download as GIF | Authorized complete bounded history, never silently truncated. | Selected acknowledged branch only. |
| Resign | Own color only; disabled for viewers and non-active matches. | Ends only this attempt; prior completion stays set. |

Rendering/handlers and permission checks were reviewed together. Browser checks
exercise native menus, selected branches, promotion/castles and export URLs;
HTTP/encoding tests cover authorization and downloaded contents. This does not
claim every clipboard platform or physical touch/audio device was exercised.

## Presentation and image

There are 40 maintained screenshots in `docs/screenshots/`: owner, guest, viewer
and expanded-puzzle contexts in light/dark at 320/390/768/1440 pixels. Browser
checks assert overflow and account-header alignment. Captures wait for board
animations to settle. Representative mobile/desktop screenshots were visually
reviewed, including neighboring text/badges and branch controls. Retired puzzle
parents form a compact summary above the required live boards. The five local
account-header CSS files compare byte-for-byte equal.

The final local image build and probe use:

```sh
docker --context default build --network host -t razcloud-chess:validation ./chess
# From the Chess repository:
bash tools/check-image.sh razcloud-chess:validation
bash -n scripts/live-acceptance.sh tools/check-image.sh
```

The build passed with the Distroless runtime, embedded local assets and license
bundle. The isolated image fixture verified startup/migrations/seeding, UID 10001
and the binary readiness command. It removed its containers/network afterward.
Parent/infra Git whitespace checks passed. No existing Compose service, operator
environment, production backend or Docker context was changed.

## Shared service styling — 25 November 2026

Chess now uses the platform cloud-home/service-name header with the shared
account control on the right. A compact PicoCSS navigation row replaces the
unstructured links and keeps its active marker current after HTMX navigation.
Match and puzzle moves use PicoCSS input/button groups. Both native game menus
share compact, consistently aligned link/button rows. The public playing page
shows the invited display name with a Guest badge; bootstrap and viewer pages
have no account identity. No dependencies or authorization paths changed.

Validation ran inside `chess/` with `GOCACHE=/tmp/razcloud-chess-ui-cache`,
`TEST_DATABASE_URL` pointing to a new disposable loopback-only PostgreSQL
container, and `CHROMIUM_EXECUTABLE_PATH=/opt/brave-bin/brave`:

- `gofmt` on the changed Go browser test; `go build ./...` and `go vet ./...`.
- `go test ./... -count=1` and `go test -race ./... -count=1`: all 140 checks
  passed in each suite; the opt-in live acceptance test skipped.
- Final focused `go test -race ./internal/app -run TestBrowser -count=1 -v`:
  all five browser checks passed after the menu contrast/mobile navigation fixes.
  The two screenshot-producing checks passed again after synchronizing captures
  with CSS theme transitions.
- Light/dark owner, guest, viewer and expanded-puzzle layouts at
  320/390/768/1440 pixels, plus both menus at 390/1440 pixels: 40 refreshed
  screenshots. Checks cover overflow, account/guest alignment, long guest-name
  truncation, grouped move alignment, all eleven menu entries, consistent menu
  text colors/targets, keyboard focus, persistent board/menu nodes, and the
  navigation marker after HTMX swaps. Representative screenshots were visually
  reviewed. Captures wait for board animations and CSS transitions to finish.
- The local account-header CSS still matches Gateway byte-for-byte;
  `git diff --check` passed.

Go checks used an isolated writable cache because the default cache was outside
the sandbox. Database/browser checks ran outside the sandbox for local sockets
and Chromium. The temporary PostgreSQL container was removed afterward.
No live SSO, production database, Docker image build, publication or deployment
was exercised for this styling change.

## Button-anchored game menus — 25 November 2026

Match and puzzle menus now use native CSS anchor positioning: their top-left
corner meets the invoking button's bottom-left corner when space permits.
Fallbacks flip horizontally, vertically or both, then allow horizontal shifting
and finally use the available viewport when neither side fits. The existing
height limit and scrolling keep all actions reachable on short screens.
Browsers without `position-area` support retain their default centered popover.
The final placement stays within five alternatives, the minimum required by the
[CSS specification](https://www.w3.org/TR/css-anchor-position-1/#position-try-fallbacks).

Validation used the same isolated Go cache and installed Chromium as above,
with a new disposable loopback PostgreSQL container:

- `gofmt`, `go build ./...`, `go vet ./...`, `go test ./... -count=1` and
  `go test -race ./... -count=1`: 140 checks passed per suite; opt-in live
  acceptance skipped.
- Browser checks now open both menus through their actual buttons and verify
  aligned edges, viewport bounds, all four corners and horizontal shifting in
  both themes at 390/1440 pixels. A 390×300 match verifies bounded scrolling
  and access to the final action. Existing 320/390/768/1440 layout and persistent
  state checks also passed with production CSP.
- Eight menu screenshots refreshed; representative desktop/mobile captures in
  both themes visually reviewed. `git diff --check` passed.

No JavaScript, templates or dependencies changed. Firefox/Safari and live SSO
were not exercised. No image publication or deployment ran.

## Handoff limits

No remote, commit, Git tag, parent gitlink or image was published, and nothing
was deployed. `chess/` is registered as a local parent submodule with intended
origin `git@github.com:razzie-cloud/chess.git`; remote availability and component
publication remain pending. Infra's pre-existing commit is preserved.
Physical authenticators, host-browser networking outside this machine, production
DNS/TLS and the actual Swarm cutover remain release-environment checks. Legacy
rooms/ownership are not imported. Release prerequisites and rollback are in
[the reference](reference.md#release) and the parent/infra operations documents.

## Puzzle mate lengths and consecutive IDs — 2026-11-25

The importer calculates the shortest forced mate within bounded search before
publication checks the single-fork, three-board limit. Compilation never replaces
an unsupported shortest strategy with a longer line. All 220 original positions
were verified as mate in two; the 206 supported positions now use consecutive
slots 0–205 and visible numbers/titles 1–206. The initial migration stores exact
`mate_in` values in the range 1–5. No additional migration was added.

Build, vet, full tests with disposable PostgreSQL, the maintained Chromium
browser suite and the full race suite passed. Browser checks include both themes at 320/390/768/1440,
consecutive links/titles, exact mate labels, HTMX navigation and existing puzzle
moves/retries/completion. Puzzle screenshots were refreshed and visually reviewed.
The full legacy-position test allows 30 seconds per entry for race instrumentation;
the production search timeout remains five seconds.

Only the local Chess puzzle tables and their receipts were reset. The existing
match was retained, and the old four puzzle attempts and one progress row were
cleared. The rebuilt local Chess service imported 206 mate-in-two definitions,
became healthy and retained the Caddy sign-in redirect. The actual Gateway login
flow was not repeated. No production deployment or publication was performed.

## Stateless puzzles and match navigation — 2026-11-26

Puzzles now opens every collection with its numbered puzzles and an initial-board
preview on hover or keyboard focus. Collection sections have no card background;
horizontal dividers separate collections. Sessions, Archive and this overview omit
the redundant page heading. Solved numbers have both a checkmark and color.

Opening a puzzle immediately enables play. Each move submits the complete
decision history and every board. The server reconstructs the canonical state
from the immutable proof, rejects edited or omitted defenses, applies the move,
and returns every updated board. Only an all-lines mate persists a completion
bit. There are no puzzle attempt routes, partial-progress records or sessions;
historical migration tables are retained unused. Reloading starts from the initial
position without clearing completion. Finished boards stay mounted and locked at
their mating positions, with the defender's king red. Next undone navigation
prefers later puzzles and wraps within the same collection.

New game is the first navigation item and opens normal, Fischer random and custom
setup choices. Match titles show both opponents, sourcing the host name from
verified identity; an append-only migration allows older matches to acquire that
name on their next owner visit. Game menu sits beside the title, with move and
access management in the side panel. Sessions and Archive use position cards.
Explicit FEN database tags fix the unfiltered list mapper so previews contain the
latest stored position, including completed games and the owner's orientation.

Validation used Go 1.27.4, `GOCACHE=/tmp/razcloud-chess-ui-cache`, a new disposable
loopback PostgreSQL container with isolated test schemas, and
`CHROMIUM_EXECUTABLE_PATH=/opt/brave-bin/brave`:

```sh
gofmt -l cmd internal tests
go build ./...
go vet ./...
go run ./cmd/puzzleprep verify -input internal/db/seeds/puzzles.json
go test -json ./... -count=1
go test -race -json ./... -count=1
git diff --check
```

Formatting, build, vet and bundle verification passed. Regular and race suites
each passed 162 tests/subtests across 13 tested packages; no race was reported.
The separate opt-in live test skipped in these runs. All seven browser tests ran
under production CSP, including both themes at 320/390/768/1440 pixels, unchanged
polls, drafts, promotion/castling, full-state puzzle moves, rejected moves,
locked siblings, tooltip focus/Escape, next-undone HTMX navigation, all three
new-game choices, card positions and control alignment. All 206 published puzzle
states replay to every required mate. Adversarial tests reject missing/duplicated
boards, changed FENs, completion flags, paths and decisions; replica retries and
concurrent completion bits pass without creating attempts. Representative final
desktop/mobile screenshots were visually reviewed, including tooltip viewport
captures, flat collections, completed red kings, side-panel controls and cards.

`CHROMIUM_EXECUTABLE_PATH=/opt/brave-bin/brave scripts/live-acceptance.sh` also
passed using newly built Gateway/Chess binaries and a disposable local stack.
It verified real Gateway/Caddy sign-in, the guest's host-versus-guest title,
two-sided moves and PGN, two round-robin replicas, replica restarts during a
match and full-state puzzle, durable completion, and actual Gateway sign-out
blocking owner/puzzle moves while preserving independent guest access. The
script removed its stack; the separate test PostgreSQL container was removed.
The pre-existing Air Compose Chess container remained healthy.

Firefox/Safari and physical authenticators were not exercised. No production
database, Docker context, deployment, image publication or remote was changed.

## 2026-11-26 — Shared theme and play sidebars

Chess now uses the platform's light/dark palette and gold selected navigation.
Match controls have spaced sections and dividers. Puzzle headings and playing
color remain above flat boards; status, navigation and move controls use the same
sidebar sections. Forks become stacked board/sidebar rows, retaining mounted
boards and completed lines. Shared puzzle feedback and navigation transfer to
the first surviving branch without duplication.

Gold knight cues explicitly name White or Black to move, using the canonical
position. Waiting and completed matches retain their own status, and completed
puzzle lines show checkmate instead of a move prompt. New game's fourth choice,
**Play a random puzzle**, selects from all available puzzle slots. The protected
redirect preserves the shell and selected puzzle URL; an empty collection list
returns to Puzzles. The in-game menu now contains only match actions.

Validation used Go 1.27.4, `GOCACHE=/tmp/razcloud-chess-ui-cache`, a disposable
loopback PostgreSQL container with isolated test schemas, and
`CHROMIUM_EXECUTABLE_PATH=/opt/brave-bin/brave`:

```sh
gofmt -l cmd internal tests
go build ./...
go vet ./...
go run ./cmd/puzzleprep verify -input internal/db/seeds/puzzles.json
go test -json ./... -count=1
go test -race -json ./... -count=1
git diff --check
```

Formatting, build, vet and seed verification passed. Regular and race suites each
passed 170 tests/subtests across 13 tested packages, including all seven browser
tests under production CSP; no race was reported. Browser checks cover both
themes at 320/390/768/1440 pixels, owner/guest/viewer layouts, persistent boards,
fork/completion/retry behavior, menu placement, control alignment and random-puzzle
navigation. Turn-status tests cover White, Black, own/opponent turns, viewers,
waiting/completed matches and canonical puzzle responses. Screenshots were
refreshed and representative desktop/mobile match, puzzle, completion and menu
views were inspected visually. The shared account-header stylesheet remains
identical to Files'.

The opt-in live Gateway SSO test skipped; live SSO acceptance and a Chess Docker
build were not repeated for this UI change. Firefox/Safari and physical
authenticators were not exercised. No production services, operator environment,
Docker context, remote, image publication or deployment was changed.

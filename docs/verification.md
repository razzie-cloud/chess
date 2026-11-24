# Chess implementation verification

Completed locally on 24–25 November 2026. The
[phased implementation record](plans/2026-11-24-chess-implementation.md)
maps the design's phases to the delivered source. This file records service
checks; [platform verification](platform-verification.md) records Compose,
Swarm, broker policies, helper fixtures, tag selection and Caddy validation.
[Live acceptance](live-acceptance.md) separately records real Gateway sessions,
virtual passkeys, anonymous guest play, round-robin replicas, restarts and sign-out.

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
bundle contains 207 accepted positions, with 13 rejected source entries retained
as drafts in their original permanent slots. No proof or opening artifact drift
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

There are 32 maintained screenshots in `docs/screenshots/`: owner, guest, viewer
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

## Handoff limits

No remote, commit, Git tag, parent gitlink or image was published, and nothing
was deployed. `chess/` is registered as a local parent submodule with intended
origin `git@github.com:razzie-cloud/chess.git`; remote availability and component
publication remain pending. Infra's pre-existing commit is preserved.
Physical authenticators, host-browser networking outside this machine, production
DNS/TLS and the actual Swarm cutover remain release-environment checks. Legacy
rooms/ownership are not imported. Release prerequisites and rollback are in
[the reference](reference.md#release) and the parent/infra operations documents.

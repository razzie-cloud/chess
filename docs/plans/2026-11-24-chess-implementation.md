# Chess phased implementation evidence

The [design contract](2026-11-24-chess-design.md) is implemented in the independent
[`chess/`](../../README.md) submodule. Application source is not tracked as
ordinary parent files. The intended GitHub remote was not accessible; the local
submodule uses `git@github.com:razzie-cloud/chess.git`, with component publication
required before publishing its parent pin. The service, platform configuration
and release preparation can be reviewed locally; remote/image publication and
production cutover remain pending.

The original design and implementation records now live in this repository.
Infra's pre-existing ahead-of-remote commit `55dbecf` was preserved. Component
work and parent integration are committed locally; no push, tag publication,
deployment, Docker context change or operator environment edit was performed.

## Phases

| Phase | Implemented slice and evidence |
| --- | --- |
| 0 — contracts and compatibility | Pinned original RazChess at `1da855236570ca9f56162b71e854bf4c37ea1ad5`; selected malbrecht rules library builds with Go 1.27.4; all 960 starts and required castling fixtures pass. Original ECO and artwork/library provenance retained. Browser fixtures exercise real chessboard.js, native controls and production CSP. Screened all 220 puzzle positions at explicit mate limits 2/3/4/5. |
| 1 — durable foundation | Independent module, mandatory signed Gateway verification, scoped broker discovery, strict origins, bounded startup/shutdown, private health probes, JSON logs, Goose session-locked migrations and PostgreSQL/go-rel store. Tests cover four concurrent migration runs, rollback, restarts and one-connection operation. |
| 2 — rules and openings | Strict UCI/FEN, legal Chess960 generation/castling, PGN import/export/result conflicts, terminal outcomes and history-aware repetition. All 3,397 original ECO records replay; deepest recognition survives departure, imports and reconstruction. |
| 3 — matches and authorization | Owner setup, guest fragment exchange, distinct 30-day per-match sessions, independent rotating view grants, own-color moves/resignation, durable receipts, permanent matches, indexed Sessions/Archive and confirmed deletion/tombstones. PostgreSQL race, replay, ownership and retention fixtures pass. |
| 4 — persistent interface | Stable board/menu/promotion nodes, revision-guarded state swaps, unchanged 204 polls, pending moves, polling/backoff/visibility lifecycle, opening/history/status, native controls, accessible UCI/castles, annotations and local sounds. Browser fixtures cover 30 unchanged polls, remote moves, old responses, promotion cancellation/underpromotion, castling and HTMX navigation/drafts. |
| 5 — puzzle preparation | Offline add/import/compile/verify/screen commands; deterministic bounded AND/OR proof and independent validation; all legal defenses; global one-fork rule; immutable slots and transactionally imported seeds. 207 accepted positions publish in their original slots; 13 rejected positions remain drafts. |
| 6 — puzzle gameplay | Owned resumable attempts, exact prescribed moves, at most three live boards, source-path transition receipts and atomic `BIT(256)` completion after every line. Tests cover stale tabs, retired/final retries, wrong owners, forged paths, repeated solves, editions, concurrent bits 0/255 and rejected slot 256. |
| 7 — original feature parity | All eleven menu items appear in match and puzzle contexts; owner/guest/viewer permissions differ appropriately. Custom editor, FEN counters/rights, PGN error drafts, clipboard fallback, independent links, branch-selected exports and viewer toggle are present. GIF fixtures decode capture, en passant, castling and promotion positions; downloads retain artwork attribution. |
| 8 — platform/release preparation | Parent Compose/Caddy/auth policies and tokenless reset; Chess PostgreSQL broker grant; Swarm service/tags/helper readiness/reset; six-service image selection; landing card; CI; licenses; backup/cutover/rollback docs. Legacy extra definition removed from discovery, with its rollback broker grant retained. Offline checks and real local image readiness pass. |
| 9 — whole-system acceptance | Database-backed unit/integration/race and production-CSP browser fixtures exercise reconstructed independent handlers, alternate puzzle requests and persistent state. Real Gateway/Caddy acceptance and the final command record are documented in the service verification files. Production release remains an operator action. |

Final acceptance also covers real Gateway sign-out with retained match/puzzle
boards and independent guest access. Review regressions prevent a guest session
from authorizing a queued read, move or receipt replay after its absolute expiry,
and preserve custom-FEN Chess960 rules and exported variants across restarts.
Final service build/vet and all 140 regular/race checks pass; the final
Distroless image startup/readiness probe passes as UID 10001. The complete
platform helper suite has 122 passing checks, including its race run.

## Puzzle content decision

The reviewed manifest uses a declared mate limit of three. Of the 220 original
positions, 207 satisfy the full proof and single-fork rules; 13 remain explicit
drafts, with rejection reasons and offending positions retained. Original source
indices map to permanent slots without renumbering or silent omissions. The
edition is `razchess-mates-v1` and has gaps where drafts are excluded.

At mate limit two, 206 positions prove (138 forced-line and 68 fork positions).
Limits three, four and five each accept 207; the additional position needs three
solver decisions along its longest line. Maximum accepted complete lines is
three, maximum total solver decisions is five, and screening recorded no search
budget exhaustion. Fixtures include forced, two-way and three-way solutions,
promotion/castling, a rejected true second fork and a rejected four-reply defense.
See [`puzzles/README.md`](../../puzzles/README.md), source manifests,
screening reports and the deterministic compiled seed for the complete record.

The old audio samples lacked independent provenance. They were replaced with
reproducible original tones; feedback categories remain. The twelve piece images
match the pinned chessboard.js release and retain verified Cburnett CC BY-SA 3.0
attribution. ECO records retain CC0 provenance. See
[`THIRD_PARTY_NOTICES.md`](../../THIRD_PARTY_NOTICES.md).

## Verification and remaining operator work

Exact commands, results and limits are recorded in
[`chess/docs/verification.md`](../verification.md),
[`platform-verification.md`](../platform-verification.md), and
[`live-acceptance.md`](../live-acceptance.md). Browser screenshots
cover owner/guest/viewer and branching-puzzle pages in light/dark at
320/390/768/1440 pixels; representative desktop/mobile and close-up alignment
views were visually inspected.

Before release, publish the independent component commits and image, verify
the registered Chess remote is available, and then publish parent pins. Select the published
`CHESS_IMAGE_TAG` in the operator environment, prepare both DNS/TLS hostnames,
back up PostgreSQL and the legacy Valkey data, explicitly remove the old running
service at authorized cutover, and retain its configuration/data for rollback.
The removed extra YAML does not automatically prune an old Swarm service.
Nothing imports legacy room ownership or synchronizes new state back to RazChess.
The [service release guide](../reference.md#release) and
[infra operations](../../../infra/docs/operations.md) provide the concrete sequence.

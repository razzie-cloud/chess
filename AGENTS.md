# Chess development

This independent Go 1.27 module is `github.com/razzie-cloud/chess`. Keep its
source here, coordination in the parent, and Swarm configuration in `infra`.
The design and phased implementation evidence live in `docs/plans/`.

PostgreSQL is authoritative for matches, grants, sessions, puzzle definitions,
attempts and progress. No Redis, game registry, sticky sessions or runtime solver.
Use pgx/database/sql, go-rel and append-only Goose migrations. Lock a match before
its grants/session/receipts; lock an attempt before its branches then progress.
Use one transaction connection, including with a one-connection pool.

Protected routes require current signed Gateway identity, never unsigned UUID or
role headers. Keep broker discovery and Gateway verifier source/tests synchronized
with Files/Notes. Public routes accept only guest sessions or separate viewing
capabilities and never forwarded identity. Validate exact configured host and
POST Origin, bound request bodies and never log tokens, URLs or backend errors.

Matches never expire automatically. Only their creator can delete them. Retain
creation tombstones after deletion; grants, moves and presence must cascade.
Guest authorization rechecks current generation and expiry on every operation.
The unreleased collection has 206 calculated mate-in-two puzzles at consecutive
slots 0–205; original indices remain in provenance. Import calculates shortest
mate length before publication applies the three-board limit. After release,
puzzle slots and published definitions are immutable; a content correction needs
a new collection edition. Completion bits only change from zero to one in SQL.

Frontend uses escaped embedded Go templates, local HTMX/PicoCSS/chessboard.js and
external scripts under CSP. Keep a board, menu and promotion dialog mounted across
state updates, reject stale revisions and retain drafts on errors. Public shells
omit signed-in account identity; authorized guest play shows the invited name and
a Guest badge, while viewers remain anonymous. Signed-in account CSS follows the identical platform copy.

Run `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` and
`go test -race ./...`. PostgreSQL integration tests require disposable loopback
`TEST_DATABASE_URL` and isolated random schemas. Never use production databases
or Docker contexts. Verify prepared seeds with `go run ./cmd/puzzleprep verify
-input internal/db/seeds/puzzles.json`; compilation must reproduce committed data.
Inspect desktop/mobile and both themes with production CSP.

Do not create/push a remote, publish images or deploy unless explicitly authorized.

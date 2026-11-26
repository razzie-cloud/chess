# Chess

Razzie Cloud's independent Go Chess service. PostgreSQL owns matches, guest and
viewing grants, immutable puzzle definitions and personal completion. Browsers use
ordinary POST moves and HTMX state updates; there are no game WebSockets,
process-local sessions, Redis dependency or runtime puzzle search.

Creators sign in through Access Gateway, choose their color and give an opponent
a private playing link. The opponent needs no account. A separate viewing link
is read-only. Matches remain until their creator explicitly deletes them;
completed matches appear in the Archive. Cloud administrators have no additional
access to other users' matches or puzzles.

Puzzles open ready to play from an overview containing every collection and
number, with initial-board previews on hover or keyboard focus. Every move sends
the complete history and all boards; the server replays them against the prepared
proof before replying with every updated board. All defense lines must reach
checkmate before personal progress changes from undone to done. Completed boards
stay locked at their final positions. There are no puzzle sessions: reloading a
puzzle starts from its initial position; completed progress stays saved. The
navigation button opens the collection's next undone puzzle.

The repository is registered locally as the parent's `chess` submodule, with
the intended origin `git@github.com:razzie-cloud/chess.git`. That remote and its
commits/image are not published yet. Publish component commits before publishing
the parent pin, and verify the image before selecting a deployment tag. Source
integration does not deploy the replacement for legacy RazChess.

The [design contract](docs/plans/2026-11-24-chess-design.md) and
[phased implementation evidence](docs/plans/2026-11-24-chess-implementation.md)
are maintained with this service.

## Run locally

The parent development stack supplies Caddy, Gateway, PostgreSQL and the scoped
broker. From the parent checkout, prepare its ignored development environment
and run `make local-auth`, then `docker compose up --build`. Chess uses
`http://chess.localhost:8080`; public playing/viewing links use
`http://shared-chess.localhost:8080`. `LOCAL_PORT` changes both origins together.
Only Caddy publishes a loopback port. Preserve existing volumes.

For standalone development, set `ORIGIN`, a different-host `SHARE_ORIGIN`,
`ACCESS_GATEWAY_URL`, `LANDING_ORIGIN`, and `POSTGRES_URI` or
`POSTGRES_URI_FROM`, then run `go run ./cmd/chess`. Gateway key discovery and
verified request assertions are mandatory; there is no unsigned development
fallback. Database discovery uses `BROKER_TOKEN_FILE` and optional
`BROKER_CA_FILE`. Each setting also has a matching CLI flag; see
[the reference](docs/reference.md).

## Verify

Run inside this module with the Go toolchain declared in `go.mod`:

```sh
gofmt -w cmd internal tests tools
go build ./...
go vet ./...
go test ./...
go test -race ./...
go run ./cmd/puzzleprep verify -input internal/db/seeds/puzzles.json
```

Database tests require a disposable loopback `TEST_DATABASE_URL` and create
isolated random schemas. Browser tests additionally require
`CHROMIUM_EXECUTABLE_PATH`; they exercise the production CSP and save screenshots
to `docs/screenshots/`. No database configured means integration tests skip.
The browser fixtures use real signed identity assertions and local key discovery;
[live acceptance](docs/live-acceptance.md) separately covers the actual Gateway
and Caddy flow.

The preparation command recompiles selected entries deterministically. The
bundled collection contains 206 calculated mate-in-two positions, numbered
1–206; 14 positions exceeding the shortest solution’s board limit are excluded. See [puzzle onboarding](puzzles/README.md)
and [rules/opening compatibility](docs/rules-compatibility.md). All 3,397 original
ECO records are retained and normalized locally.

Back up the complete PostgreSQL database, including capabilities, match history,
archive, puzzle definitions and progress (plus any historical attempt rows).
There is no match expiry or automatic match cleanup. See
[release and rollback](docs/reference.md#release).
Artwork/dependency notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Stockfish opponents

All three new-game forms support guests or five fixed Stockfish difficulties.
The optional private worker uses one thread with a bounded queue; Chess persists
bot turns and validates webhook moves transactionally. Build the worker with
`docker build -f Dockerfile.bot -t razcloud-stockfish:local .`, or use the parent
repository's ordinary `docker compose up --build -d` for a 1.25 CPU / 512 MiB
setup. Its main stack includes the worker; set a random 32+ character
`CHESS_BOT_TOKEN` in the parent's ignored `.env` before starting.
See [configuration, API and design](docs/plans/2026-11-26-stockfish-bot.md).

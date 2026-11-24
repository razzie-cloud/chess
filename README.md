# Chess

Razzie Cloud's independent Go Chess service. PostgreSQL owns matches, guest and
viewing grants, resumable puzzle attempts and personal completion. Browsers use
ordinary POST moves and HTMX state updates; there are no game WebSockets,
process-local sessions, Redis dependency or runtime puzzle search.

Creators sign in through Access Gateway, choose their color and give an opponent
a private playing link. The opponent needs no account. A separate viewing link
is read-only. Matches remain until their creator explicitly deletes them;
completed matches appear in the Archive. Cloud administrators have no additional
access to other users' matches or puzzles.

The repository is registered locally as the parent's `chess` submodule, with
the intended origin `git@github.com:razzie-cloud/chess.git`. That remote and its
commits/image are not published yet. Publish component commits before publishing
the parent pin, and verify the image before selecting a deployment tag. Source
integration does not deploy the replacement for legacy RazChess.

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
bundled collection contains 207 accepted original positions; 13 rejected entries
remain drafts in their original slots. See [puzzle onboarding](puzzles/README.md)
and [rules/opening compatibility](docs/rules-compatibility.md). All 3,397 original
ECO records are retained and normalized locally.

Back up the complete PostgreSQL database, including capabilities, match history,
archive, puzzle definitions, attempts and progress. There is no match expiry or
automatic match cleanup. See [release and rollback](docs/reference.md#release).
Artwork/dependency notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

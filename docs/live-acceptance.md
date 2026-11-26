# Live Gateway, guest play and replica acceptance

The 26 November 2026 rerun passed after the stateless-puzzle and navigation
changes, using `CHROMIUM_EXECUTABLE_PATH=/opt/brave-bin/brave` and
`GOCACHE=/tmp/razcloud-chess-ui-cache`. The live fixture now opens puzzles
directly and retains their complete client state across replica restarts;
only all-lines completion is persisted. It also checks the guest's opponent
title and Guest badge. Actual sign-out still blocks owner/puzzle moves while
leaving the independent guest capability valid. The disposable stack cleaned
itself up; the existing development Compose stack was left running. See
[the current verification record](verification.md#stateless-puzzles-and-match-navigation--2026-11-26).

Passed on 24 November 2026 and repeated with sign-out acceptance on 25 November
2026 using the independent local Chess worktree and the
parent checkout's Access Gateway source at
`a3b9d3dc7a4984291a8a79f6b4c752b29fb2facf`.

This run used real HTTP requests through Caddy, actual Gateway sessions and
forward-auth callbacks, and Gateway-issued PASETO assertions verified by Chess.
The browser test did not inject signed identity headers or intercept application
responses. Chromium's CDP Fetch hook continued requests unchanged. WebAuthn used
a disposable CTAP2 virtual authenticator with resident credentials, user
verification and automatic presence.

## Command and environment

From the Chess repository:

```sh
CHROMIUM_EXECUTABLE_PATH=/home/razzie/.cache/hlsrec/browser/chromium-1717349/chrome \
  scripts/live-acceptance.sh
```

The script builds both current source trees with `CGO_ENABLED=0 go build`, using
`GOCACHE=/tmp/razcloud-chess-gocache` unless overridden. Its default Gateway source
is `../access-gateway`; `CHESS_GATEWAY_SOURCE` can select another local checkout.
It requires the default local Docker context and a Unix-socket Docker daemon and
does not change contexts.

The run created a uniquely named Docker bridge and containers for PostgreSQL,
Valkey, Access Gateway, two independent Chess processes, and Caddy. It used the
already available local images `postgres:18-alpine`,
`valkey/valkey:9.1.0-alpine`, `caddy:2-alpine`, and
`gcr.io/distroless/static-debian13:nonroot`. The Gateway and Chess containers
mounted the newly built binaries read-only. Only Caddy published a port, bound
to `127.0.0.1` at an ephemeral available port. The actual origins were
`http://auth.localhost:<port>`, `http://chess.localhost:<port>` and
`http://shared-chess.localhost:<port>`.

PostgreSQL was a new disposable server/database with separate `gateway` and
`chess` schemas. Gateway used only that server and the new Valkey container.
Both Chess processes used the same `chess` schema, public origins and private
Gateway discovery origin. Caddy's protected and public Chess upstreams used
`lb_policy round_robin`; no affinity was configured. The script executed the
actual binaries' `healthcheck` commands before acceptance and after restarts.

The script supplied the generated local origins and private bootstrap-log path
to this exact opt-in test command:

```sh
go test ./internal/app -run '^TestLiveGatewaySSOAndAnonymousGuest$' -count=1 -v
```

`CHESS_LIVE_RESTART_CONTAINER` named only the disposable replica; the test refuses
to restart a container outside that fixture's naming pattern. Ordinary unit and
race suites skip this opt-in test when its live fixture environment is absent.

## Observed acceptance

The final run, including sign-out acceptance, passed in 18.58 seconds:

- The Gateway's real first-user registration created a passkey and administrator
  account through the browser bridge. The bootstrap credential was read from a
  private temporary log and was never printed.
- Navigation to protected Chess completed Gateway authorization and its
  `/_auth/callback` flow, then rendered the authenticated username and the signed
  Administrator badge.
- The authenticated creator created a standard match. A separate anonymous
  browser opened its fragment playing link, scrubbed the fragment and displayed
  no cloud identity. Loading the bootstrap page left the match pending; the
  explicit Continue button exchanged the capability and activated the match.
- The creator played `e2e4` and the guest played `e7e5`. Both browsers observed
  the authoritative revisions and history. The guest exported PGN containing
  both moves. The public host returned 404 for personal puzzle routes.
- One actual Chess container restarted after the creator's move. The guest
  session and durable match continued through the other and restarted processes.
- The signed-in creator started the reviewed branching puzzle at
  `razchess-mates-v1/9`, submitted its first prescribed move and obtained the two
  required defense branches. The replica restarted again, and a fresh page
  resumed the expanded attempt. Every remaining required solver decision was
  accepted, both lines finished, and a subsequent collection read showed the
  durable solved bit.
- After completion, retained match and puzzle pages shared the same actual
  Gateway session with a fixture profile that submitted Gateway's real Sign out
  form. Their next state reads stopped interaction and displayed native
  same-origin **Sign in again** links without replacing either board with login
  HTML. The anonymous guest still exported the unchanged match successfully.
  Private cookie copies between isolated fixture profiles established the shared
  real session; no signed identity header or application response was injected.
- The owner, puzzle and guest browser contexts reported no uncaught JavaScript
  errors.

Earlier runs exposed only fixture problems: PostgreSQL's temporary initialization
server was mistaken for final readiness, a DOM-element wait predicate needed
Boolean coercion, and an import was briefly misplaced during test editing. The
final script checks the PostgreSQL TCP listener and waits for complete documents
before using their deferred browser hooks. The final opt-in test passes.

The sign-out follow-up fixed a browser behavior gap: automatic state-fetch
redirects reached the external Gateway origin and left a generic connection
error under the local-only CSP. State reads now use manual redirect handling,
recognize expired access, stop mutation controls and polling, and offer native
sign-in navigation. Puzzle transport ambiguity also triggers a fresh authorized
state read. Public capability failures retain their separate access-expired
message rather than requiring a cloud account.

## Cleanup and scope

The EXIT trap removed all six fixture containers, their anonymous volumes, the
test network, binaries and private logs. All four browser profiles were removed.
Post-run `docker ps -a --filter name=chess-live-` and
`docker network ls --filter name=chess-live-` returned no resources. The existing
parent PostgreSQL test fixture was not used or changed by this run.

No production database, Docker context, deployment, operator environment, remote
repository, published image or legacy RazChess data was changed. The bootstrap
account, passkey, sessions, capabilities, match, puzzle attempt and progress bit
existed only in the discarded fixture.

This run verifies local HTTP SSO and process restart behavior with virtual
WebAuthn. HTTPS/DNS on the deployment hosts and physical authenticators remain
release-environment checks. The separate browser matrix covers both themes,
responsive layouts, promotion, delayed responses and persistent board controls;
the separate rule/opening tests cover opening reconstruction and notation.

# Stockfish bot server and Chess integration

Implemented locally on 26 November 2026. The worker is a separate executable and
container built from this repository. Publication and production rollout remain
operator actions. See [verification](../verification.md#stockfish-bots--26-november-2026).

## Execution and resource budget

The private worker exposes one application endpoint, `POST /jobs`, and ordinary
GET/HEAD `/healthz` and `/readyz` probes. It needs no database, volume, Redis,
public hostname or browser credentials. One FIFO consumer runs one Stockfish
process at a time. Every search uses `Threads=1`, `Hash=32` MiB, `Ponder=false`
and a bounded search time. A fresh process per turn isolates engine state and
crashes; it is killed and reaped before another starts. Startup performs a real
search before accepting work. Each subsequent search has a five-second hard
deadline, including initialization. Failed searches are retried by Chess.

The local container has a hard **1.25 CPU / 512 MiB** limit and a 32-process/thread
limit. Go uses one processor and a 64 MiB soft memory target; Stockfish's process
memory is additional and stays under the container's shared hard limit. The
32 MiB hash leaves room for NNUE networks, board history and the HTTP server.
No pondering or analysis runs between jobs. CPU consumption during ordinary
searches should be near one core. Extra allowance covers startup/HTTP overhead.

`BOT_QUEUE_SIZE` defaults to **8**, valid from 1 to 32, counting running,
validating, queued and callback-delivery jobs together. A full queue returns
`429` with `Retry-After: 5`; it never spawns another engine or grows the queue.
One admission slot bounds simultaneous JSON/PGN validation; competing validations
return `429` with `Retry-After: 1`. HTTP JSON is limited to 300 KiB and the PGN to
256 KiB / 2,048 plies. Request IDs deduplicate work currently held by the worker;
completed IDs are released rather than accumulating an unbounded registry.

| Level | UI label | Stockfish Skill Level | Search budget |
| --- | --- | --- | --- |
| 1 | Beginner | 0 | 100 ms |
| 2 | Easy | 3 | 200 ms |
| 3 | Medium | 7 | 350 ms |
| 4 | Hard | 12 | 600 ms |
| 5 | Expert | 20 | 1,000 ms |

These are relative presets, without Elo promises. Even minimum-skill Stockfish
can challenge beginners; strength and search depth depend on hardware and engine
version. [Stockfish documents the skill and resource controls](https://github.com/official-stockfish/Stockfish/blob/master/src/engine.cpp)
and [explains weakened play](https://official-stockfish.github.io/docs/stockfish-wiki/Stockfish-FAQ.html).
Tune these five presets only after human playtesting, preserving the budget.

## Private HTTP contract

Both directions require `Authorization: Bearer <BOT_TOKEN>`. Generate an
independent random token of at least 32 characters; broker tokens and Gateway
identity assertions serve different purposes. Production can mount a Docker
secret through `BOT_TOKEN_FILE`, which takes precedence over `BOT_TOKEN`.
Only trusted services on the internal network should reach either listener.
Use TLS if requests cross an untrusted network. Neither URL is routed by Caddy.

```http
POST /jobs
Content-Type: application/json
Authorization: Bearer <shared bot credential>

{
  "id": "faec4656-e0d3-47d9-8831-47d35267f74a",
  "pgn": "[Result \"*\"]\n\n1. e4 *",
  "level": 3,
  "webhook": "http://chess:8082/bot/moves"
}
```

The UUID identifies this position revision's job, not a public match capability.
The request's webhook must **exactly** match the worker's operator-configured
`BOT_WEBHOOK_URL`. Arbitrary callback destinations, URL credentials, query
strings and fragments are rejected. Outbound calls use no environment proxy and
never follow redirects, so credentials cannot escape via a redirect.

| Response | Meaning |
| --- | --- |
| `202` | Accepted, or an identical ID/payload is already held; no response body. |
| `400` | Malformed, oversized, unknown-field or trailing JSON. |
| `401` | Missing/incorrect bearer credential. |
| `409` | Held ID reused with different details. |
| `422` | Invalid UUID, difficulty or webhook; invalid or completed-game PGN. |
| `429` | Capacity exhausted or another admission is being validated. |

PGN is parsed and legally replayed before acceptance. Setup/FEN and Chess960
tags are supported. The engine receives the starting FEN and the entire mainline,
preserving repetition context. UCI Chess960 mode is always enabled internally
to match Chess's canonical king-to-rook castling representation, including
orthodox positions. The worker checks its returned move against the legal moves
before delivery. Promotions include their lower-case UCI suffix.

```http
POST /bot/moves
Content-Type: application/json
Authorization: Bearer <shared bot credential>

{"id":"faec4656-e0d3-47d9-8831-47d35267f74a","uci":"e7e5"}
```

The callback has a two-second timeout and up to three immediate attempts. Any
2xx response acknowledges it. Callback failure releases the worker's slot;
Chess's durable retry will resubmit the same job. A crash can discard all worker
RAM state. Delivery is at least once, with an idempotent Chess consumer.

## Durable match integration

An append-only migration adds `matches.bot_level` (zero for existing guest
matches) and `match_bot_jobs`, with at most one current job per match. Creating
or updating an active match queues its next bot turn **inside the same database
transaction**. Bot matches start active immediately, retain a viewing link, and
have no guest playing grant or guest session. The creator can play only their
chosen color. Difficulty is immutable for that match.

A dispatcher in each Chess replica claims a job using a short PostgreSQL
`FOR UPDATE SKIP LOCKED` lease, then performs HTTP outside the transaction. The
30-second lease permits retry after a lost request/response, engine restart or
missing callback. Rejected submissions, including queue-full responses, become
eligible after five seconds. Dispatch is capped at one submission per second per
Chess replica. Worker overload delays bot turns without rolling back human moves.
The durable PostgreSQL backlog is bounded to one row per pending bot turn; the
worker's configured capacity bounds its accepted in-memory work, not the total
number of persisted Chess matches.

The callback lives on Chess's separate private `BOT_PORT` (default 8082), with
only `POST /bot/moves`. The two existing browser hosts do not expose this route.
After authenticating the bot credential, Chess finds the job, locks the match
before the job, checks current revision, status and bot seat, and legally replays
the returned move. A successful callback commits one canonical move with actor
`bot`, updates outcome/revision and removes the job atomically. Repeated,
superseded or deleted job callbacks receive `204` without changing the board.
Illegal moves receive `422`; transient persistence failures receive `503`.
Resignation, completion and deletion cancel outstanding jobs. A concurrent
callback either commits first or observes cancellation. Link management that
changes revision requeues the current bot turn with a new ID.

PostgreSQL remains authoritative across Chess replicas and restarts. No
browser poll or open tab is needed to trigger a bot turn. A bot turn stays
pending during engine outages and resumes on retry; it does not expire or
forfeit the match. Back up the new job table with the ordinary Chess database.

## New-match experience

Normal, Fischer random and custom setup all offer **Guest with a private link**
or **Stockfish bot**, followed by five named difficulty choices. Choosing a bot
removes the guest-name requirement, preserves the inactive guest draft, and uses
the selected bot name in Sessions, Archive and match titles. Your White/Black
choice still controls orientation and seat; imported games retain their side to
move. If the bot plays first, creating the match immediately queues its move.
Already completed imports remain archived without a search.

Bot matches omit playing-link controls, show when the bot turn is waiting, and
use the existing state poll to display the response and unlock your next move.
Viewing links, exports, resignation and deletion keep their usual behavior.
Custom-position validation and error rendering retain opponent and difficulty.
Bot choices are disabled when Chess has no `BOT_URL`; existing bot matches remain
readable until the operator re-enables dispatch. See the
[desktop preview](../screenshots/setup-bot-light-1440.png) and
[mobile dark preview](../screenshots/setup-bot-dark-390.png).

## Run locally

From the parent repository, set a fresh `CHESS_BOT_TOKEN` in the existing ignored
`.env` (generate with `openssl rand -hex 32`), then use:

```sh
docker compose up --build -d chess stockfish
```

The main Compose file includes the worker and builds `chess/Dockerfile.bot`. It
joins only the private backend network, sets both directions' URLs and token, and
applies the hard CPU/RAM/process limits.
It is also included with the `docker-compose.none.yml` broker-auth override. No host
ports, public routes or new persistence volumes are added. Source changes to the
worker require rebuilding its image; Chess continues using its existing Air setup.

Outside Compose, build `go build -o stockfish-server ./cmd/stockfish-server`,
install Stockfish and set `STOCKFISH_PATH` if different from
`/usr/games/stockfish`. Worker configuration is `SERVICE_PORT`, `BOT_TOKEN` or
`BOT_TOKEN_FILE`, `BOT_WEBHOOK_URL`, and optional `BOT_QUEUE_SIZE`.
Chess needs `BOT_URL` (full `/jobs` URL), the same `BOT_WEBHOOK_URL` and credential,
and optional `BOT_PORT`. Existing Chess database/Gateway settings still apply.

For Swarm, the production `infra/docker-stack.yml` now includes a private worker
with one replica, stop-first updates and hard 1.25 CPU / 512 MiB limits. The Chess
release workflow publishes `ghcr.io/razzie-cloud/chess-stockfish` from
`Dockerfile.bot` beside the ordinary Chess image, using the same `CHESS_IMAGE_TAG`.
Both images must be published before selecting that release; the deploy helper
pulls both in preflight. Parent tag selection continues using Chess's shared tag.

The deploy helper automatically creates the external `chess_bot_token` Docker
secret if missing and preserves existing values, independently of broker auth
mode. It mounts the secret into both containers as UID/GID 10001, mode 0400;
`BOT_TOKEN_FILE` reads it without putting credentials in the environment or stack
output. Production does not need the local `CHESS_BOT_TOKEN` setting in `.env`.
`CHESS_BOT_QUEUE_SIZE` is exported through infra's Makefile. The private URLs are
`http://stockfish:8080/jobs` and `http://chess:8082/bot/moves`; neither endpoint is
published through Caddy. Both Chess replicas coordinate dispatch and callbacks
through PostgreSQL, and worker readiness participates in deployment acceptance.
See infra's `docs/operations.md#chess-stockfish-worker` for release and secret
lifecycle details. No production deployment was performed while preparing this.

The Debian Stockfish package includes its NNUE data and GPL notices. The worker
image includes those notices and the matching Chess dependency licenses. If
distributing the built image, provide corresponding Stockfish source under its
GPL terms; Debian's matching source package identifies the engine source.

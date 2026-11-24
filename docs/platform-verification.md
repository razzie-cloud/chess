# Rules, assets and platform verification

Checked on 24 November 2026 using Go 1.27.4. Commands were run inside the
indicated independent module or parent checkout. `GOCACHE` pointed at a disposable
directory under `/tmp` because the default build cache is outside the sandbox.

## Rules and openings

In Chess, `go test -race ./internal/game ./internal/opening` passed all 14 tests.
`go vet ./internal/game ./internal/opening ./cmd/ecoprep` passed. The full original
ECO dataset has 3,397 accepted records and zero rejected records; regeneration
matches the bundled artifact byte for byte. Fixtures cover all 960 unique valid
starts, normal and stationary-piece castles, promotion, en passant, pinned moves,
draw precedence, repetition identity, bounded PGN and result/setup conflicts.
Detailed provenance and limits are in `rules-compatibility.md`.

The twelve piece PNGs match the pinned chessboard.js release byte for byte.
Commons metadata was checked for all twelve Cburnett designs; licenses and
attribution are in `THIRD_PARTY_NOTICES.md` and `artwork-provenance.json`.
Two complete runs of `go run ./tools/generate-sounds.go` produced identical
SHA-256 hashes for every new tone; FFprobe verified mono 22,050 Hz Vorbis output.
No original audio recordings are retained.

## Parent orchestration

These commands passed without starting the existing Compose stack:

```sh
docker compose --env-file .env.example config --quiet
docker compose --env-file .env.example --profile storage-demo config --quiet
docker compose --env-file .env.example -f docker-compose.yml -f docker-compose.none.yml config --quiet
bash scripts/test-update-image-tags.sh
bash scripts/test-local-auth.sh
bash -n scripts/update-image-tags.sh scripts/test-update-image-tags.sh scripts/test-local-auth.sh infra/deploy.sh
```

The resolved example Compose JSON was also checked for Chess's distinct origins,
dedicated PostgreSQL discovery/token, explicit local gateway allowlist entry and
complete token/volume reset in none mode. Both development and production broker
policies passed the real `broker-token validate` command. Each grants Chess only
`postgres/chess`; the old `valkey/razchess` rollback grant is unchanged.

The tag fixture resolves six platform remotes, rejects prereleases/malformed
tags, preserves unrelated selections and file mode, and keeps the environment
unchanged on final-service discovery/no-stable-tag/fetch failures. The auth
fixture generates only temporary development credentials and verifies private
directory/master permissions, the Chess token and idempotent regeneration.

## Infra

In `infra/`, `go build ./...`, `go vet ./...`, `go test ./cmd/infra-helper` and
`go test -race ./cmd/infra-helper` passed all 122 helper tests, including explicit
Chess token ownership, no Valkey allocation, two-replica acceptance readiness and
none-mode resources. Offline core/extra round trips passed:

```sh
make ENV_FILE=.env.example DOCKER_CONTEXT=default DATABASE_BROKER_IMAGE_TAG=validation ACCESS_GATEWAY_IMAGE_TAG=validation FILES_IMAGE_TAG=validation NOTES_IMAGE_TAG=validation CHESS_IMAGE_TAG=validation LANDING_IMAGE_TAG=validation config
make ENV_FILE=.env.example DOCKER_CONTEXT=default config-extra
```

These placeholder tags establish interpolation only. They were command-line
overrides; no operator environment was changed. Extra-file acceptance now covers
the three remaining public extras. The pre-existing local infra commit was
preserved; no commits were pushed and no Swarm deployment ran.

## Container and proxy

The default Docker endpoint was verified as local `cachyos-desktop`, with Swarm
inactive. `docker --context default build --network host -t razcloud-chess:validation ./chess`
passed with the Distroless runtime and license bundle. From Chess,
`bash tools/check-image.sh razcloud-chess:validation` passed: an isolated internal
network, disposable PostgreSQL and mock public-key discovery exercised real
migrations/seeding/startup, UID 10001 and the image's binary readiness command.
The fixture removed its own containers/network afterward. It did not start or
change existing Compose services or volumes.

The local Caddyfile passed a disposable network-disabled Caddy check:

```sh
docker --context default run --rm --network none -e LOCAL_PORT=8080 -v /home/razzie/Projects/razcloud/Caddyfile:/etc/caddy/Caddyfile:ro caddy:2-alpine caddy adapt --config /etc/caddy/Caddyfile --adapter caddyfile --validate
```

Its generated JSON retains identity stripping, signed-header copying and
ordinary forward auth on every Chess app path, plus only the five explicit
public route families and a final 404 on the shared host.

## Remaining external acceptance

This report does not claim live Gateway SSO/callback, production DNS/TLS,
hardware/browser-host access or a deployment. Browser and whole-service tests
are recorded separately by the implementation lead. CI now requires an installed
Chromium/Chrome binary, runs the database-backed browser fixtures, uploads their
screenshots, checks compiled puzzle/ECO drift and builds/probes the image.
The new remote/image publication remains pending. The submodule is registered
locally; never publish its parent pin before the component commit is available.
Read infra's Chess cutover/rollback guide
before replacing the legacy running service.

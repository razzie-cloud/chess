# Offline asset preparation

Run `go run ./tools/generate-sounds.go` from the module root to regenerate the
five original `internal/web/static/sounds/*.ogg` files. It synthesizes mono
22,050 Hz sine-wave motifs with bounded decay envelopes and invokes local
FFmpeg/libvorbis with bit-exact encoding and no inherited metadata. It reads no
audio samples and makes no network requests. FFmpeg is needed only for this
preparation command, never in the runtime image.

The checked artifact was generated using FFmpeg `n9.1`; two complete runs
produced identical SHA-256 hashes for all five files. Encoding library/version
changes can change Ogg bytes; review those changes alongside this source. Motifs
last 0.08–0.39 seconds and distinguish move, capture, check, illegal move and
game-over feedback. Browsers may require a user gesture before audio playback.

The complete browser/artwork/data attribution is in `THIRD_PARTY_NOTICES.md`.
The source and compiled ECO data are regenerated with
`go generate ./internal/opening`; puzzle preparation uses `cmd/puzzleprep`.

After `docker build -t razcloud-chess:validation .`, run
`bash tools/check-image.sh razcloud-chess:validation` to verify the container's
UID and shell-free readiness probe. It requires a local Unix Docker daemon
outside Swarm, plus Python 3 and the PostgreSQL/Caddy fixture images. An isolated
internal network, disposable PostgreSQL and mock public-key discovery are
removed afterward; existing Compose services and volumes are untouched.
`CHESS_TEST_DOCKER_CONTEXT` selects the explicit local context (default `default`).
This fixture proves startup/readiness, not live SSO or signed request acceptance.

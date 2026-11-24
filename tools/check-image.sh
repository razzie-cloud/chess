#!/usr/bin/env bash
# Only isolated disposable resources are created; the existing Compose stack is untouched.
set -euo pipefail
task_context="${CHESS_TEST_DOCKER_CONTEXT:-default}"
task_image="${1:-razcloud-chess:validation}"
task_docker() { docker --context "$task_context" "$@"; }
if [[ "$(task_docker context inspect "$task_context" --format '{{.Endpoints.docker.Host}}')" != unix://* ]] || [[ "$(task_docker info --format '{{.Swarm.LocalNodeState}}')" != inactive ]]; then
  printf 'Image check requires a local Unix Docker daemon outside Swarm.\n' >&2
  exit 1
fi
task_docker image inspect "$task_image" >/dev/null
task_dir=$(mktemp -d /tmp/razcloud-chess-image.XXXXXX)
task_net=$(basename "$task_dir")
task_pg="$task_net-postgres"
task_gateway="$task_net-gateway"
task_app="$task_net-app"
cleanup() {
  task_docker rm -f "$task_app" "$task_gateway" "$task_pg" >/dev/null 2>&1 || true
  task_docker network rm "$task_net" >/dev/null 2>&1 || true
  rm -rf -- "$task_dir"
}
trap cleanup EXIT
mkdir -p "$task_dir/.well-known"
python3 - "$task_dir/.well-known/paseto-keys" <<'PY'
from datetime import datetime,timedelta,timezone
from pathlib import Path
import hashlib,json,sys
now=datetime.now(timezone.utc)
key=bytes(32)
stamp=lambda d:d.isoformat().replace('+00:00','Z')
Path(sys.argv[1]).write_text(json.dumps({'issuer':'https://auth.example','expires_at':stamp(now+timedelta(hours=1)),'keys':[{'kid':hashlib.sha256(key).hexdigest()[:32],'public_key':key.hex(),'not_before':stamp(now-timedelta(minutes=1)),'expires_at':stamp(now+timedelta(hours=1))}]}))
PY
cat > "$task_dir/Caddyfile" <<'CADDY'
{
 admin off
 auto_https off
}
:8081 {
 root * /srv
 file_server
}
CADDY
chmod -R a+rX "$task_dir"
task_docker network create --internal "$task_net" >/dev/null
task_docker run -d --rm --name "$task_pg" --network "$task_net" --network-alias postgres -e POSTGRES_USER=chess_test -e POSTGRES_DB=chess_test -e POSTGRES_HOST_AUTH_METHOD=trust postgres:17-alpine >/dev/null
for task_try in {1..30}; do
 if task_docker exec "$task_pg" pg_isready -U chess_test -d chess_test >/dev/null 2>&1; then break; fi
 sleep 1
done
task_docker run -d --rm --name "$task_gateway" --network "$task_net" --network-alias gateway -v "$task_dir:/srv:ro" caddy:2-alpine caddy run --config /srv/Caddyfile --adapter caddyfile >/dev/null
task_docker run -d --rm --name "$task_app" --network "$task_net" --network-alias app -e 'POSTGRES_URI=postgres://chess_test@postgres:5432/chess_test?sslmode=disable' -e ORIGIN=https://chess.example -e SHARE_ORIGIN=https://shared-chess.example -e LANDING_ORIGIN=https://home.example -e ACCESS_GATEWAY_URL=http://gateway:8081 "$task_image" >/dev/null
task_ready=false
for task_try in {1..30}; do
 if task_docker exec "$task_app" /usr/local/bin/chess healthcheck; then task_ready=true; break; fi
 sleep 1
done
if [[ "$task_ready" != true ]]; then
 task_docker logs "$task_app" >&2
 exit 1
fi
[[ "$(task_docker inspect -f '{{.Config.User}}' "$task_app")" == '10001:10001' ]]
# Probe uses only the container's binary; no shell/wget is available in the image.
task_docker exec "$task_app" /usr/local/bin/chess healthcheck
printf 'Distroless Chess image startup and binary readiness passed (UID10001, isolated PostgreSQL and mock key discovery).\n'

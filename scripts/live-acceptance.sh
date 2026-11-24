#!/usr/bin/env bash
# Disposable local Gateway/Chess/Valkey/PostgreSQL/Caddy acceptance, never deploys.
set -euo pipefail

chess_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
gateway_dir="${CHESS_GATEWAY_SOURCE:-$chess_dir/../access-gateway}"
if [[ ! -f "$gateway_dir/go.mod" ]]; then
  echo 'Set CHESS_GATEWAY_SOURCE to the local Access Gateway repository.' >&2
  exit 1
fi
if [[ -z "${CHROMIUM_EXECUTABLE_PATH:-}" ]]; then
  echo 'Set CHROMIUM_EXECUTABLE_PATH to a local Chromium executable.' >&2
  exit 1
fi
if [[ "$(docker context show)" != default ]]; then
  echo 'Live acceptance requires the default local Docker context.' >&2
  exit 1
fi
case "${DOCKER_HOST:-unix:///var/run/docker.sock}" in
  unix://*) ;;
  *) echo 'Live acceptance requires a local Unix-socket Docker daemon.' >&2; exit 1 ;;
esac
fixture_dir="$(mktemp -d /tmp/chess-live-XXXXXX)"
fixture_name="chess-live-${fixture_dir##*-}"
network="$fixture_name-network"
postgres="$fixture_name-postgres"
valkey="$fixture_name-valkey"
gateway="$fixture_name-gateway"
chess="$fixture_name-chess"
chess_replica="$fixture_name-chess-replica"
caddy="$fixture_name-caddy"
cleanup() {
  docker rm -fv "$caddy" "$chess_replica" "$chess" "$gateway" "$valkey" "$postgres" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf -- "$fixture_dir"
}
trap cleanup EXIT
public_port="$(python3 - <<'PY'
import socket
with socket.socket() as listener:
    listener.bind(('127.0.0.1', 0))
    print(listener.getsockname()[1])
PY
)"
auth_origin="http://auth.localhost:$public_port"
chess_origin="http://chess.localhost:$public_port"
share_origin="http://shared-chess.localhost:$public_port"

export GOCACHE="${GOCACHE:-/tmp/razcloud-chess-gocache}"
(cd "$gateway_dir" && CGO_ENABLED=0 go build -o "$fixture_dir/access-gateway" ./cmd/access-gateway)
(cd "$chess_dir" && CGO_ENABLED=0 go build -o "$fixture_dir/chess" ./cmd/chess)

docker network create "$network" >/dev/null
docker run -d --name "$postgres" --network "$network" --network-alias postgres \
  -e POSTGRES_PASSWORD=chess-live-test-only -e POSTGRES_DB=chess_live \
  postgres:18-alpine >/dev/null
docker run -d --name "$valkey" --network "$network" --network-alias valkey \
  valkey/valkey:9.1.0-alpine valkey-server --save '' --appendonly no >/dev/null
for _ in {1..60}; do
  if docker exec "$postgres" pg_isready -h 127.0.0.1 -U postgres -d chess_live >/dev/null 2>&1; then break; fi
  sleep 0.2
done
docker exec "$postgres" psql -U postgres -d chess_live -v ON_ERROR_STOP=1 \
  -c 'CREATE SCHEMA gateway; CREATE SCHEMA chess;' >/dev/null

docker run -d --name "$gateway" --network "$network" --network-alias gateway \
  --mount "type=bind,src=$fixture_dir/access-gateway,dst=/usr/local/bin/access-gateway,readonly" \
  -e SERVICE_PORT=8080 -e "ORIGIN=$auth_origin" -e RP_ID=auth.localhost \
  -e RP_NAME=Chess-live-fixture -e "FORWARD_AUTH_ORIGINS=$chess_origin" \
  -e 'POSTGRES_URI=postgres://postgres:chess-live-test-only@postgres:5432/chess_live?sslmode=disable&search_path=gateway' \
  -e 'REDIS_URI=redis://valkey:6379/0' \
  --entrypoint /usr/local/bin/access-gateway gcr.io/distroless/static-debian13:nonroot >/dev/null
for _ in {1..60}; do
  if docker exec "$gateway" /usr/local/bin/access-gateway healthcheck >/dev/null 2>&1; then break; fi
  sleep 0.2
done
docker exec "$gateway" /usr/local/bin/access-gateway healthcheck
docker logs "$gateway" >"$fixture_dir/gateway.log" 2>&1
chmod 600 "$fixture_dir/gateway.log"

docker run -d --name "$chess" --network "$network" --network-alias chess \
  --mount "type=bind,src=$fixture_dir/chess,dst=/usr/local/bin/chess,readonly" \
  -e SERVICE_PORT=8080 -e "ORIGIN=$chess_origin" -e "SHARE_ORIGIN=$share_origin" \
  -e "LANDING_ORIGIN=http://localhost:$public_port" -e ACCESS_GATEWAY_URL=http://gateway:8080 \
  -e 'POSTGRES_URI=postgres://postgres:chess-live-test-only@postgres:5432/chess_live?sslmode=disable&search_path=chess' \
  --entrypoint /usr/local/bin/chess gcr.io/distroless/static-debian13:nonroot >/dev/null
for _ in {1..60}; do
  if docker exec "$chess" /usr/local/bin/chess healthcheck >/dev/null 2>&1; then break; fi
  sleep 0.2
done
docker exec "$chess" /usr/local/bin/chess healthcheck
docker run -d --name "$chess_replica" --network "$network" --network-alias chess-replica \
  --mount "type=bind,src=$fixture_dir/chess,dst=/usr/local/bin/chess,readonly" \
  -e SERVICE_PORT=8080 -e "ORIGIN=$chess_origin" -e "SHARE_ORIGIN=$share_origin" \
  -e "LANDING_ORIGIN=http://localhost:$public_port" -e ACCESS_GATEWAY_URL=http://gateway:8080 \
  -e 'POSTGRES_URI=postgres://postgres:chess-live-test-only@postgres:5432/chess_live?sslmode=disable&search_path=chess' \
  --entrypoint /usr/local/bin/chess gcr.io/distroless/static-debian13:nonroot >/dev/null
for _ in {1..60}; do
  if docker exec "$chess_replica" /usr/local/bin/chess healthcheck >/dev/null 2>&1; then break; fi
  sleep 0.2
done
docker exec "$chess_replica" /usr/local/bin/chess healthcheck

cat >"$fixture_dir/Caddyfile" <<EOF
{
  admin off
  auto_https off
}
$auth_origin {
  reverse_proxy gateway:8080
}
$chess_origin {
  route {
    request_header -X-Auth-Request-User
    request_header -X-Auth-Request-Name
    request_header -X-Auth-Request-Token
    forward_auth gateway:8080 {
      uri /forward_auth
      copy_headers X-Auth-Request-User X-Auth-Request-Name X-Auth-Request-Token
    }
    reverse_proxy chess:8080 chess-replica:8080 {
      lb_policy round_robin
    }
  }
}
$share_origin {
  route {
    request_header -X-Auth-Request-User
    request_header -X-Auth-Request-Name
    request_header -X-Auth-Request-Token
    @shared path /g /guest-sessions /play/* /watch/* /static/*
    reverse_proxy @shared chess:8080 chess-replica:8080 {
      lb_policy round_robin
    }
    respond 404
  }
}
EOF
docker run -d --name "$caddy" --network "$network" \
  -p "127.0.0.1:$public_port:$public_port" \
  --mount "type=bind,src=$fixture_dir/Caddyfile,dst=/etc/caddy/Caddyfile,readonly" \
  caddy:2-alpine >/dev/null
for _ in {1..60}; do
  if docker exec "$caddy" caddy validate --config /etc/caddy/Caddyfile >/dev/null 2>&1; then break; fi
  sleep 0.2
done

cd "$chess_dir"
CHESS_LIVE_ORIGIN="$chess_origin" CHESS_LIVE_SHARE_ORIGIN="$share_origin" \
  CHESS_LIVE_AUTH_ORIGIN="$auth_origin" CHESS_LIVE_GATEWAY_LOG="$fixture_dir/gateway.log" \
  CHESS_LIVE_RESTART_CONTAINER="$chess_replica" \
  go test ./internal/app -run '^TestLiveGatewaySSOAndAnonymousGuest$' -count=1 -v

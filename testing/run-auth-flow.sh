#!/usr/bin/env bash
# Run the live auth/admin Playwright suite against a disposable Docker container.
#
# Defaults to provisioning a fresh container + database on http://localhost:8090,
# seeding an admin with a generated password, and leaving the container running
# so the run can be inspected or repeated with PROVISION=0.
#
#   ./run-auth-flow.sh                    # fresh container, full suite
#   ./run-auth-flow.sh --headed           # watch the browser
#   PROVISION=0 ADMIN_PASS=... ./run-auth-flow.sh
#   HOST_PORT=8091 ./run-auth-flow.sh
#   BUILD=1 ./run-auth-flow.sh            # rebuild the image first
#
# Environment:
#   PROVISION=1|0       1 provisions a fresh container (default 1)
#   BUILD=1             Force a docker image build
#   IMAGE               Image name (default tsa-auth-test:latest)
#   CONTAINER_NAME      Container name (default tsa-auth-test)
#   HOST_PORT           Host port for the container (default 8090)
#   TARGET_URL          App origin (default http://localhost:$HOST_PORT)
#   ADMIN_USER          Admin username (default admin)
#   ADMIN_PASS          Admin password. When provisioning, a random one is
#                       generated if unset. Required when PROVISION=0.
#   KEEP_CONTAINER=0    Remove the container when the script exits
#   RESET_VOLUMES=0     Keep existing DB/download volumes when provisioning
#   DATA_VOLUME         Default ${CONTAINER_NAME}-data
#   DOWNLOADS_VOLUME    Default ${CONTAINER_NAME}-downloads
#   ALLOW_SHARED_VOLUMES=1  Permit resetting volumes whose names do not contain
#                       CONTAINER_NAME (protects real app volumes)
set -euo pipefail

cd "$(dirname "$0")"
ROOT="$(cd .. && pwd)"

CONTAINER_NAME="${CONTAINER_NAME:-tsa-auth-test}"
IMAGE="${IMAGE:-tsa-auth-test:latest}"
HOST_PORT="${HOST_PORT:-8090}"
TARGET_URL="${TARGET_URL:-http://localhost:${HOST_PORT}}"
PROVISION="${PROVISION:-1}"
BUILD="${BUILD:-0}"
KEEP_CONTAINER="${KEEP_CONTAINER:-1}"
RESET_VOLUMES="${RESET_VOLUMES:-1}"
ADMIN_USER="${ADMIN_USER:-admin}"
DATA_VOLUME="${DATA_VOLUME:-${CONTAINER_NAME}-data}"
DOWNLOADS_VOLUME="${DOWNLOADS_VOLUME:-${CONTAINER_NAME}-downloads}"
GENERATED_ADMIN_PASS=0

log() { printf '%s\n' "$*"; }
fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

cleanup_container() {
  if [[ "$PROVISION" == "1" && "$KEEP_CONTAINER" != "1" ]]; then
    log "removing container $CONTAINER_NAME"
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  fi
}
trap cleanup_container EXIT

command -v docker >/dev/null 2>&1 || fail "docker is required"
command -v node >/dev/null 2>&1 || fail "node is required"
command -v curl >/dev/null 2>&1 || fail "curl is required"
docker info >/dev/null 2>&1 || fail "the docker daemon is not reachable"

wait_for_health() {
  log "waiting for ${TARGET_URL}/api/health"
  for _ in $(seq 1 90); do
    if curl -fsS --max-time 2 "${TARGET_URL}/api/health" >/dev/null 2>&1; then
      log "app is healthy"
      return 0
    fi
    sleep 2
  done
  docker logs --tail 60 "$CONTAINER_NAME" >&2 2>/dev/null || true
  fail "app at ${TARGET_URL} did not become healthy"
}

seed_admin() {
  if [[ -z "${ADMIN_PASS:-}" ]]; then
    ADMIN_PASS="$(openssl rand -hex 12)"
    GENERATED_ADMIN_PASS=1
  fi
  log "seeding admin '${ADMIN_USER}'"
  docker exec -i \
    -e "ADMIN_SEED_USER=${ADMIN_USER}" \
    -e "ADMIN_SEED_PASSWORD=${ADMIN_PASS}" \
    "$CONTAINER_NAME" \
    python - <<'PY'
import os

from app.auth.db import connect, now_iso
from app.auth.security import hash_password

username = os.environ["ADMIN_SEED_USER"]
password = os.environ["ADMIN_SEED_PASSWORD"]
stamp = now_iso()
conn = connect()
try:
    row = conn.execute("SELECT id FROM users WHERE username = ?", (username,)).fetchone()
    if row:
        conn.execute(
            "UPDATE users SET password_hash = ?, role = 'admin', status = 'active' WHERE id = ?",
            (hash_password(password), row["id"]),
        )
        print("updated existing admin")
    else:
        conn.execute(
            "INSERT INTO users (username, password_hash, role, status, created_at, approved_at)"
            " VALUES (?, ?, 'admin', 'active', ?, ?)",
            (username, hash_password(password), stamp, stamp),
        )
        print("created admin")
    conn.commit()
finally:
    conn.close()
PY
}

if [[ "$PROVISION" == "1" ]]; then
  if ! docker image inspect "$IMAGE" >/dev/null 2>&1 || [[ "$BUILD" == "1" ]]; then
    log "building image $IMAGE"
    docker build -t "$IMAGE" "$ROOT"
  fi

  case "${DATA_VOLUME} ${DOWNLOADS_VOLUME}" in
    *"$CONTAINER_NAME"*) ;;
    *)
      if [[ "${ALLOW_SHARED_VOLUMES:-0}" != "1" ]]; then
        fail "refusing to reset volumes that do not contain '$CONTAINER_NAME'" \
          "(DATA_VOLUME='$DATA_VOLUME', DOWNLOADS_VOLUME='$DOWNLOADS_VOLUME')." \
          "Set ALLOW_SHARED_VOLUMES=1 to override."
      fi
      ;;
  esac

  log "recreating container $CONTAINER_NAME"
  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  if [[ "$RESET_VOLUMES" == "1" ]]; then
    docker volume rm "$DATA_VOLUME" "$DOWNLOADS_VOLUME" >/dev/null 2>&1 || true
  fi

  docker run -d --name "$CONTAINER_NAME" \
    -p "${HOST_PORT}:8080" \
    -e TMDB_API_KEY=dummy \
    -e FORUM_PROBE_ENABLED=false \
    -v "${DATA_VOLUME}:/data" \
    -v "${DOWNLOADS_VOLUME}:/downloads" \
    "$IMAGE" >/dev/null

  wait_for_health
  seed_admin
  log "admin credentials for this run: ${ADMIN_USER} / ${ADMIN_PASS}"
else
  curl -fsS --max-time 3 "${TARGET_URL}/api/health" >/dev/null 2>&1 ||
    fail "no app reachable at ${TARGET_URL}; start it or leave PROVISION=1"
  [[ -n "${ADMIN_PASS:-}" ]] || fail "ADMIN_PASS is required when PROVISION=0"
fi

if [[ ! -d node_modules/playwright ]]; then
  log "installing test dependencies (npm install)"
  npm install --no-audit --no-fund
fi

log "running live auth suite"
set +e
TARGET_URL="$TARGET_URL" \
  ADMIN_USER="$ADMIN_USER" \
  ADMIN_PASS="$ADMIN_PASS" \
  node test-auth-flow.cjs "$@"
status=$?
set -e

if [[ "$GENERATED_ADMIN_PASS" == "1" ]]; then
  log "generated admin password: ${ADMIN_PASS}"
fi

exit "$status"

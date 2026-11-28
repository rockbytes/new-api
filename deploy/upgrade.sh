#!/usr/bin/env bash
# Rolling upgrade for the two-instance new-api deployment behind nginx.
#   CONFIRM_BACKWARD_COMPATIBLE=yes [EXPECTED_SHA256=...] [FORCE_RESTART=yes] [DRAIN_TIMEOUT_SECONDS=600] /opt/new-api/bin/upgrade.sh <version>
# <version> is a directory name under /opt/new-api/releases that contains new-api.
# Order: B (slave) first, then A (master). Each instance is taken out of nginx and
# drained before restart: it stops admitting WebSocket sessions, and in-flight
# requests and sessions finish. A drain that does not finish in time aborts the
# upgrade unless FORCE_RESTART=yes.
set -euo pipefail

VERSION="${1:-}"
ROOT=/opt/new-api
CONF=/etc/nginx/sites-available/new-api.conf
LOCK=/run/new-api-upgrade.lock
DRAIN_TIMEOUT_SECONDS="${DRAIN_TIMEOUT_SECONDS:-600}"
DRAIN_SOCK_A=/run/newapi-drain-a/drain.sock
DRAIN_SOCK_B=/run/newapi-drain-b/drain.sock

if [ "$#" -ne 1 ] || [[ -z "$VERSION" || "$VERSION" == -* ]]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi
if [ "$(id -u)" -ne 0 ]; then echo "must run as root" >&2; exit 2; fi

REL="$ROOT/releases/$VERSION/new-api"
LOG="/var/log/new-api-upgrade-$(date +%Y%m%d-%H%M%S)-$VERSION.log"
LAST_TS=""

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG"; }

health_ok() { curl -s -m 3 "http://127.0.0.1:$1/api/status" | grep -q '"success":true'; }

wait_healthy() {
  local port=$1
  for _ in $(seq 1 60); do
    health_ok "$port" && return 0
    sleep 1
  done
  return 1
}

# set_upstream <port> <up|down>
set_upstream() {
  local port=$1 state=$2
  if [ "$state" = down ]; then
    sed -i -E "s/server 127\.0\.0\.1:${port}( down)?;/server 127.0.0.1:${port} down;/" "$CONF"
  else
    sed -i -E "s/server 127\.0\.0\.1:${port}( down)?;/server 127.0.0.1:${port};/" "$CONF"
  fi
  nginx -t >/dev/null 2>&1 && systemctl reload nginx
}

# drain_status <socket>: prints the drain status JSON, fails if the endpoint is unreachable.
drain_status() { curl -sf -m 2 --unix-socket "$1" http://localhost/drain/status; }

# drained <socket>: no HTTP request and no WebSocket session is in flight.
drained() { drain_status "$1" | grep -q '"active_requests":0,"active_websockets":0'; }

# drain_instance <socket> <label>: refuse new WebSocket sessions, then wait for in-flight work.
drain_instance() {
  local sock=$1 label=$2 deadline
  if ! curl -sf -m 3 -X POST --unix-socket "$sock" http://localhost/drain >/dev/null; then
    log "$label: drain endpoint did not accept POST /drain"
    return 1
  fi
  deadline=$((SECONDS + DRAIN_TIMEOUT_SECONDS))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if drained "$sock"; then
      log "$label drained"
      return 0
    fi
    sleep 1
  done
  log "$label not drained after ${DRAIN_TIMEOUT_SECONDS}s; last status: $(drain_status "$sock" || echo unreachable)"
  return 1
}

# undrain <socket>: best effort. An instance that is not draining is unaffected.
undrain() { curl -sf -m 3 -X DELETE --unix-socket "$1" http://localhost/drain >/dev/null 2>&1 || true; }

# Put back in rotation every healthy instance, and clear its drain state first.
restore_traffic() {
  local port sock
  for pair in "3000 $DRAIN_SOCK_A" "3001 $DRAIN_SOCK_B"; do
    read -r port sock <<<"$pair"
    undrain "$sock"
    if health_ok "$port"; then set_upstream "$port" up; fi
  done
}

die() {
  log "FAILED: $*"
  restore_traffic
  log "upgrade aborted, traffic restored to healthy instances; log: $LOG"
  exit 1
}

trap 'log "unexpected error at line $LINENO"; restore_traffic' ERR

# restart_instance <unit> <port> <a|b>
restart_instance() {
  local unit=$1 port=$2 inst=$3 pid actual expected err
  LAST_TS=$(date '+%F %T')
  ln -sfn "$REL" "$ROOT/instances/$inst/new-api"
  systemctl restart "$unit"
  wait_healthy "$port" || die "$unit not healthy on :$port after restart"

  pid=$(systemctl show -p MainPID --value "$unit")
  actual=$(readlink -f "/proc/$pid/exe")
  expected=$(readlink -f "$REL")
  [ "$actual" = "$expected" ] || die "$unit runs $actual, expected $expected"

  err=$(journalctl -u "$unit" --no-pager --since "$LAST_TS" | grep -icE 'fatal|panic' || true)
  [ "$err" -eq 0 ] || die "$unit logged $err fatal/panic lines since restart"
  log "$unit restarted on $VERSION and healthy on :$port"
}

task_starts() {  # task_starts <unit>: background task starts since last restart
  journalctl -u "$1" --no-pager --since "$LAST_TS" | grep -c 'task started' || true
}

exec 9>"$LOCK"
flock -n 9 || { echo "another upgrade is already running" >&2; exit 1; }

log "upgrade to $VERSION started"

if [ "${CONFIRM_BACKWARD_COMPATIBLE:-}" != yes ]; then
  die "set CONFIRM_BACKWARD_COMPATIBLE=yes after confirming this release has no schema change that breaks the currently running version"
fi
[ -x "$REL" ] || die "binary missing or not executable: $REL"
if [ -n "${EXPECTED_SHA256:-}" ]; then
  actual_sha=$(sha256sum "$REL" | awk '{print toupper($1)}')
  [ "$actual_sha" = "$(echo "$EXPECTED_SHA256" | tr '[:lower:]' '[:upper:]')" ] || die "sha256 mismatch for $REL"
fi
nginx -t >/dev/null 2>&1 || die "nginx configuration is invalid"
health_ok 3000 || die "A (:3000) is not healthy before upgrade"
health_ok 3001 || die "B (:3001) is not healthy before upgrade"
# Without a drain endpoint the running build cannot be drained, so nothing is changed.
drain_status "$DRAIN_SOCK_A" >/dev/null 2>&1 || die "A drain endpoint not reachable at $DRAIN_SOCK_A (DRAIN_SOCKET not configured on the running build)"
drain_status "$DRAIN_SOCK_B" >/dev/null 2>&1 || die "B drain endpoint not reachable at $DRAIN_SOCK_B (DRAIN_SOCKET not configured on the running build)"
log "pre-checks passed"

log "phase 1: upgrade slave B (new-api-b, :3001)"
set_upstream 3001 down
if ! drain_instance "$DRAIN_SOCK_B" B; then
  [ "${FORCE_RESTART:-}" = yes ] || die "B did not drain; in-flight work is still running on B"
  log "FORCE_RESTART=yes: restarting B with in-flight work still running"
fi
restart_instance new-api-b 3001 b
[ "$(task_starts new-api-b)" -eq 0 ] || die "slave B started background tasks; NODE_TYPE=slave is not in effect"
set_upstream 3001 up
health_ok 3000 || die "A (:3000) unhealthy after phase 1"
log "phase 1 complete"

log "phase 2: upgrade master A (new-api-a, :3000); no master runs during this restart"
set_upstream 3000 down
if ! drain_instance "$DRAIN_SOCK_A" A; then
  [ "${FORCE_RESTART:-}" = yes ] || die "A did not drain; in-flight work is still running on A"
  log "FORCE_RESTART=yes: restarting A with in-flight work still running"
fi
restart_instance new-api-a 3000 a
[ "$(task_starts new-api-a)" -gt 0 ] || die "master A started no background tasks"
set_upstream 3000 up
log "phase 2 complete"

health_ok 3000 && health_ok 3001 || die "final health check failed"
log "upgrade complete: both instances run $VERSION"

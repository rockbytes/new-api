#!/usr/bin/env bash
# Rolling upgrade for the two-instance new-api deployment behind nginx.
#   CONFIRM_BACKWARD_COMPATIBLE=yes [EXPECTED_SHA256=...] [FORCE_RESTART=yes] [DRAIN_TIMEOUT_SECONDS=600] /opt/new-api/bin/upgrade.sh <version>
# <version> is a directory name under /opt/new-api/releases that contains new-api.
# Order: B (slave) first, then A (master). Each instance is taken out of nginx and
# drained before restart: it stops admitting WebSocket sessions, and in-flight
# requests and sessions finish. A drain that does not finish in time aborts the
# upgrade unless FORCE_RESTART=yes.
# For the first durable billing release, see doc/done/personal-key-usage.md.
#   EXPECTED_SHA256=... upgrade.sh --billing-preflight <version>
#   CONFIRM_BILLING_INGRESS_FENCED=yes EXPECTED_SHA256=... upgrade.sh --billing-maintenance <version>
set -euo pipefail

MODE=rolling
case "${1:-}" in
  --billing-preflight|--billing-maintenance) MODE=$1; shift ;;
esac
VERSION="${1:-}"
ROOT=/opt/new-api
CONF=/etc/nginx/sites-available/new-api.conf
LOCK=/run/new-api-upgrade.lock
BILLING_TOKEN=/run/new-api-billing-maintenance
DRAIN_TIMEOUT_SECONDS="${DRAIN_TIMEOUT_SECONDS:-600}"
DRAIN_SOCK_A=/run/newapi-drain-a/drain.sock
DRAIN_SOCK_B=/run/newapi-drain-b/drain.sock

if [ -z "$VERSION" ] || { [ "$MODE" != rolling ] && { [ "$#" -ne 1 ] || [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; }; }; then
  echo "usage: $0 <version> | $0 [--billing-preflight|--billing-maintenance] vX.Y.Z" >&2
  exit 2
fi
if [ "$(id -u)" -ne 0 ]; then echo "must run as root" >&2; exit 2; fi

REL="$ROOT/releases/$VERSION/new-api"
BILLING_STAGE="$REL-billing-maintenance"
LOG="/var/log/new-api-upgrade-$(date +%Y%m%d-%H%M%S)-$VERSION.log"
LAST_TS=""

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG"; }

preflight_fail() { log "PREFLIGHT FAILED: $* (no services or traffic changed)"; exit 1; }

health_ok() { curl -s -m 3 "http://127.0.0.1:$1/api/status" | grep -q '"success":true'; }

wait_healthy() {
  local port=$1 attempts=${2:-60}
  for _ in $(seq 1 "$attempts"); do
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
  if [ "$MODE" = rolling ]; then
    restore_traffic
    log "upgrade aborted, traffic restored to healthy instances; log: $LOG"
  fi
  exit 1
}

# start_release <start|restart> <unit> <port> <binary> [health-check attempts]
start_release() {
  local action=$1 unit=$2 port=$3 binary=$4 attempts=${5:-60} pid actual expected err
  LAST_TS=$(date '+%F %T')
  systemctl "$action" "$unit"
  wait_healthy "$port" "$attempts" || die "$unit not healthy on :$port after $action"

  pid=$(systemctl show -p MainPID --value "$unit")
  actual=$(readlink -f "/proc/$pid/exe")
  expected=$(readlink -f "$binary")
  [ "$actual" = "$expected" ] || die "$unit runs $actual, expected $expected"

  err=$(journalctl -u "$unit" --no-pager --since "$LAST_TS" | grep -icE 'fatal|panic' || true)
  [ "$err" -eq 0 ] || die "$unit logged $err fatal/panic lines since $action"
  log "$unit $action complete on $VERSION and healthy on :$port"
}

restart_instance() {
  local unit=$1 port=$2 inst=$3
  ln -sfn "$REL" "$ROOT/instances/$inst/new-api"
  start_release restart "$unit" "$port" "$REL"
}

task_starts() {  # task_starts <unit>: background task starts since last restart
  journalctl -u "$1" --no-pager --since "$LAST_TS" | grep -c 'task started' || true
}

# Verify the running process's NODE_TYPE, not defaults or localized task logs.
billing_role_matches() {
  local unit=$1 role=$2 pid
  pid=$(systemctl show -p MainPID --value "$unit") || return 1
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
  if grep -aFxz 'NODE_TYPE=slave' "/proc/$pid/environ" >/dev/null 2>&1; then
    [ "$role" = slave ]
  else
    [ "$?" -eq 1 ] && [ "$role" = master ]
  fi
}

exec 9>"$LOCK"
flock -n 9 || { echo "another upgrade is already running" >&2; exit 1; }

# The staged name blocks even legacy rolling scripts until the cutover succeeds.
if [ -e "$BILLING_STAGE" ] || [ -L "$BILLING_STAGE" ]; then
  if [ "$MODE" = rolling ] && [ "$(readlink "$REL" || true)" != "${BILLING_STAGE##*/}" ]; then
    preflight_fail "this release requires --billing-preflight and --billing-maintenance"
  fi
fi
if [ -e "$BILLING_TOKEN" ] || [ -L "$BILLING_TOKEN" ]; then
  preflight_fail "maintenance token already exists: $BILLING_TOKEN; inspect the previous cutover before proceeding"
fi

if [ "$MODE" != rolling ]; then
  [ ! -e "$REL" ] && [ ! -L "$REL" ] || preflight_fail "canonical release path already exists: $REL"
  [ -f "$BILLING_STAGE" ] && [ -x "$BILLING_STAGE" ] && [ ! -L "$BILLING_STAGE" ] || preflight_fail "stage the binary only at $BILLING_STAGE"
  [ "$(readlink -f "$BILLING_STAGE")" = "$BILLING_STAGE" ] || preflight_fail "the staged release must be managed directly under $ROOT/releases"
  [[ "${EXPECTED_SHA256:-}" =~ ^[[:xdigit:]]{64}$ ]] || preflight_fail "set EXPECTED_SHA256 to the verified release checksum"
  [[ "$DRAIN_TIMEOUT_SECONDS" =~ ^[1-9][0-9]{0,5}$ ]] || preflight_fail "DRAIN_TIMEOUT_SECONDS must be a positive integer of up to six digits"
  [ "${FORCE_RESTART:-}" != yes ] || preflight_fail "FORCE_RESTART cannot be used during billing maintenance"
  if [ "$MODE" = --billing-maintenance ]; then
    [ "${CONFIRM_BILLING_INGRESS_FENCED:-}" = yes ] || preflight_fail "fence public ingress and direct clients, and stop other writers/cleanup jobs before setting CONFIRM_BILLING_INGRESS_FENCED=yes"
  fi
  actual_sha=$(sha256sum "$BILLING_STAGE") || preflight_fail "cannot checksum $BILLING_STAGE"
  [ "${actual_sha%% *}" = "${EXPECTED_SHA256,,}" ] || preflight_fail "sha256 mismatch for $BILLING_STAGE"
  nginx -t >/dev/null 2>&1 || preflight_fail "nginx configuration is invalid"
  for inst in a b; do
    unit="new-api-$inst"
    [ "$(systemctl show -p ActiveState --value "$unit")" = active ] || preflight_fail "$unit is not active"
    [ "$(systemctl show -p KillMode --value "$unit")" = control-group ] || preflight_fail "$unit must use KillMode=control-group"
    [ -L "$ROOT/instances/$inst/new-api" ] || preflight_fail "$unit launcher is not a release symlink"
  done
  billing_role_matches new-api-a master || preflight_fail "cannot verify master role for new-api-a"
  billing_role_matches new-api-b slave || preflight_fail "cannot verify slave role for new-api-b"
  health_ok 3000 && health_ok 3001 || preflight_fail "both instances must be healthy"
  for sock in "$DRAIN_SOCK_A" "$DRAIN_SOCK_B"; do
    status=$(drain_status "$sock") || preflight_fail "drain endpoint not reachable at $sock"
    [[ "$status" == *'"state":"running"'* ]] || preflight_fail "instance at $sock is already draining"
  done
  log "billing maintenance preflight passed for $VERSION"
  [ "$MODE" != --billing-preflight ] || exit 0

  # The ingress fence is operator-owned; do not touch nginx or clear drains
  # on failure. The stopped units/launcher links are durable recovery facts.
  billing_step=drain
  token_tmp=
  trap 'rc=$?; if [ -n "$token_tmp" ]; then
      if [ "$token_tmp" -ef "$BILLING_TOKEN" ]; then rm -f -- "$BILLING_TOKEN" || rc=1; fi;
      rm -f -- "$token_tmp" || rc=1;
    fi;
    if [ "$rc" -ne 0 ]; then log "billing maintenance failed during $billing_step; keep ingress fenced and follow the runbook; log: $LOG"; fi;
    exit "$rc"' EXIT
  trap 'exit 1' HUP INT TERM

  log "billing maintenance: draining both old instances (ingress must already be fenced)"
  drain_instance "$DRAIN_SOCK_A" A || die "A did not drain; neither instance was stopped"
  drain_instance "$DRAIN_SOCK_B" B || die "B did not drain; neither instance was stopped"

  billing_step=stop
  log "billing maintenance: stopping both old instances before authorizing initialization"
  systemctl stop new-api-a new-api-b
  for unit in new-api-a new-api-b; do
    [ "$(systemctl show -p ActiveState --value "$unit")" = inactive ] &&
      [ "$(systemctl show -p MainPID --value "$unit")" = 0 ] &&
      [ "$(systemctl show -p Result --value "$unit")" = success ] || die "$unit did not stop cleanly"
  done

  billing_step=primary-start
  token_tmp=$(mktemp "$BILLING_TOKEN.XXXXXX")
  printf '%s\n' "$VERSION" >"$token_tmp"
  chmod 0644 "$token_tmp"
  ln -T "$token_tmp" "$BILLING_TOKEN"
  ln -sfn "$BILLING_STAGE" "$ROOT/instances/a/new-api"
  ln -sfn "$BILLING_STAGE" "$ROOT/instances/b/new-api"
  # /api/status becomes available only after the primary's synchronous backfill.
  # Keep the regular upgrade's short health timeout unchanged.
  start_release start new-api-a 3000 "$BILLING_STAGE" 600
  billing_role_matches new-api-a master || die "cannot verify master role for new-api-a"
  rm -- "$BILLING_TOKEN"

  billing_step=slave-start
  start_release start new-api-b 3001 "$BILLING_STAGE"
  billing_role_matches new-api-b slave || die "cannot verify slave role for new-api-b"
  health_ok 3000 && health_ok 3001 || die "final health check failed"

  billing_step=publish
  ln -sT "${BILLING_STAGE##*/}" "$REL"
  log "billing maintenance complete: both instances run $VERSION; verify before removing the ingress fence"
  exit 0
fi

trap 'log "unexpected error at line $LINENO"; restore_traffic' ERR
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

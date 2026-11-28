#!/usr/bin/env bash
# Isolated script tests: run with bash deploy/upgrade_test.sh on Linux.
set -euo pipefail

TEST_DIR=$(mktemp -d)
trap 'rc=$?; if [ "$rc" -ne 0 ] && [ -f "$TEST_DIR/output" ]; then cat "$TEST_DIR/output" >&2; fi; rm -rf "$TEST_DIR"' EXIT
SCRIPT=$(cd "$(dirname "$0")" && pwd)/upgrade.sh
export FIXTURE="$TEST_DIR/fixture" VERSION=v1.2.3
export PATH="$TEST_DIR/bin:$PATH"
mkdir -p "$TEST_DIR/bin"

# Redirect the production paths only in the test copy. No host services are used.
sed -e "s|/opt/new-api|$FIXTURE/root|g" \
  -e "s|/etc/nginx/sites-available/new-api.conf|$FIXTURE/nginx.conf|g" \
  -e "s|/proc/|$FIXTURE/proc/|g" \
  -e "s|/run/|$FIXTURE/|g" \
  -e "s|/var/log/|$FIXTURE/|g" "$SCRIPT" > "$TEST_DIR/upgrade.sh"

cat > "$TEST_DIR/bin/stub" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
root="$FIXTURE/root"
release="$root/releases/$VERSION"
events="$FIXTURE/events"
token="$FIXTURE/new-api-billing-maintenance"
case ${0##*/} in
  id) printf '0\n' ;;
  nginx) ;;
  sleep) ;;
  journalctl) if [[ " $* " = *' -u new-api-a '* ]]; then printf 'task started\n'; fi ;;
  curl)
    if [[ "$*" = *':3000/'* || "$*" = *'-a/drain.sock'* ]]; then inst=a; else inst=b; fi
    [[ -f "$FIXTURE/$inst-active" ]] || exit 1
    if [[ "$*" = *'POST '* ]]; then
      printf 'drain %s\n' "$inst" >> "$events"
      [[ ${FAIL_DRAIN:-} != "$inst" ]] || exit 1
      touch "$FIXTURE/$inst-draining"
    elif [[ "$*" = *'DELETE '* ]]; then
      printf 'undrain %s\n' "$inst" >> "$events"
      rm -f "$FIXTURE/$inst-draining"
    elif [[ "$*" = *'/drain/status'* ]]; then
      state=running
      if [[ -f "$FIXTURE/$inst-draining" ]]; then state=draining; fi
      printf '{"state":"%s","active_requests":0,"active_websockets":0}\n' "$state"
    else
      printf '{"success":true}\n'
    fi ;;
  systemctl)
    action=$1
    inst=${*: -1}
    inst=${inst##*-}
    case "$inst" in a) pid=10;; b) pid=11;; *) pid=0;; esac
    case "$action" in
      show)
        case "$*" in
          *ActiveState*) if [[ -f "$FIXTURE/$inst-active" ]]; then printf 'active\n'; else printf 'inactive\n'; fi ;;
          *MainPID*) if [[ -f "$FIXTURE/$inst-active" ]]; then printf '%s\n' "$pid"; else printf '0\n'; fi ;;
          *KillMode*) printf 'control-group\n' ;;
          *Result*) printf 'success\n' ;;
          *) exit 1 ;;
        esac ;;
      stop)
        [[ ! -e "$token" && ! -e "$release/new-api" ]]
        for inst in a b; do
          printf 'stop %s\n' "$inst" >> "$events"
          [[ -f "$FIXTURE/$inst-draining" && ${FAIL_STOP:-} != "$inst" ]] || exit 1
          rm "$FIXTURE/$inst-active"
        done ;;
      start)
        printf 'start %s\n' "$inst" >> "$events"
        [[ ! -e "$release/new-api" ]]
        [[ $(readlink "$root/instances/a/new-api") == "$release/new-api-billing-maintenance" ]]
        [[ $(readlink "$root/instances/b/new-api") == "$release/new-api-billing-maintenance" ]]
        if [[ "$inst" == a ]]; then
          [[ ! -e "$FIXTURE/a-active" && ! -e "$FIXTURE/b-active" ]]
          [[ $(<"$token") == "$VERSION" && $(stat -c '%a' "$token") == 644 ]]
        else
          [[ -f "$FIXTURE/a-active" && ! -e "$FIXTURE/b-active" && ! -e "$token" ]]
        fi
        [[ ${FAIL_START:-} != "$inst" ]] || exit 1
        touch "$FIXTURE/$inst-active"
        ln -sfn "$(readlink "$root/instances/$inst/new-api")" "$FIXTURE/proc/$pid/exe" ;;
      restart)
        printf 'restart %s\n' "$inst" >> "$events"
        ln -sfn "$(readlink "$root/instances/$inst/new-api")" "$FIXTURE/proc/$pid/exe" ;;
      reload) printf 'reload\n' >> "$events" ;;
      *) exit 1 ;;
    esac ;;
  *) exit 1 ;;
esac
STUB
chmod +x "$TEST_DIR/bin/stub"
for name in id nginx sleep journalctl curl systemctl; do
  ln -s stub "$TEST_DIR/bin/$name"
done

setup() {
  rm -rf "$FIXTURE"
  release="$FIXTURE/root/releases/$VERSION"
  old="$FIXTURE/root/releases/v1.2.2/new-api"
  mkdir -p "$release" "$(dirname "$old")" "$FIXTURE/root/instances/"{a,b} "$FIXTURE/proc/"{10,11}
  touch "$old" "$FIXTURE/"{a-active,b-active,events}
  ln -s "$old" "$FIXTURE/root/instances/a/new-api"
  ln -s "$old" "$FIXTURE/root/instances/b/new-api"
  ln -s "$old" "$FIXTURE/proc/10/exe"
  ln -s "$old" "$FIXTURE/proc/11/exe"
  printf 'PORT=3000\0' > "$FIXTURE/proc/10/environ"
  printf 'PORT=3001\0NODE_TYPE=slave\0' > "$FIXTURE/proc/11/environ"
  printf 'server 127.0.0.1:3000;\nserver 127.0.0.1:3001;\n' > "$FIXTURE/nginx.conf"
  printf 'test binary\n' > "$release/new-api-billing-maintenance"
  chmod +x "$release/new-api-billing-maintenance"
  export EXPECTED_SHA256
  EXPECTED_SHA256=$(sha256sum "$release/new-api-billing-maintenance")
  EXPECTED_SHA256=${EXPECTED_SHA256%% *}
}

upgrade() { bash "$TEST_DIR/upgrade.sh" "$@" > "$TEST_DIR/output" 2>&1; }
maintenance() { CONFIRM_BILLING_INGRESS_FENCED=yes upgrade --billing-maintenance "$VERSION"; }
should_fail() {
  if "$@"; then printf 'unexpected success: %s\n' "$*" >&2; exit 1; fi
}
expect_events() { diff -u <(printf '%s' "$1") "$FIXTURE/events"; }

setup
upgrade --billing-preflight "$VERSION"
expect_events ''
should_fail upgrade --billing-maintenance "$VERSION"
CONFIRM_BACKWARD_COMPATIBLE=yes should_fail upgrade "$VERSION"
EXPECTED_SHA256=$(printf '%064d' 0) should_fail maintenance
FORCE_RESTART=yes should_fail maintenance
cp "$release/new-api-billing-maintenance" "$release/new-api"
CONFIRM_BACKWARD_COMPATIBLE=yes should_fail upgrade "$VERSION"
should_fail maintenance
expect_events ''

setup
printf '%s\n' "$VERSION" > "$FIXTURE/new-api-billing-maintenance"
should_fail maintenance
expect_events ''
[[ $(<"$FIXTURE/new-api-billing-maintenance") == "$VERSION" ]]

setup
printf 'PORT=3001\0' > "$FIXTURE/proc/11/environ"
should_fail maintenance
expect_events ''

setup
FAIL_DRAIN=b should_fail maintenance
expect_events $'drain a\ndrain b\n'
[[ ! -e "$release/new-api" && ! -e "$FIXTURE/new-api-billing-maintenance" ]]

setup
FAIL_STOP=b should_fail maintenance
expect_events $'drain a\ndrain b\nstop a\nstop b\n'
[[ ! -e "$release/new-api" && ! -e "$FIXTURE/new-api-billing-maintenance" ]]

setup
FAIL_START=a should_fail maintenance
expect_events $'drain a\ndrain b\nstop a\nstop b\nstart a\n'
[[ ! -e "$release/new-api" && ! -e "$FIXTURE/new-api-billing-maintenance" ]]

setup
FAIL_START=b should_fail maintenance
expect_events $'drain a\ndrain b\nstop a\nstop b\nstart a\nstart b\n'
[[ ! -e "$release/new-api" && -x "$release/new-api-billing-maintenance" && ! -e "$FIXTURE/new-api-billing-maintenance" ]]
CONFIRM_BACKWARD_COMPATIBLE=yes should_fail upgrade "$VERSION"
expect_events $'drain a\ndrain b\nstop a\nstop b\nstart a\nstart b\n'

setup
maintenance
expect_events $'drain a\ndrain b\nstop a\nstop b\nstart a\nstart b\n'
[[ $(readlink "$release/new-api") == new-api-billing-maintenance && -x "$release/new-api" && ! -e "$FIXTURE/new-api-billing-maintenance" ]]

setup
mv "$release/new-api-billing-maintenance" "$release/new-api"
CONFIRM_BACKWARD_COMPATIBLE=yes upgrade "$VERSION"
expect_events $'reload\ndrain b\nrestart b\nreload\nreload\ndrain a\nrestart a\nreload\n'
printf 'upgrade tests passed\n'

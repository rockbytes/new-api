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
events="$FIXTURE/events"
conf="$FIXTURE/nginx.conf"
case ${0##*/} in
  id) printf '0\n' ;;
  nginx) ;;
  sleep) ;;
  journalctl) if [[ " $* " = *' -u new-api-a '* ]]; then printf 'task started\n'; fi ;;
  curl)
    if [[ "$*" = *':3000/'* || "$*" = *'-a/drain.sock'* ]]; then inst=a; else inst=b; fi
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
    case "$*" in
      'show -p MainPID --value new-api-a') printf '10\n' ;;
      'show -p MainPID --value new-api-b') printf '11\n' ;;
      'restart new-api-a'|'restart new-api-b')
        inst=${2##*-}
        if [[ "$inst" == a ]]; then pid=10; else pid=11; fi
        printf 'restart %s\n' "$inst" >> "$events"
        [[ -f "$FIXTURE/$inst-draining" ]]
        ln -sfn "$(readlink "$root/instances/$inst/new-api")" "$FIXTURE/proc/$pid/exe"
        rm "$FIXTURE/$inst-draining" ;;
      'reload nginx')
        a=up b=up
        if grep -q '127.0.0.1:3000 down;' "$conf"; then a=down; fi
        if grep -q '127.0.0.1:3001 down;' "$conf"; then b=down; fi
        printf 'reload %s %s\n' "$a" "$b" >> "$events" ;;
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
  release="$FIXTURE/root/releases/$VERSION/new-api"
  old="$FIXTURE/root/releases/v1.2.2/new-api"
  mkdir -p "$(dirname "$release")" "$(dirname "$old")" "$FIXTURE/root/instances/"{a,b} "$FIXTURE/proc/"{10,11}
  touch "$old" "$FIXTURE/events"
  ln -s "$old" "$FIXTURE/root/instances/a/new-api"
  ln -s "$old" "$FIXTURE/root/instances/b/new-api"
  ln -s "$old" "$FIXTURE/proc/10/exe"
  ln -s "$old" "$FIXTURE/proc/11/exe"
  printf 'server 127.0.0.1:3000;\nserver 127.0.0.1:3001;\n' > "$FIXTURE/nginx.conf"
  printf 'test binary\n' > "$release"
  chmod +x "$release"
  export EXPECTED_SHA256 CONFIRM_BACKWARD_COMPATIBLE=yes FORCE_RESTART=no DRAIN_TIMEOUT_SECONDS=600
  EXPECTED_SHA256=$(sha256sum "$release")
  EXPECTED_SHA256=${EXPECTED_SHA256%% *}
}

upgrade() { bash "$TEST_DIR/upgrade.sh" "$@" > "$TEST_DIR/output" 2>&1; }
should_fail() {
  if "$@"; then printf 'unexpected success: %s\n' "$*" >&2; exit 1; fi
}
expect_events() { diff -u <(printf '%s' "$1") "$FIXTURE/events"; }

# Retired maintenance commands are not accepted as version arguments.
setup
should_fail upgrade --billing-maintenance "$VERSION"
grep -q '^usage: .* <version>$' "$TEST_DIR/output"
expect_events ''

# B serves the new release before A is drained; only one upstream is out at a time.
setup
upgrade "$VERSION"
expect_events $'reload up down\ndrain b\nrestart b\nreload up up\nreload down up\ndrain a\nrestart a\nreload up up\n'
[[ $(readlink "$FIXTURE/proc/10/exe") == "$release" && $(readlink "$FIXTURE/proc/11/exe") == "$release" ]]

# A drain failure leaves A on the old release and puts both healthy nodes back in rotation.
setup
FAIL_DRAIN=a should_fail upgrade "$VERSION"
expect_events $'reload up down\ndrain b\nrestart b\nreload up up\nreload down up\ndrain a\nundrain a\nreload up up\nundrain b\nreload up up\n'
[[ $(readlink "$FIXTURE/root/instances/a/new-api") == "$old" && $(readlink "$FIXTURE/proc/11/exe") == "$release" ]]
printf 'upgrade tests passed\n'

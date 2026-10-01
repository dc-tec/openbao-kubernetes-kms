# Helpers shared by every generated node-setup.sh. init writes the values of one
# generation above this block; nothing here depends on the configuration.

fail() {
  status=$1
  shift
  printf 'node-setup: %s\n' "$*" >&2
  exit "$status"
}

say() { printf '%s\n' "$*"; }

require_root() { [ "$(id -u)" -eq 0 ] || fail 3 'run this phase as root'; }

numeric_uid() { case $1 in *[!0-9]*) id -u "$1" ;; *) printf '%s\n' "$1" ;; esac; }

numeric_gid() { case $1 in *[!0-9]*) getent group "$1" | cut -d: -f3 ;; *) printf '%s\n' "$1" ;; esac; }

require_tools() {
  missing=''
  for tool in "$@"; do
    command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
  done
  [ -z "$missing" ] || fail 3 "missing host tools:$missing"
}

# ensure_dir PATH OWNER GROUP MODE [strict]: create a missing directory, or
# check an existing one. Existing directories must not be group- or
# world-writable; strict ones must match owner, group, and mode exactly.
ensure_dir() {
  if [ -L "$1" ] || { [ -e "$1" ] && [ ! -d "$1" ]; }; then
    fail 3 "$1 exists and is not a directory"
  fi
  if [ ! -d "$1" ]; then
    mkdir -p -- "$(dirname -- "$1")"
    install -d -o "$2" -g "$3" -m "$4" -- "$1"
    say "created $1 ($2:$3 $4)"
    return
  fi
  perms=$(stat -c '%a' -- "$1")
  case $perms in *[2367]? | *[2367]) fail 3 "$1 is group- or world-writable ($perms)" ;; esac
  if [ "${5:-}" = strict ]; then
    want="$(numeric_uid "$2"):$(numeric_gid "$3"):$4"
    actual=$(stat -c '%u:%g:%a' -- "$1")
    [ "$actual" = "$want" ] || fail 3 "$1 is $actual, expected $want"
  fi
  say "ok $1"
}

# install_new SOURCE TARGET OWNER GROUP MODE: install a file on a fresh node,
# never replace one.
install_new() {
  { [ -f "$1" ] && [ ! -L "$1" ]; } || fail 2 "$1 is not a regular file"
  [ -d "$(dirname -- "$2")" ] || fail 3 "$(dirname -- "$2") is missing; run prepare first"
  if [ -e "$2" ] || [ -L "$2" ]; then
    fail 3 "$2 already exists; this script sets up fresh nodes only, see Operate: Upgrade"
  fi
  install -o "$3" -g "$4" -m "$5" -- "$1" "$2"
  say "installed $2 ($3:$4 $5)"
}

# check_identity runs config as the runtime identity and requires the
# fingerprint of this generation.
check_identity() {
  [ -f "$config_target" ] || fail 3 "$config_target is missing; run install first"
  summary=$(as_runtime "$binary" config --config "$config_target") ||
    fail 4 'config failed as the runtime identity'
  printf '%s\n' "$summary" | grep -qxF "identityFingerprint: $fingerprint" ||
    fail 3 "$config_target does not match this generation ($fingerprint)"
}

run_checks() {
  check_identity
  printf '%s\n' "$summary"
  as_runtime "$binary" verify-key --config "$config_target" || fail 4 'verify-key failed'
  as_runtime "$binary" doctor --config "$config_target" || fail 4 'doctor failed'
  (umask 077 && printf '%s %s\n' "$fingerprint" "$(date +%s)" > "$marker")
  say "check passed for $fingerprint; review any [warn] lines above"
}

# require_recent_check binds start to a passed check of the same identity.
require_recent_check() {
  [ -f "$marker" ] || fail 3 'run check before start'
  read -r checked_fingerprint checked_at < "$marker"
  [ "$checked_fingerprint" = "$fingerprint" ] ||
    fail 3 'the last check was for another identity; run check again'
  [ $(($(date +%s) - checked_at)) -le "$check_max_age" ] ||
    fail 3 'the last check is older than one hour; run check again'
  check_identity
}

wait_ready() {
  attempt=0
  until curl -fsS -o /dev/null "$ready_url"; do
    attempt=$((attempt + 1))
    [ "$attempt" -lt "$ready_attempts" ] || fail 1 'the provider did not become ready; check its logs'
    sleep 2
  done
  say "provider ready at $ready_url"
}

usage() {
  say "node-setup.sh for $model, identity $fingerprint"
  say 'Phases, run as root in order:'
  say '  prepare   check host tools and the package or kit, create directories'
  say "  install   $install_usage"
  say '  check     run config, verify-key, and doctor as the runtime identity'
  say '  start     start the provider and wait for /ready'
}


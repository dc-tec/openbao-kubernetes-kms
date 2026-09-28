#!/bin/sh
# Run on a Linux control-plane host after the local API server and provider start.
set -eu
test "$#" -le 1 || { echo 'usage: probe-apiserver [absolute-socket-path]' >&2; exit 2; }
socket=${1:-/run/openbao-kms/kms.sock}
test "$(id -u)" = 0 || { echo 'run as root to select the API server identity' >&2; exit 1; }
pid=$(pgrep -x kube-apiserver || true)
case "$pid" in ''|*[!0-9]*) echo 'expected exactly one local kube-apiserver process' >&2; exit 1 ;; esac
status_file="/proc/$pid/status"
client_uid=$(awk '/^Uid:/ {print $3}' "$status_file")
client_gid=$(awk '/^Gid:/ {print $3}' "$status_file")
client_groups=$(awk '/^Groups:/ {for (i=2; i<=NF; i++) printf "%s%s", (i==2 ? "" : ","), $i}' "$status_file")
case "$client_uid:$client_gid" in *[!0-9:]*|:|:*) echo 'cannot read API server identity' >&2; exit 1 ;; esac
test -n "$client_uid" && test -n "$client_gid"
set -- setpriv "--reuid=$client_uid" "--regid=$client_gid"
if [ -n "$client_groups" ]; then
  set -- "$@" "--groups=$client_groups"
else
  set -- "$@" --clear-groups
fi
exec "$@" bao-kms-provider probe --socket "$socket" --output json

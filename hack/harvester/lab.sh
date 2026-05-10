#!/bin/sh
set -eu

root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$root"

GOFLAGS="${GOFLAGS:--mod=vendor}"
export GOFLAGS

exec go run ./hack/tools/harvester_lab lab "$@"

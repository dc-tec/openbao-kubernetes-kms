#!/bin/bash
# Run only through make systemd-install-check in a disposable Linux container.
set -euo pipefail

if [[ "${KMS_INSTALL_TEST_CONTAINER:-}" != 1 || "$(id -u)" != 0 ]]; then
  echo 'run through make systemd-install-check; this test writes system paths' >&2
  exit 1
fi

repo=/src
work=$(mktemp -d)
export GOCACHE="$work/go-cache"
# This disposable fixture needs no Git metadata. The root-run container may
# mount a checkout owned by another user or a worktree with external metadata.
export GOFLAGS="-mod=vendor -buildvcs=false"
export GOTOOLCHAIN=local
ARCH=$(go env GOARCH)
export ARCH

extract_commands() {
  local file=$1 marker=$2
  awk -v marker="<!-- $marker -->" '
    $0 == marker { found = 1; next }
    found && $0 == "```sh" { code = 1; next }
    code && $0 == "```" { done = 1; exit }
    code { print }
    END { if (!done) exit 1 }
  ' "$file"
}

extract_commands "$repo/docs/getting-started/install.md" systemd-bundle-install > "$work/install.sh"
extract_commands "$repo/deploy/package/bundles/systemd/README.md" systemd-bundle-install > "$work/readme-install.sh"
cmp "$work/install.sh" "$work/readme-install.sh"
extract_commands "$repo/docs/getting-started/install.md" systemd-bundle-extract > "$work/extract.sh"
extract_commands "$repo/docs/getting-started/install.md" systemd-runtime-files > "$work/runtime.sh"

if [[ -n "${BUNDLE_ARCHIVE:-}" ]]; then
  archive=$(basename "$BUNDLE_ARCHIVE")
  VERSION=${archive#bao-kms-provider_}
  VERSION=${VERSION%_systemd_linux_${ARCH}.tar.gz}
  cp "$BUNDLE_ARCHIVE" "$work/$archive"
else
  VERSION=0.0.0-install-check
  archive="bao-kms-provider_${VERSION}_systemd_linux_${ARCH}.tar.gz"
  cd "$repo"
  CGO_ENABLED=0 go build -o "$work/bao-kms-provider" ./cmd/bao-kms-provider
  go run ./hack/tools/release_bundle -kind systemd \
    -output "$work/$archive" -prefix "${archive%.tar.gz}" \
    -binary "$work/bao-kms-provider"
fi
export VERSION
cd "$work"
# Source the documented extraction so its cd also selects the installation directory.
source "$work/extract.sh"
bash "$work/install.sh"
if [[ "${APPLY_PREVIEW_FIX:-false}" == true ]]; then
  extract_commands "$repo/docs/getting-started/install.md" systemd-preview-permissions > "$work/permissions.sh"
  bash "$work/permissions.sh"
fi

# Run the documented initial file placement with non-secret fixtures.
cp config/provider-systemd.yaml provider.yaml
printf 'test CA fixture\n' > ca.crt
printf 'test JWT fixture\n' > identity.jwt
bash "$work/runtime.sh"
systemd-analyze verify /usr/lib/systemd/system/bao-kms-provider.service

# Exercise actual filesystem access as the service identity, not as root.
runuser -u openbao-kms -- bao-kms-provider config --config /etc/openbao-kms/config.yaml > /dev/null
runuser -u openbao-kms -- sh -eu -c '
  test -r /etc/openbao-kms/tls/ca.crt
  test -r /var/lib/openbao-kms/identity.jwt
  test ! -w /etc/openbao-kms/config.yaml
  touch /var/lib/openbao-kms/state/install-check
  touch /run/openbao-kms/install-check
'
test "$(stat -c '%a:%U:%G' /etc/openbao-kms)" = '750:root:openbao-kms'
test "$(stat -c '%a:%U:%G' /run/openbao-kms)" = '2750:openbao-kms:openbao-kms-socket'
test "$(stat -c '%G' /run/openbao-kms/install-check)" = openbao-kms-socket

# Socket-group membership must not grant access to config, JWT, or state.
useradd --no-create-home --gid openbao-kms-socket kms-socket-test
runuser -u kms-socket-test -- sh -eu -c '
  test -x /run/openbao-kms
  test ! -w /run/openbao-kms
  test ! -r /etc/openbao-kms/config.yaml
  test ! -r /var/lib/openbao-kms/identity.jwt
  test ! -x /var/lib/openbao-kms/state
'

# Reinstallation and tmpfiles processing must preserve operator files and state.
sha256sum /etc/openbao-kms/config.yaml /etc/openbao-kms/tls/ca.crt \
  /var/lib/openbao-kms/identity.jwt /var/lib/openbao-kms/state/install-check > "$work/before.sha256"
bash "$work/install.sh"
# Resolve the override exactly as boot-time tmpfiles processing does.
systemd-tmpfiles --create --prefix=/etc/openbao-kms --prefix=/var/lib/openbao-kms --prefix=/run/openbao-kms
sha256sum --check "$work/before.sha256"
runuser -u openbao-kms -- bao-kms-provider config --config /etc/openbao-kms/config.yaml > /dev/null
test ! -e /etc/systemd/system/multi-user.target.wants/bao-kms-provider.service
printf '%s\n' 'PASS: documented install, service-user access, socket-group isolation, and reinstall preservation'

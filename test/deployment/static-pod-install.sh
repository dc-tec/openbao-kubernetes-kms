#!/bin/bash
# Run only in the disposable container created by make static-pod-install-check.
set -euo pipefail
if [[ "${KMS_INSTALL_TEST_CONTAINER:-}" != 1 || "$(id -u)" != 0 ]]; then
  echo 'run through make static-pod-install-check; this test writes system paths' >&2
  exit 1
fi

work=$(mktemp -d)
export GOCACHE="$work/go-cache" GOFLAGS='-mod=vendor -buildvcs=false' GOTOOLCHAIN=local
arch=$(go env GOARCH)
if [[ -n "${BUNDLE_ARCHIVE:-}" ]]; then
  archive=$(realpath "$BUNDLE_ARCHIVE")
else
  archive="$work/kit.tar.gz"
  CGO_ENABLED=0 go build -o "$work/provider" ./cmd/bao-kms-provider
  go run ./hack/tools/release_bundle -kind static-pod -prefix kit \
    -binary "$work/provider" -output "$archive" \
    -image-ref "ghcr.io/dc-tec/bao-kms-provider@sha256:$(printf '%064d' 0)"
fi
expect_exit() {
  local want=$1 message=$2 code=0
  shift 2
  "$@" > "$work/out" 2>&1 || code=$?
  if [[ "$code" != "$want" ]] || ! grep -qF -- "$message" "$work/out"; then
    printf 'FAIL: %s exited %s, want %s with %q:\n' "$*" "$code" "$want" "$message" >&2
    cat "$work/out" >&2
    exit 1
  fi
}
mkdir "$work/extracted"
tar -xzf "$archive" -C "$work/extracted" --strip-components=1
cd "$work/extracted"
awk '
  $0 == "<!-- static-pod-kit-install -->" { found = 1; next }
  found && $0 == "```sh" { code = 1; next }
  code && $0 == "```" { done = 1; exit }
  code { print }
  END { if (!done) exit 1 }
' README.md > "$work/install.sh"
bash "$work/install.sh"
sh /src/test/deployment/check-architecture.sh "$arch" /usr/bin/bao-kms-provider
gid=$(getent group openbao-kms-socket | cut -d: -f3)

for source in file oauth2; do
  bao-kms-provider init --values "config/init-values-$source.yaml" --out "$work/$source" \
    --new-key --model static-pod --socket-gid "$gid" --image "$(cat image-ref.txt)"
  bao-kms-provider init --values "$work/$source/config.yaml" --out "$work/$source-node2" \
    --model static-pod --socket-gid "$gid" --image "$(cat image-ref.txt)"
  cmp "$work/$source/installation.json" "$work/$source-node2/installation.json"
done
# Run the generated node setup; the container has no containerd, so a stub
# reports the digest as preloaded.
mkdir -p /etc/kubernetes/manifests
printf '#!/bin/sh\ntest "$1" = inspecti\n' > /usr/local/bin/crictl
chmod 0755 /usr/local/bin/crictl
node_setup="$work/file/node-setup.sh"
bao-kms-provider init --values "$work/file/config.yaml" --out "$work/other-gid" \
  --model static-pod --socket-gid 4242 --image "$(cat image-ref.txt)"
expect_exit 3 'GID is' sh "$work/other-gid/node-setup.sh" prepare
expect_exit 3 'run check before start' sh "$node_setup" start
expect_exit 0 'Next: sh' sh "$node_setup" prepare
printf 'test CA fixture\n' > "$work/ca.crt"
printf 'non-secret credential fixture\n' > "$work/credential"
expect_exit 0 'Next: sh' sh "$node_setup" install --ca "$work/ca.crt" --credential "$work/credential"
expect_exit 3 'already exists' sh "$node_setup" install --ca "$work/ca.crt" --credential "$work/credential"
test "$(stat -c '%u:%g:%a' /etc/openbao-kms/config.yaml)" = 0:65532:640
test "$(stat -c '%u:%g:%a' /var/lib/openbao-kms/credentials/identity.jwt)" = 0:65532:640
test "$(stat -c '%u:%g:%a' /var/lib/openbao-kms/state)" = 65532:65532:750
test "$(stat -c '%u:%g:%a' /run/openbao-kms)" = "65532:$gid:2750"
# config and the fingerprint pass as UID 65532; verify-key needs OpenBao.
expect_exit 4 'verify-key failed' sh "$node_setup" check
expect_exit 3 'run check before start' sh "$node_setup" start
setpriv --reuid=65532 --regid=65532 --groups="$gid" sh -eu -c '
  bao-kms-provider config --config /etc/openbao-kms/config.yaml >/dev/null
  test -r /var/lib/openbao-kms/credentials/identity.jwt
  test ! -w /etc/openbao-kms/config.yaml
  touch /var/lib/openbao-kms/state/install-check /run/openbao-kms/install-check
'
setpriv --reuid=65531 --regid="$gid" --clear-groups sh -eu -c '
  test -x /run/openbao-kms
  test ! -w /run/openbao-kms
  test ! -r /etc/openbao-kms/config.yaml
  test ! -r /var/lib/openbao-kms/credentials/identity.jwt
  test ! -x /var/lib/openbao-kms/state
'
sha256sum /etc/openbao-kms/config.yaml /var/lib/openbao-kms/credentials/identity.jwt \
  /var/lib/openbao-kms/state/install-check > "$work/preserved.sha256"
bash "$work/install.sh"
expect_exit 0 'Next: sh' sh "$node_setup" prepare
sha256sum --check "$work/preserved.sha256"
test ! -e /etc/kubernetes/manifests/bao-kms-provider.yaml
printf 'PASS: linux/%s kit, both init sources, generated node setup, runtime ownership, isolation, and reinstall preservation\n' "$arch"

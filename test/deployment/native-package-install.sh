#!/bin/bash
# Run only through make native-package-install-check in a disposable Linux container.
set -euo pipefail
if [[ "${KMS_INSTALL_TEST_CONTAINER:-}" != 1 || "$(id -u)" != 0 ]]; then
  echo 'run through make native-package-install-check; this test writes system paths' >&2
  exit 1
fi
: "${PACKAGE_FILE:?select the exact package artifact}"
: "${PACKAGE_ARCH:?select amd64 or arm64}"
: "${PACKAGE_VERSION:?select the release version}"
work=$(mktemp -d)
package=$(realpath "$PACKAGE_FILE")
case "$package" in
  *.deb)
    test "$(dpkg-deb -f "$package" Package)" = bao-kms-provider
    test "$(dpkg-deb -f "$package" Architecture)" = "$PACKAGE_ARCH"
    install_package() { dpkg --install "$package"; }
    remove_package() { dpkg --remove bao-kms-provider; }
    ;;
  *.rpm)
    case "$PACKAGE_ARCH" in amd64) rpm_arch=x86_64 ;; arm64) rpm_arch=aarch64 ;; *) exit 2 ;; esac
    test "$(rpm -qp --queryformat '%{NAME}' "$package")" = bao-kms-provider
    test "$(rpm -qp --queryformat '%{ARCH}' "$package")" = "$rpm_arch"
    install_package() { rpm --upgrade --replacepkgs "$package"; }
    remove_package() { rpm --erase bao-kms-provider; }
    ;;
  *) echo 'select a .deb or .rpm artifact' >&2; exit 2 ;;
esac

install_package
sh /src/test/deployment/check-architecture.sh "$PACKAGE_ARCH" /usr/bin/bao-kms-provider
test "$(bao-kms-provider version | sed -n 's/^version: //p')" = "$PACKAGE_VERSION"
getent passwd openbao-kms >/dev/null
getent group openbao-kms-socket >/dev/null
test "$(stat -c '%a:%U:%G' /etc/openbao-kms)" = '750:root:openbao-kms'
test "$(stat -c '%a:%U:%G' /etc/openbao-kms/credentials)" = '750:root:openbao-kms'
test "$(stat -c '%a:%U:%G' /run/openbao-kms)" = '2750:openbao-kms:openbao-kms-socket'
test -x /usr/share/bao-kms-provider/probe-apiserver
systemd-analyze verify /usr/lib/systemd/system/bao-kms-provider.service

# No OpenBao or issuer is needed to check local installation boundaries.
install -m 0640 -o root -g openbao-kms \
  /usr/share/doc/bao-kms-provider/examples/provider-systemd.yaml /etc/openbao-kms/config.yaml
printf 'operator-owned credential fixture\n' > "$work/credential"
install -m 0640 -o root -g openbao-kms "$work/credential" /etc/openbao-kms/credentials/client-secret
runuser -u openbao-kms -- bao-kms-provider config --config /etc/openbao-kms/config.yaml >/dev/null
runuser -u openbao-kms -- sh -eu -c '
  test -r /etc/openbao-kms/credentials/client-secret
  test ! -w /etc/openbao-kms/config.yaml
  test ! -w /etc/openbao-kms/credentials/client-secret
  touch /var/lib/openbao-kms/state/package-check /run/openbao-kms/package-check
'
useradd --no-create-home --gid openbao-kms-socket kms-package-consumer
runuser -u kms-package-consumer -- sh -eu -c '
  test -x /run/openbao-kms
  test ! -w /run/openbao-kms
  test ! -r /etc/openbao-kms/config.yaml
  test ! -r /etc/openbao-kms/credentials/client-secret
  test ! -x /var/lib/openbao-kms/state
'
sha256sum /etc/openbao-kms/config.yaml /etc/openbao-kms/credentials/client-secret \
  /var/lib/openbao-kms/state/package-check > "$work/preserved.sha256"
install_package
systemd-tmpfiles --create /usr/lib/tmpfiles.d/openbao-kms.conf
sha256sum --check "$work/preserved.sha256"
test ! -e /etc/systemd/system/multi-user.target.wants/bao-kms-provider.service
test ! -e /run/openbao-kms/kms.sock
remove_package
sha256sum --check "$work/preserved.sha256"
test ! -e /usr/bin/bao-kms-provider
printf 'PASS: %s native package, runtime access, reinstall/removal preservation, and no activation\n' "$PACKAGE_ARCH"

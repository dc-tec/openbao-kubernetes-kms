#!/bin/sh
set -eu

ASSET_DIR="${ASSET_DIR:-/tmp/openbao-kms-provider-assets}"
CONFIG_PATH="${CONFIG_PATH:-/etc/openbao-kms/config.yaml}"
ENCRYPTION_CONFIG_PATH="${ENCRYPTION_CONFIG_PATH:-/etc/kubernetes/openbao-kms/encryption-config.yaml}"

if ! getent group openbao-kms >/dev/null; then
	groupadd --system openbao-kms
fi
if ! getent group openbao-kms-socket >/dev/null; then
	groupadd --system openbao-kms-socket
fi
if ! getent passwd openbao-kms >/dev/null; then
	useradd --system --gid openbao-kms --groups openbao-kms-socket \
		--home-dir /var/lib/openbao-kms --shell /usr/sbin/nologin \
		--comment "OpenBao Kubernetes KMS provider" openbao-kms
fi
usermod -a -G openbao-kms-socket openbao-kms

install -d -m 0750 -o root -g openbao-kms /etc/openbao-kms
install -d -m 0755 -o root -g root /etc/openbao-kms/tls
install -d -m 0750 -o openbao-kms -g openbao-kms /var/lib/openbao-kms
install -d -m 0750 -o openbao-kms -g openbao-kms /var/lib/openbao-kms/state
install -d -m 2750 -o openbao-kms -g openbao-kms-socket /run/openbao-kms
install -d -m 0755 -o root -g root /etc/kubernetes/openbao-kms
install -d -m 0755 -o root -g root /usr/lib/tmpfiles.d

cat >/usr/lib/tmpfiles.d/openbao-kms.conf <<'EOF'
d /run/openbao-kms 2750 openbao-kms openbao-kms-socket -
EOF
systemd-tmpfiles --create /usr/lib/tmpfiles.d/openbao-kms.conf

install -m 0755 -o root -g root "$ASSET_DIR/bao-kms-provider" /usr/bin/bao-kms-provider
install -m 0640 -o root -g openbao-kms "$ASSET_DIR/provider.yaml" "$CONFIG_PATH"
install -m 0644 -o root -g root "$ASSET_DIR/openbao-ca.crt" /etc/openbao-kms/tls/ca.crt
install -m 0600 -o openbao-kms -g openbao-kms "$ASSET_DIR/identity.jwt" /var/lib/openbao-kms/identity.jwt
install -m 0644 -o root -g root "$ASSET_DIR/encryption-config.yaml" "$ENCRYPTION_CONFIG_PATH"
install -m 0644 -o root -g root "$ASSET_DIR/bao-kms-provider.service" /etc/systemd/system/bao-kms-provider.service

/usr/bin/bao-kms-provider doctor --config "$CONFIG_PATH" --encryption-config "$ENCRYPTION_CONFIG_PATH" >/var/log/bao-kms-provider-doctor.log

systemctl daemon-reload
systemctl enable bao-kms-provider.service >/dev/null
systemctl restart bao-kms-provider.service

for _ in $(seq 1 60); do
	if curl -fsS http://127.0.0.1:8082/ready >/dev/null; then
		printf '%s\n' "systemd provider is ready"
		exit 0
	fi
	sleep 2
done

systemctl status bao-kms-provider.service --no-pager >&2 || true
journalctl -u bao-kms-provider.service -n 80 --no-pager >&2 || true
exit 1

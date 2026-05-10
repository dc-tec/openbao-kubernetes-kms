#!/bin/sh
set -eu

ASSET_DIR="${ASSET_DIR:-/tmp/openbao-kms-provider-assets}"
CONFIG_PATH="${CONFIG_PATH:-/etc/openbao-kms/config.yaml}"
ENCRYPTION_CONFIG_PATH="${ENCRYPTION_CONFIG_PATH:-/etc/kubernetes/openbao-kms/encryption-config.yaml}"
SOCKET_GID="${SOCKET_GID:-1234}"

crictl_cmd() {
	crictl --config /dev/null \
		--runtime-endpoint unix:///run/containerd/containerd.sock \
		--image-endpoint unix:///run/containerd/containerd.sock "$@"
}

if ! getent group "$SOCKET_GID" >/dev/null; then
	groupadd --system --gid "$SOCKET_GID" openbao-kms-socket
fi

install -d -m 0750 -o root -g root /etc/openbao-kms
install -d -m 0755 -o root -g root /etc/openbao-kms/tls
install -d -m 0750 -o 65532 -g 65532 /var/lib/openbao-kms
install -d -m 0750 -o 65532 -g 65532 /var/lib/openbao-kms/state
install -d -m 2750 -o 65532 -g "$SOCKET_GID" /run/openbao-kms
install -d -m 0755 -o root -g root /etc/kubernetes/openbao-kms
install -d -m 0755 -o root -g root /usr/lib/tmpfiles.d

cat >/usr/lib/tmpfiles.d/openbao-kms-static-pod.conf <<EOF
d /run/openbao-kms 2750 65532 ${SOCKET_GID} -
EOF
systemd-tmpfiles --create /usr/lib/tmpfiles.d/openbao-kms-static-pod.conf

install -m 0755 -o root -g root "$ASSET_DIR/bao-kms-provider" /usr/bin/bao-kms-provider
install -m 0640 -o root -g 65532 "$ASSET_DIR/provider.yaml" "$CONFIG_PATH"
install -m 0644 -o root -g root "$ASSET_DIR/openbao-ca.crt" /etc/openbao-kms/tls/ca.crt
install -m 0600 -o 65532 -g 65532 "$ASSET_DIR/identity.jwt" /var/lib/openbao-kms/identity.jwt
install -m 0644 -o root -g root "$ASSET_DIR/encryption-config.yaml" "$ENCRYPTION_CONFIG_PATH"

ctr -n k8s.io images import "$ASSET_DIR/bao-kms-provider-image.tar" >/dev/null
/usr/bin/bao-kms-provider doctor --config "$CONFIG_PATH" --encryption-config "$ENCRYPTION_CONFIG_PATH" >/var/log/bao-kms-provider-doctor.log
install -m 0644 -o root -g root "$ASSET_DIR/bao-kms-provider.yaml" /etc/kubernetes/manifests/bao-kms-provider.yaml

for _ in $(seq 1 90); do
	if curl -fsS http://127.0.0.1:8082/ready >/dev/null; then
		printf '%s\n' "static-pod provider is ready"
		exit 0
	fi
	sleep 2
done

cid="$(crictl_cmd ps -a --name '^bao-kms-provider$' -q | head -n1 || true)"
if [ -n "$cid" ]; then
	crictl_cmd logs "$cid" 2>&1 | tail -120 >&2 || true
else
	crictl_cmd ps -a >&2 || true
fi
exit 1

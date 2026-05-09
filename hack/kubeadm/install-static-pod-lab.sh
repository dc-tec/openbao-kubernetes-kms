#!/bin/sh
set -eu

ROOT="${ROOT:-}"
CONFIG="${CONFIG:-./deploy/config/provider-static-pod.yaml}"
MANIFEST="${MANIFEST:-./deploy/static-pod/bao-kms-provider.yaml}"
DEST_MANIFEST="${DEST_MANIFEST:-/etc/kubernetes/manifests/bao-kms-provider.yaml}"
SOCKET_GID="${SOCKET_GID:-1234}"

require_root() {
	if [ "$(id -u)" -ne 0 ]; then
		printf '%s\n' "run as root or set ROOT to a staging directory" >&2
		exit 1
	fi
}

if [ -z "$ROOT" ]; then
	require_root
fi

install -d -m 0750 -o root -g root "${ROOT}/etc/openbao-kms"
install -d -m 0755 -o root -g root "${ROOT}/etc/openbao-kms/tls"
install -d -m 0750 -o 65532 -g 65532 "${ROOT}/var/lib/openbao-kms"
install -d -m 0750 -o 65532 -g 65532 "${ROOT}/var/lib/openbao-kms/state"
install -d -m 2770 -o 65532 -g "$SOCKET_GID" "${ROOT}/run/openbao-kms"
install -D -m 0640 -o root -g 65532 "$CONFIG" "${ROOT}/etc/openbao-kms/config.yaml"
install -D -m 0644 -o root -g root "$MANIFEST" "${ROOT}${DEST_MANIFEST}"

printf '%s\n' "installed static pod lab manifest at ${ROOT}${DEST_MANIFEST}"
printf '%s\n' "replace the image digest and place ca.crt plus identity.jwt before enabling kube-apiserver encryption"

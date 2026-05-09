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

install_dir() {
	dst="$1"
	mode="$2"
	owner="$3"
	group="$4"
	if [ -n "$ROOT" ] && [ "${PRESERVE_OWNERS:-false}" != "true" ]; then
		install -d -m "$mode" "${ROOT}${dst}"
	else
		install -d -m "$mode" -o "$owner" -g "$group" "${ROOT}${dst}"
	fi
}

install_file() {
	src="$1"
	dst="$2"
	mode="$3"
	owner="$4"
	group="$5"
	if [ -n "$ROOT" ] && [ "${PRESERVE_OWNERS:-false}" != "true" ]; then
		install -D -m "$mode" "$src" "${ROOT}${dst}"
	else
		install -D -m "$mode" -o "$owner" -g "$group" "$src" "${ROOT}${dst}"
	fi
}

install_dir /etc/openbao-kms 0750 root root
install_dir /etc/openbao-kms/tls 0755 root root
install_dir /var/lib/openbao-kms 0750 65532 65532
install_dir /var/lib/openbao-kms/state 0750 65532 65532
install_dir /run/openbao-kms 2770 65532 "$SOCKET_GID"
install_file "$CONFIG" /etc/openbao-kms/config.yaml 0640 root 65532
install_file "$MANIFEST" "$DEST_MANIFEST" 0644 root root

printf '%s\n' "installed static pod lab manifest at ${ROOT}${DEST_MANIFEST}"
printf '%s\n' "replace the image digest and place ca.crt plus identity.jwt before enabling kube-apiserver encryption"

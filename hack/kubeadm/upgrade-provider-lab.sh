#!/bin/sh
set -eu

MODE="${MODE:-systemd}"
BINARY="${BINARY:-./bin/bao-kms-provider}"
CONFIG="${CONFIG:-/etc/openbao-kms/config.yaml}"
SERVICE="${SERVICE:-bao-kms-provider.service}"
MANIFEST="${MANIFEST:-./deploy/static-pod/bao-kms-provider.yaml}"
DEST_MANIFEST="${DEST_MANIFEST:-/etc/kubernetes/manifests/bao-kms-provider.yaml}"
BACKUP_DIR="${BACKUP_DIR:-/var/lib/openbao-kms/rollback}"

if [ "$(id -u)" -ne 0 ]; then
	printf '%s\n' "run as root on one control-plane node at a time" >&2
	exit 1
fi

mkdir -p "$BACKUP_DIR"

case "$MODE" in
systemd)
	bao-kms-provider doctor --config "$CONFIG"
	cp -p /usr/local/bin/bao-kms-provider "$BACKUP_DIR/bao-kms-provider.previous"
	install -m 0755 -o root -g root "$BINARY" /usr/local/bin/bao-kms-provider
	systemctl restart "$SERVICE"
	;;
static-pod)
	cp -p "$DEST_MANIFEST" "$BACKUP_DIR/bao-kms-provider.yaml.previous"
	install -m 0644 -o root -g root "$MANIFEST" "$DEST_MANIFEST"
	;;
*)
	printf '%s\n' "MODE must be systemd or static-pod" >&2
	exit 2
	;;
esac

printf '%s\n' "upgrade applied; verify /ready and KMS Status before moving to the next node"

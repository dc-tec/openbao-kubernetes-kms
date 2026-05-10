#!/bin/sh
set -eu

MODE="${MODE:-systemd}"
CONFIG="${CONFIG:-/etc/openbao-kms/config.yaml}"
SERVICE="${SERVICE:-bao-kms-provider.service}"
DEST_MANIFEST="${DEST_MANIFEST:-/etc/kubernetes/manifests/bao-kms-provider.yaml}"
BACKUP_DIR="${BACKUP_DIR:-/var/lib/openbao-kms/rollback}"

if [ "$(id -u)" -ne 0 ]; then
	printf '%s\n' "run as root on one control-plane node at a time" >&2
	exit 1
fi

case "$MODE" in
systemd)
	test -f "$BACKUP_DIR/bao-kms-provider.previous"
	bao-kms-provider doctor --config "$CONFIG"
	install -m 0755 -o root -g root "$BACKUP_DIR/bao-kms-provider.previous" /usr/bin/bao-kms-provider
	systemctl restart "$SERVICE"
	;;
static-pod)
	test -f "$BACKUP_DIR/bao-kms-provider.yaml.previous"
	install -m 0644 -o root -g root "$BACKUP_DIR/bao-kms-provider.yaml.previous" "$DEST_MANIFEST"
	;;
*)
	printf '%s\n' "MODE must be systemd or static-pod" >&2
	exit 2
	;;
esac

printf '%s\n' "rollback applied; verify decrypts before continuing"

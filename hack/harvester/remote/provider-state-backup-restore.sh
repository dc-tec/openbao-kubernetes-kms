#!/bin/sh
set -eu

ACTION="${ACTION:?ACTION is required}"
RESTORE_ID="${RESTORE_ID:?RESTORE_ID is required}"
OPENBAO_LAB_DIR="${OPENBAO_LAB_DIR:-/root/openbao-kms-lab}"
BACKUP_DIR="${OPENBAO_LAB_DIR}/backups"
BACKUP_PATH="${BACKUP_DIR}/provider-state-${RESTORE_ID}.tar.gz"
STATE_PARENT="${STATE_PARENT:-/var/lib/openbao-kms}"
STATE_DIR="${STATE_PARENT}/state"

case "$ACTION" in
backup)
	install -d -m 0700 "$BACKUP_DIR"
	if [ ! -d "$STATE_DIR" ]; then
		printf 'provider state directory not found: %s\n' "$STATE_DIR" >&2
		exit 1
	fi
	tar -C "$STATE_PARENT" -czf "$BACKUP_PATH" state
	chmod 0600 "$BACKUP_PATH"
	printf 'provider state backup written for %s\n' "$RESTORE_ID"
	;;
restore)
	if [ ! -f "$BACKUP_PATH" ]; then
		printf 'provider state backup not found: %s\n' "$BACKUP_PATH" >&2
		exit 1
	fi
	owner="$(stat -c '%u:%g' "$STATE_PARENT")"
	if [ -d "$STATE_DIR" ]; then
		mv "$STATE_DIR" "${STATE_DIR}.pre-openbao-kms-${RESTORE_ID}-$(date +%s)"
	fi
	tar -C "$STATE_PARENT" -xzf "$BACKUP_PATH"
	chown -R "$owner" "$STATE_DIR"
	chmod 0750 "$STATE_DIR"
	printf 'provider state restored for %s\n' "$RESTORE_ID"
	;;
*)
	printf 'unknown ACTION: %s\n' "$ACTION" >&2
	exit 2
	;;
esac

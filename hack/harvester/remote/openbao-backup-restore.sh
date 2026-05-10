#!/bin/sh
set -eu

ACTION="${ACTION:?ACTION is required}"
RESTORE_ID="${RESTORE_ID:?RESTORE_ID is required}"
OPENBAO_LAB_DIR="${OPENBAO_LAB_DIR:-/root/openbao-kms-lab}"
BACKUP_DIR="${OPENBAO_LAB_DIR}/backups"
BACKUP_PATH="${BACKUP_DIR}/openbao-data-${RESTORE_ID}.tar.gz"

export BAO_ADDR="https://127.0.0.1:8200"
export BAO_CACERT="/etc/openbao.d/tls/ca.crt"

wait_openbao_http() {
	health_url="$BAO_ADDR/v1/sys/health?standbyok=true&sealedcode=200&uninitcode=200"
	for _ in $(seq 1 60); do
		if curl -fsS --cacert "$BAO_CACERT" "$health_url" >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	return 1
}

unseal_openbao() {
	wait_openbao_http
	if bao status -format=json | jq -e '.sealed == true' >/dev/null; then
		unseal_key="$(jq -r '.unseal_keys_b64[0]' "${OPENBAO_LAB_DIR}/init.json")"
		bao operator unseal "$unseal_key" >/dev/null
	fi
}

case "$ACTION" in
backup)
	install -d -m 0700 "$BACKUP_DIR"
	systemctl stop openbao.service
	tar -C /var/lib/openbao -czf "$BACKUP_PATH" data
	chmod 0600 "$BACKUP_PATH"
	systemctl start openbao.service
	unseal_openbao
	printf 'OpenBao data backup written for %s\n' "$RESTORE_ID"
	;;
restore)
	if [ ! -f "$BACKUP_PATH" ]; then
		printf 'OpenBao backup not found: %s\n' "$BACKUP_PATH" >&2
		exit 1
	fi
	systemctl stop openbao.service
	if [ -d /var/lib/openbao/data ]; then
		mv /var/lib/openbao/data "/var/lib/openbao/data.pre-openbao-kms-${RESTORE_ID}-$(date +%s)"
	fi
	tar -C /var/lib/openbao -xzf "$BACKUP_PATH"
	chown -R openbao:openbao /var/lib/openbao/data
	systemctl start openbao.service
	unseal_openbao
	printf 'OpenBao data restored for %s\n' "$RESTORE_ID"
	;;
rotate-transit)
	root_token="$(jq -r '.root_token' "${OPENBAO_LAB_DIR}/init.json")"
	BAO_TOKEN="$root_token" bao write -f transit/keys/k8s-workload-a-etcd/rotate >/dev/null
	printf 'OpenBao Transit key rotated for %s\n' "$RESTORE_ID"
	;;
*)
	printf 'unknown ACTION: %s\n' "$ACTION" >&2
	exit 2
	;;
esac

#!/bin/sh
set -eu
. "$(dirname "$0")/common.sh"

require_cmd docker
ensure_state_dirs
validate_auth_mode

ca_file="$(find_openbao_ca)"
printf '%s\n' "$ca_file" > "$STATE_DIR/openbao/ca-file"

token_file="$STATE_DIR/openbao/root-token"
init_output_file="$STATE_DIR/openbao/init.txt"
status_file="$STATE_DIR/openbao/status.txt"
recovery_key_file="$STATE_DIR/openbao/recovery-key"

write_status() {
	docker_compose exec -T openbao bao status > "$status_file" 2>&1 || true
}

status_has() {
	key="$1"
	value="$2"
	awk -v key="$key" -v value="$value" '$1 == key && $2 == value { found = 1 } END { exit !found }' "$status_file"
}

wait_for_status() {
	deadline=$(( $(date +%s) + 120 ))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		write_status
		if grep -q '^Initialized[[:space:]]' "$status_file"; then
			return 0
		fi
		sleep 2
	done
	printf '%s\n' "OpenBao status did not become reachable" >&2
	cat "$status_file" >&2 || true
	return 1
}

capture_init_material() {
	root_token="$(awk -F': ' '/Initial Root Token:/ { print $2; exit }' "$init_output_file")"
	if [ -z "$root_token" ]; then
		printf '%s\n' "OpenBao init output did not include an initial root token" >&2
		cat "$init_output_file" >&2
		exit 1
	fi
	printf '%s\n' "$root_token" > "$token_file"
	chmod 0600 "$token_file"

	recovery_key="$(awk -F': ' '/^(Recovery Key|Unseal Key) 1:/ { print $2; exit }' "$init_output_file")"
	if [ -n "$recovery_key" ]; then
		printf '%s\n' "$recovery_key" > "$recovery_key_file"
		chmod 0600 "$recovery_key_file"
	fi
	chmod 0600 "$init_output_file"
}

initialize_openbao() {
	if status_has Initialized true; then
		if [ ! -s "$token_file" ]; then
			printf '%s\n' "OpenBao is already initialized but $token_file is missing; run make dev-env-reset" >&2
			exit 1
		fi
		return 0
	fi

	if ! docker_compose exec -T openbao bao operator init -recovery-shares=1 -recovery-threshold=1 > "$init_output_file" 2>&1; then
		docker_compose exec -T openbao bao operator init -key-shares=1 -key-threshold=1 > "$init_output_file" 2>&1
	fi
	capture_init_material
	write_status
}

wait_until_unsealed() {
	deadline=$(( $(date +%s) + 120 ))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		write_status
		if status_has Initialized true && status_has Sealed false; then
			printf '%s\n' "OpenBao is ready at $OPENBAO_HOST_ADDRESS"
			return 0
		fi
		sleep 2
	done
	printf '%s\n' "OpenBao did not become initialized and unsealed" >&2
	cat "$status_file" >&2 || true
	return 1
}

wait_for_status
initialize_openbao
wait_until_unsealed

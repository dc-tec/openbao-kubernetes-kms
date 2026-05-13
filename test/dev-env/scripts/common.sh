#!/bin/sh
set -eu

DEV_ENV_SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
DEV_ENV_DIR="$(CDPATH= cd -- "$DEV_ENV_SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$DEV_ENV_DIR/../.." && pwd)"
STATE_DIR="${DEV_ENV_STATE_DIR:-$DEV_ENV_DIR/.state}"

KIND_CLUSTER="${DEV_ENV_KIND_CLUSTER:-openbao-kms-dev}"
KIND_CONTEXT="kind-$KIND_CLUSTER"
KIND_NODE="${KIND_CLUSTER}-control-plane"
COMPOSE_PROJECT="${DEV_ENV_COMPOSE_PROJECT:-openbao-kms-dev-env}"
DEV_ENV_AUTH="${DEV_ENV_AUTH:-${AUTH:-jwt}}"
PROVIDER_IMAGE="${DEV_ENV_PROVIDER_IMAGE:-ghcr.io/dc-tec/bao-kms-provider:dev-env}"
OPENBAO_HOST_ADDRESS="${DEV_ENV_OPENBAO_HOST_ADDRESS:-https://localhost:18200}"
OPENBAO_PROVIDER_ADDRESS="${DEV_ENV_OPENBAO_PROVIDER_ADDRESS:-https://openbao:8200}"
OPENBAO_TLS_SERVER_NAME="${DEV_ENV_OPENBAO_TLS_SERVER_NAME:-localhost}"

JWT_ISSUER="${DEV_ENV_JWT_ISSUER:-https://issuer.openbao-kms.dev-env.internal}"
JWT_AUDIENCE="${DEV_ENV_JWT_AUDIENCE:-bao-kms-provider}"
JWT_SUBJECT="${DEV_ENV_JWT_SUBJECT:-system:openbao-kms:dev-env}"
JWT_TTL_SECONDS="${DEV_ENV_JWT_TTL_SECONDS:-3600}"

PKCS11_SPIFFE_ID="${DEV_ENV_PKCS11_SPIFFE_ID:-spiffe://example.org/openbao-kms/dev-env}"
PKCS11_TOKEN_LABEL="${DEV_ENV_PKCS11_TOKEN_LABEL:-openbao-kms-dev-env}"
PKCS11_KEY_LABEL="${DEV_ENV_PKCS11_KEY_LABEL:-openbao-kms-client}"
PKCS11_MODULE_PATH="${DEV_ENV_PKCS11_MODULE_PATH:-/usr/lib/softhsm/libsofthsm2.so}"

require_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf '%s\n' "$1 is required" >&2
		exit 127
	fi
}

docker_compose() {
	docker compose -f "$DEV_ENV_DIR/compose.yaml" --project-name "$COMPOSE_PROJECT" "$@"
}

ensure_state_dirs() {
	mkdir -p \
		"$STATE_DIR/jwt" \
		"$STATE_DIR/kind" \
		"$STATE_DIR/openbao/data" \
		"$STATE_DIR/openbao/seal" \
		"$STATE_DIR/openbao/tls" \
		"$STATE_DIR/opentofu" \
		"$STATE_DIR/pkcs11/hsm" \
		"$STATE_DIR/pkcs11/tls" \
		"$STATE_DIR/prometheus" \
		"$STATE_DIR/grafana"
	chmod 0777 "$STATE_DIR/openbao/data" "$STATE_DIR/prometheus" "$STATE_DIR/grafana"
}

validate_auth_mode() {
	case "$DEV_ENV_AUTH" in
		jwt|pkcs11)
			;;
		*)
			printf 'unsupported DEV_ENV_AUTH=%s; expected jwt or pkcs11\n' "$DEV_ENV_AUTH" >&2
			exit 2
			;;
	esac
}

kind_node_image() {
	awk '/^[[:space:]]*kindNodeImage:[[:space:]]*/ { print $2; exit }' "$REPO_ROOT/.ci/versions.yaml"
}

find_openbao_ca() {
	ca_file="$STATE_DIR/openbao/tls/ca.pem"
	if [ -z "$ca_file" ]; then
		printf '%s\n' "OpenBao CA certificate was not found under $STATE_DIR/openbao/tls" >&2
		exit 1
	fi
	if [ ! -s "$ca_file" ]; then
		printf '%s\n' "OpenBao CA certificate was not found at $ca_file" >&2
		exit 1
	fi
	printf '%s\n' "$ca_file"
}

render_template() {
	template="$1"
	output="$2"
	output_dir="$(dirname "$output")"
	mkdir -p "$output_dir"
	sed \
		-e "s|{{PROVIDER_IMAGE}}|$(sed_replacement "$PROVIDER_IMAGE")|g" \
		-e "s|{{JWT_ISSUER}}|$(sed_replacement "$JWT_ISSUER")|g" \
		-e "s|{{JWT_AUDIENCE}}|$(sed_replacement "$JWT_AUDIENCE")|g" \
		-e "s|{{JWT_SUBJECT}}|$(sed_replacement "$JWT_SUBJECT")|g" \
		-e "s|{{KEY_LINEAGE_ID}}|$(sed_replacement "$KEY_LINEAGE_ID")|g" \
		-e "s|{{PKCS11_MODULE_PATH}}|$(sed_replacement "$PKCS11_MODULE_PATH")|g" \
		-e "s|{{PKCS11_TOKEN_LABEL}}|$(sed_replacement "$PKCS11_TOKEN_LABEL")|g" \
		-e "s|{{PKCS11_KEY_LABEL}}|$(sed_replacement "$PKCS11_KEY_LABEL")|g" \
		"$template" > "$output"
}

sed_replacement() {
	printf '%s' "$1" | sed 's/[&|\\]/\\&/g'
}

wait_for_readyz() {
	require_cmd kubectl
	deadline="${1:-120}"
	end=$(( $(date +%s) + deadline ))
	while [ "$(date +%s)" -lt "$end" ]; do
		if kubectl --context "$KIND_CONTEXT" get --raw=/readyz 2>/dev/null | grep -q '^ok'; then
			return 0
		fi
		sleep 2
	done
	printf '%s\n' "kube-apiserver did not become ready for context $KIND_CONTEXT" >&2
	return 1
}

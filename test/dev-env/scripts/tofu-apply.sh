#!/bin/sh
set -eu
. "$(dirname "$0")/common.sh"

require_cmd tofu
ensure_state_dirs
validate_auth_mode

ca_file="$(find_openbao_ca)"
token_file="$STATE_DIR/openbao/root-token"
if [ ! -s "$token_file" ]; then
	printf '%s\n' "OpenBao root token is missing; run make dev-env-openbao first" >&2
	exit 1
fi

export TF_VAR_openbao_address="$OPENBAO_HOST_ADDRESS"
export TF_VAR_openbao_token="$(cat "$token_file")"
export TF_VAR_openbao_ca_cert_file="$ca_file"
export TF_VAR_openbao_tls_server_name="$OPENBAO_TLS_SERVER_NAME"
export TF_VAR_jwt_public_key_file="$STATE_DIR/jwt/jwt.pub"
export TF_VAR_jwt_issuer="$JWT_ISSUER"
export TF_VAR_jwt_audience="$JWT_AUDIENCE"
export TF_VAR_jwt_subject="$JWT_SUBJECT"
if [ "$DEV_ENV_AUTH" = "pkcs11" ]; then
	if [ ! -s "$STATE_DIR/pkcs11/client-ca.pem" ]; then
		printf '%s\n' "PKCS#11 client CA is missing; run make dev-env-pkcs11-material first" >&2
		exit 1
	fi
	export TF_VAR_enable_jwt_auth=false
	export TF_VAR_enable_pkcs11_cert_auth=true
	export TF_VAR_pkcs11_client_ca_file="$STATE_DIR/pkcs11/client-ca.pem"
	export TF_VAR_pkcs11_uri_san="$PKCS11_SPIFFE_ID"
else
	export TF_VAR_enable_jwt_auth=true
	export TF_VAR_enable_pkcs11_cert_auth=false
fi

tofu -chdir="$DEV_ENV_DIR/opentofu" init
tofu -chdir="$DEV_ENV_DIR/opentofu" apply -auto-approve

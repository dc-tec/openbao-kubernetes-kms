#!/bin/sh
set -eu

OPENBAO_LAB_DIR="${OPENBAO_LAB_DIR:-/root/openbao-kms-lab}"
TRANSIT_MOUNT="${TRANSIT_MOUNT:-transit}"
TRANSIT_KEY="${TRANSIT_KEY:-k8s-workload-a-etcd}"
AUTH_MOUNT="${AUTH_MOUNT:-k8s-workload-a-jwt}"
AUTH_ROLE="${AUTH_ROLE:-openbao-kms-control-plane}"
POLICY_NAME="${POLICY_NAME:-openbao-kms-workload-a}"
JWT_ISSUER="${JWT_ISSUER:-https://issuer.example.internal}"
JWT_AUDIENCE="${JWT_AUDIENCE:-bao-kms-provider}"
JWT_SUBJECT="${JWT_SUBJECT:-system:openbao-kms:workload-a}"

export BAO_ADDR="${BAO_ADDR:-https://127.0.0.1:8200}"
export BAO_CACERT="${BAO_CACERT:-/etc/openbao.d/tls/ca.crt}"
export BAO_TOKEN="$(jq -r '.root_token' "$OPENBAO_LAB_DIR/init.json")"

if ! bao status -format=json | jq -e '.sealed == false' >/dev/null; then
	unseal_key="$(jq -r '.unseal_keys_b64[0]' "$OPENBAO_LAB_DIR/init.json")"
	bao operator unseal "$unseal_key" >/dev/null
fi

if ! bao secrets list -format=json | jq -e --arg mount "${TRANSIT_MOUNT}/" 'has($mount)' >/dev/null; then
	bao secrets enable -path="$TRANSIT_MOUNT" transit >/dev/null
fi

if ! bao read -format=json "${TRANSIT_MOUNT}/keys/${TRANSIT_KEY}" >/dev/null 2>&1; then
	bao write "${TRANSIT_MOUNT}/keys/${TRANSIT_KEY}" type=aes256-gcm96 >/dev/null
fi
bao write "${TRANSIT_MOUNT}/config/keys" disable_upsert=true >/dev/null

cat >"$OPENBAO_LAB_DIR/provider-policy.hcl" <<EOF
path "${TRANSIT_MOUNT}/keys/${TRANSIT_KEY}" {
  capabilities = ["read"]
}

path "${TRANSIT_MOUNT}/encrypt/${TRANSIT_KEY}" {
  capabilities = ["update"]
}

path "${TRANSIT_MOUNT}/decrypt/${TRANSIT_KEY}" {
  capabilities = ["update"]
}

path "${TRANSIT_MOUNT}/config/keys" {
  capabilities = ["read"]
}

path "sys/capabilities-self" {
  capabilities = ["update"]
}
EOF
bao policy write "$POLICY_NAME" "$OPENBAO_LAB_DIR/provider-policy.hcl" >/dev/null

if ! bao auth list -format=json | jq -e --arg mount "${AUTH_MOUNT}/" 'has($mount)' >/dev/null; then
	bao auth enable -path="$AUTH_MOUNT" jwt >/dev/null
fi

bao write "auth/${AUTH_MOUNT}/config" \
	jwt_validation_pubkeys=@"$OPENBAO_LAB_DIR/jwt_public_key.pem" \
	bound_issuer="$JWT_ISSUER" >/dev/null

bao write "auth/${AUTH_MOUNT}/role/${AUTH_ROLE}" \
	role_type=jwt \
	user_claim=sub \
	bound_audiences="$JWT_AUDIENCE" \
	bound_subject="$JWT_SUBJECT" \
	token_policies="$POLICY_NAME" \
	token_ttl=10m \
	token_max_ttl=30m \
	token_no_default_policy=true \
	clock_skew_leeway=30s \
	expiration_leeway=30s >/dev/null

printf '%s\n' "OpenBao Transit and JWT auth configured"

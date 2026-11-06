#!/bin/sh
set -eu
. "$(dirname "$0")/common.sh"

require_cmd openssl
ensure_state_dirs
validate_auth_mode

jwt_key="$STATE_DIR/jwt/jwt.key"
jwt_pub="$STATE_DIR/jwt/jwt.pub"
jwt_file="$STATE_DIR/jwt/identity.jwt"
lineage_file="$STATE_DIR/key-lineage-id"
static_seal_key="$STATE_DIR/openbao/seal/static-unseal.key"
openbao_tls_dir="$STATE_DIR/openbao/tls"
openbao_node_dir="$openbao_tls_dir/openbao-kms-dev-openbao-01"
openbao_ca_key="$openbao_tls_dir/ca.key"
openbao_ca_crt="$openbao_tls_dir/ca.pem"
openbao_server_key="$openbao_node_dir/private-key.pem"
openbao_server_csr="$openbao_node_dir/server.csr"
openbao_server_crt="$openbao_node_dir/server.pem"
openbao_server_chain="$openbao_node_dir/full-chain.pem"

json_string() {
	printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

if [ ! -s "$jwt_key" ]; then
	openssl genrsa -out "$jwt_key" 2048 >/dev/null 2>&1
	chmod 0600 "$jwt_key"
fi
openssl rsa -in "$jwt_key" -pubout -out "$jwt_pub" >/dev/null 2>&1
chmod 0644 "$jwt_pub"

if [ ! -s "$lineage_file" ]; then
	printf 'dev-%s\n' "$(openssl rand -hex 12)" > "$lineage_file"
fi
chmod 0644 "$lineage_file"

if [ ! -s "$static_seal_key" ]; then
	openssl rand 32 > "$static_seal_key"
	chmod 0600 "$static_seal_key"
fi

if [ ! -s "$openbao_ca_key" ] || [ ! -s "$openbao_ca_crt" ]; then
	openssl req -x509 -new -newkey rsa:4096 -nodes -days 1825 -sha256 \
		-subj "/CN=OpenBao KMS Dev CA" \
		-keyout "$openbao_ca_key" \
		-out "$openbao_ca_crt" >/dev/null 2>&1
	chmod 0600 "$openbao_ca_key"
	chmod 0644 "$openbao_ca_crt"
fi

if [ ! -s "$openbao_server_key" ] || [ ! -s "$openbao_server_chain" ]; then
	mkdir -p "$openbao_node_dir"
	san_cfg="$(mktemp)"
	cat > "$san_cfg" <<'EOF'
[req]
distinguished_name = dn
req_extensions = v3_req
[dn]
[v3_req]
subjectAltName = @alt_names
extendedKeyUsage = serverAuth, clientAuth
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
[alt_names]
DNS.1 = openbao-kms-dev-openbao-01
DNS.2 = openbao
DNS.3 = localhost
IP.1 = 127.0.0.1
EOF
	openssl req -new -newkey rsa:2048 -nodes \
		-subj "/CN=openbao-kms-dev-openbao-01" \
		-keyout "$openbao_server_key" \
		-out "$openbao_server_csr" \
		-config "$san_cfg" \
		-reqexts v3_req >/dev/null 2>&1
	openssl x509 -req -in "$openbao_server_csr" -days 825 -sha256 \
		-CA "$openbao_ca_crt" \
		-CAkey "$openbao_ca_key" \
		-CAcreateserial \
		-out "$openbao_server_crt" \
		-extfile "$san_cfg" \
		-extensions v3_req >/dev/null 2>&1
	cat "$openbao_server_crt" "$openbao_ca_crt" > "$openbao_server_chain"
	rm -f "$openbao_server_csr" "$san_cfg"
	chmod 0600 "$openbao_server_key"
	chmod 0644 "$openbao_server_crt" "$openbao_server_chain"
fi

header_b64="$(printf '%s' '{"alg":"RS256","typ":"JWT"}' | openssl base64 -A | tr '+/' '-_' | tr -d '=')"
now="$(date +%s)"
nbf=$(( now - 30 ))
exp=$(( now + JWT_TTL_SECONDS ))
claims_json="$(printf '{"aud":["%s"],"exp":%s,"iat":%s,"iss":"%s","nbf":%s,"sub":"%s"}' \
	"$(json_string "$JWT_AUDIENCE")" \
	"$exp" \
	"$now" \
	"$(json_string "$JWT_ISSUER")" \
	"$nbf" \
	"$(json_string "$JWT_SUBJECT")")"
claims_b64="$(printf '%s' "$claims_json" | openssl base64 -A | tr '+/' '-_' | tr -d '=')"
signing_input="$header_b64.$claims_b64"
sig_b64="$(printf '%s' "$signing_input" | openssl dgst -sha256 -sign "$jwt_key" -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')"
printf '%s.%s\n' "$signing_input" "$sig_b64" > "$jwt_file"
chmod 0600 "$jwt_file"

if [ "$DEV_ENV_AUTH" = "oauth2" ]; then
	keycloak_dir="$STATE_DIR/keycloak"
	keycloak_key="$keycloak_dir/tls/private-key.pem"
	keycloak_chain="$keycloak_dir/tls/full-chain.pem"
	if [ ! -s "$keycloak_key" ] || [ ! -s "$keycloak_chain" ]; then
		san_cfg="$(mktemp)"
		cat > "$san_cfg" <<'EOF'
[req]
distinguished_name = dn
req_extensions = v3_req
[dn]
[v3_req]
subjectAltName = @alt_names
extendedKeyUsage = serverAuth
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
[alt_names]
DNS.1 = keycloak
DNS.2 = localhost
IP.1 = 127.0.0.1
EOF
		openssl req -new -newkey rsa:2048 -nodes -subj "/CN=keycloak" \
			-keyout "$keycloak_key" -out "$keycloak_dir/tls/server.csr" \
			-config "$san_cfg" -reqexts v3_req >/dev/null 2>&1
		openssl x509 -req -in "$keycloak_dir/tls/server.csr" -days 825 -sha256 \
			-CA "$openbao_ca_crt" -CAkey "$openbao_ca_key" -CAcreateserial \
			-out "$keycloak_dir/tls/server.pem" -extfile "$san_cfg" -extensions v3_req >/dev/null 2>&1
		cat "$keycloak_dir/tls/server.pem" "$openbao_ca_crt" > "$keycloak_chain"
		rm -f "$keycloak_dir/tls/server.csr" "$san_cfg"
		chmod 0600 "$keycloak_key"
		chmod 0644 "$keycloak_dir/tls/server.pem" "$keycloak_chain"
	fi

	client_secret_file="$STATE_DIR/oauth2/client-secret"
	if [ ! -s "$client_secret_file" ]; then
		(umask 077 && openssl rand -hex 32 > "$client_secret_file")
	fi
	admin_env="$keycloak_dir/admin.env"
	if [ ! -s "$admin_env" ]; then
		(umask 077 && printf 'KC_BOOTSTRAP_ADMIN_USERNAME=admin\nKC_BOOTSTRAP_ADMIN_PASSWORD=%s\n' \
			"$(openssl rand -hex 16)" > "$admin_env")
	fi

	# Import the shipped client profile unchanged, adding only the fixture ID and
	# secret, so the lab exercises the documented Keycloak settings.
	client_json="$(awk -v id="$KEYCLOAK_CLIENT_UUID" -v secret_file="$client_secret_file" '
		BEGIN { getline secret < secret_file; close(secret_file) }
		NR == 1 { printf "{\"id\":\"%s\",\"secret\":\"%s\",\n", id, secret; next }
		{ print }
	' "$REPO_ROOT/deploy/config/keycloak-client.json")"
	(umask 077 && cat > "$keycloak_dir/realm.json" <<EOF
{
  "realm": "kms-dev",
  "enabled": true,
  "accessTokenLifespan": 300,
  "clients": [$client_json],
  "users": [{
    "id": "$KEYCLOAK_SUBJECT",
    "username": "service-account-$KEYCLOAK_CLIENT_ID",
    "enabled": true,
    "serviceAccountClientId": "$KEYCLOAK_CLIENT_ID"
  }]
}
EOF
	)
fi

printf '%s\n' "generated dev-env material under $STATE_DIR"

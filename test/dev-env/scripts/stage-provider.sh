#!/bin/sh
set -eu
. "$(dirname "$0")/common.sh"

require_cmd docker
require_cmd kubectl
ensure_state_dirs
validate_auth_mode

if [ "$DEV_ENV_AUTH" = "jwt" ] && [ ! -s "$STATE_DIR/jwt/identity.jwt" ]; then
	printf '%s\n' "JWT file is missing; run make dev-env-generate first" >&2
	exit 1
fi
if [ "$DEV_ENV_AUTH" = "pkcs11" ]; then
	for required_file in \
		"$STATE_DIR/pkcs11/client-ca.pem" \
		"$STATE_DIR/pkcs11/hsm/softhsm2.conf" \
		"$STATE_DIR/pkcs11/tls/client-chain.pem" \
		"$STATE_DIR/pkcs11/tls/pin"; do
		if [ ! -s "$required_file" ]; then
			printf 'PKCS#11 file is missing: %s\n' "$required_file" >&2
			exit 1
		fi
	done
fi
ca_file="$(find_openbao_ca)"
key_lineage_id="$(cat "$STATE_DIR/key-lineage-id")"

export JWT_ISSUER
export JWT_AUDIENCE
export JWT_SUBJECT
export KEY_LINEAGE_ID="$key_lineage_id"
export PROVIDER_IMAGE
export PKCS11_MODULE_PATH
export PKCS11_TOKEN_LABEL
export PKCS11_KEY_LABEL

case "$DEV_ENV_AUTH" in
	jwt)
		render_template "$DEV_ENV_DIR/config/provider-jwt.yaml.tmpl" "$STATE_DIR/kind/provider.yaml"
		render_template "$DEV_ENV_DIR/kind/provider-static-pod.yaml.tmpl" "$STATE_DIR/kind/bao-kms-provider.yaml"
		;;
	pkcs11)
		render_template "$DEV_ENV_DIR/config/provider-pkcs11.yaml.tmpl" "$STATE_DIR/kind/provider.yaml"
		render_template "$DEV_ENV_DIR/kind/provider-static-pod-pkcs11.yaml.tmpl" "$STATE_DIR/kind/bao-kms-provider.yaml"
		;;
esac
cp "$DEV_ENV_DIR/kind/encryption-config.yaml" "$STATE_DIR/kind/encryption-config.yaml"
cp "$ca_file" "$STATE_DIR/kind/openbao-ca.pem"

docker exec "$KIND_NODE" sh -c 'mkdir -p /etc/openbao-kms/tls /etc/openbao-kms/pkcs11 /etc/kubernetes/encryption/openbao-kms /var/lib/openbao-kms/pkcs11/hsm /var/lib/openbao-kms/state /run/openbao-kms'
docker cp "$STATE_DIR/kind/provider.yaml" "$KIND_NODE:/etc/openbao-kms/config.yaml"
docker cp "$STATE_DIR/kind/openbao-ca.pem" "$KIND_NODE:/etc/openbao-kms/tls/openbao-ca.pem"
docker cp "$STATE_DIR/kind/encryption-config.yaml" "$KIND_NODE:/etc/kubernetes/encryption/openbao-kms/encryption-config.yaml"

if [ "$DEV_ENV_AUTH" = "jwt" ]; then
	docker cp "$STATE_DIR/jwt/identity.jwt" "$KIND_NODE:/var/lib/openbao-kms/identity.jwt"
else
	docker cp "$STATE_DIR/pkcs11/tls/." "$KIND_NODE:/etc/openbao-kms/pkcs11"
	docker cp "$STATE_DIR/pkcs11/hsm/." "$KIND_NODE:/var/lib/openbao-kms/pkcs11/hsm"
fi

docker exec "$KIND_NODE" sh -c '
  chown root:65532 /etc/openbao-kms/config.yaml &&
  chmod 0640 /etc/openbao-kms/config.yaml &&
  chmod 0644 /etc/openbao-kms/tls/openbao-ca.pem /etc/kubernetes/encryption/openbao-kms/encryption-config.yaml &&
  if [ -f /var/lib/openbao-kms/identity.jwt ]; then chown root:65532 /var/lib/openbao-kms/identity.jwt && chmod 0640 /var/lib/openbao-kms/identity.jwt; fi &&
  if [ -d /etc/openbao-kms/pkcs11 ]; then chown -R root:65532 /etc/openbao-kms/pkcs11 && find /etc/openbao-kms/pkcs11 -type d -exec chmod 0750 {} + && find /etc/openbao-kms/pkcs11 -type f -exec chmod 0640 {} +; fi &&
  if [ -d /var/lib/openbao-kms/pkcs11/hsm ]; then chown -R 65532:65532 /var/lib/openbao-kms/pkcs11/hsm && find /var/lib/openbao-kms/pkcs11/hsm -type d -exec chmod 0700 {} + && find /var/lib/openbao-kms/pkcs11/hsm -type f -exec chmod 0600 {} +; fi &&
  chown -R 65532:65532 /var/lib/openbao-kms/state &&
  chmod 0750 /var/lib/openbao-kms/state &&
  chown 65532:1234 /run/openbao-kms &&
  chmod 2750 /run/openbao-kms
'

docker cp "$STATE_DIR/kind/bao-kms-provider.yaml" "$KIND_NODE:/etc/kubernetes/manifests/bao-kms-provider.yaml"

deadline=$(( $(date +%s) + 120 ))
while [ "$(date +%s)" -lt "$deadline" ]; do
	if docker exec "$KIND_NODE" test -S /run/openbao-kms/kms.sock >/dev/null 2>&1; then
		printf '%s\n' "provider socket is available in $KIND_NODE"
		exit 0
	fi
	sleep 2
done

printf '%s\n' "provider socket did not become available" >&2
docker exec "$KIND_NODE" crictl ps -a >&2 || true
docker exec "$KIND_NODE" crictl logs "$(docker exec "$KIND_NODE" crictl ps -a --name '^bao-kms-provider$' -q | head -n 1)" >&2 || true
exit 1

#!/bin/sh
set -eu
. "$(dirname "$0")/common.sh"

require_cmd docker
ensure_state_dirs
validate_auth_mode

if [ "$DEV_ENV_AUTH" != "pkcs11" ]; then
	exit 0
fi

if ! docker image inspect "$PROVIDER_IMAGE" >/dev/null 2>&1; then
	printf 'provider image %s is required before SoftHSM setup\n' "$PROVIDER_IMAGE" >&2
	exit 1
fi

rm -rf "$STATE_DIR/pkcs11/hsm" "$STATE_DIR/pkcs11/tls"
mkdir -p "$STATE_DIR/pkcs11/hsm" "$STATE_DIR/pkcs11/tls"

docker run --rm \
	--user 0:0 \
	--entrypoint /certauth-pkcs11-setup \
	--volume "$STATE_DIR/pkcs11/hsm:/hsm" \
	--volume "$STATE_DIR/pkcs11/tls:/bao/tls" \
	--volume "$STATE_DIR/pkcs11:/out" \
	"$PROVIDER_IMAGE" \
	--softhsm-config /hsm/softhsm2.conf \
	--token-directory /hsm/tokens \
	--module-path "$PKCS11_MODULE_PATH" \
	--token-label "$PKCS11_TOKEN_LABEL" \
	--key-label "$PKCS11_KEY_LABEL" \
	--pin-file /bao/tls/pin \
	--certificate-file /bao/tls/client-chain.pem \
	--ca-file /out/client-ca.pem \
	--spiffe-id "$PKCS11_SPIFFE_ID"

printf '%s\n' "generated SoftHSM PKCS#11 material under $STATE_DIR/pkcs11"

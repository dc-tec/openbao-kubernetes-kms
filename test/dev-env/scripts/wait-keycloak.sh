#!/bin/sh
set -eu
. "$(dirname "$0")/common.sh"

validate_auth_mode
if [ "$DEV_ENV_AUTH" != "oauth2" ]; then
	exit 0
fi
require_cmd curl
ca_file="$(find_openbao_ca)"
client_secret_file="$STATE_DIR/oauth2/client-secret"
realm_url="$KEYCLOAK_HOST_ADDRESS/realms/kms-dev"

deadline=$(( $(date +%s) + 180 ))
until curl -fsS --connect-timeout 3 --max-time 10 --cacert "$ca_file" \
	-o /dev/null "$realm_url/.well-known/openid-configuration" 2>/dev/null; do
	if [ "$(date +%s)" -ge "$deadline" ]; then
		printf '%s\n' "Keycloak did not become ready at $realm_url" >&2
		docker_compose logs --tail 50 keycloak >&2 || true
		exit 1
	fi
	sleep 3
done

# Request one token with the provider's client so a broken realm import fails
# here, before OpenBao and the provider depend on it. curl reads the client
# credentials from stdin, so the secret never appears in a command argument.
token_response="$(printf 'user = "%s:%s"\n' "$KEYCLOAK_CLIENT_ID" "$(cat "$client_secret_file")" |
	curl -fsS --connect-timeout 3 --max-time 10 --config - --cacert "$ca_file" \
		--data grant_type=client_credentials "$realm_url/protocol/openid-connect/token")"
case "$token_response" in
	*'"access_token"'*) ;;
	*) printf '%s\n' "Keycloak did not issue a client credentials token" >&2; exit 1 ;;
esac
printf '%s\n' "Keycloak issuer is ready at $KEYCLOAK_ISSUER"

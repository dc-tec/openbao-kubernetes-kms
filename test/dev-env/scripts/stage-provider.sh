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

if [ "$DEV_ENV_AUTH" = "jwt" ]; then
	# JWT mode follows the documented path: init generates the files and the
	# node runs the generated node-setup.sh phases.
	if docker exec "$KIND_NODE" test -e /etc/openbao-kms/config.yaml; then
		printf '%s\n' "provider is already staged in $KIND_NODE; run make dev-env-reset for a fresh node" >&2
		exit 1
	fi
	case "$(docker exec "$KIND_NODE" uname -m)" in
		x86_64) node_arch=amd64 ;;
		aarch64 | arm64) node_arch=arm64 ;;
		*) printf '%s\n' "unsupported Kind node architecture" >&2; exit 1 ;;
	esac
	image_digest="$(docker exec "$KIND_NODE" ctr --namespace=k8s.io images list |
		awk -v image="$PROVIDER_IMAGE" '$1 == image && $3 ~ /^sha256:/ { print $3; exit }')"
	if [ -z "$image_digest" ]; then
		printf '%s\n' "provider image $PROVIDER_IMAGE is not loaded; run make dev-env-build" >&2
		exit 1
	fi
	pinned_image="${PROVIDER_IMAGE%:*}@$image_digest"
	docker exec "$KIND_NODE" ctr --namespace=k8s.io images tag --force "$PROVIDER_IMAGE" "$pinned_image" >/dev/null
	docker exec "$KIND_NODE" sh -c 'getent group openbao-kms-socket >/dev/null || groupadd --system --gid 1234 openbao-kms-socket'
	socket_gid="$(docker exec "$KIND_NODE" getent group openbao-kms-socket | cut -d: -f3)"

	render_template "$DEV_ENV_DIR/config/provider-jwt.yaml.tmpl" "$STATE_DIR/kind/values.yaml"
	rm -rf "$STATE_DIR/kind/generated"
	(cd "$REPO_ROOT" &&
		go run ./cmd/bao-kms-provider init --values "$STATE_DIR/kind/values.yaml" --out "$STATE_DIR/kind/generated" \
			--model static-pod --image "$pinned_image" --socket-gid "$socket_gid" &&
		CGO_ENABLED=0 GOOS=linux GOARCH="$node_arch" go build -o "$STATE_DIR/kind/bao-kms-provider" ./cmd/bao-kms-provider)

	docker exec "$KIND_NODE" rm -rf /root/openbao-kms-stage
	docker exec "$KIND_NODE" mkdir -p /root/openbao-kms-stage /etc/kubernetes/encryption/openbao-kms
	docker cp "$STATE_DIR/kind/bao-kms-provider" "$KIND_NODE:/usr/bin/bao-kms-provider"
	docker exec "$KIND_NODE" chmod 0755 /usr/bin/bao-kms-provider
	docker cp "$STATE_DIR/kind/generated" "$KIND_NODE:/root/openbao-kms-stage/generated"
	docker cp "$ca_file" "$KIND_NODE:/root/openbao-kms-stage/ca.crt"
	docker cp "$STATE_DIR/jwt/identity.jwt" "$KIND_NODE:/root/openbao-kms-stage/identity.jwt"
	docker cp "$DEV_ENV_DIR/kind/encryption-config.yaml" \
		"$KIND_NODE:/etc/kubernetes/encryption/openbao-kms/encryption-config.yaml"
	docker exec "$KIND_NODE" chmod 0644 /etc/kubernetes/encryption/openbao-kms/encryption-config.yaml

	setup=/root/openbao-kms-stage/generated/node-setup.sh
	docker exec "$KIND_NODE" sh "$setup" prepare
	docker exec "$KIND_NODE" sh "$setup" install \
		--ca /root/openbao-kms-stage/ca.crt --credential /root/openbao-kms-stage/identity.jwt
	docker exec "$KIND_NODE" sh "$setup" check
	docker exec "$KIND_NODE" sh "$setup" start
	printf '%s\n' "provider started through node-setup.sh in $KIND_NODE"
	exit 0
fi

# PKCS#11 certificate auth is outside init and node-setup.sh; stage it from templates.
render_template "$DEV_ENV_DIR/config/provider-pkcs11.yaml.tmpl" "$STATE_DIR/kind/provider.yaml"
render_template "$DEV_ENV_DIR/kind/provider-static-pod-pkcs11.yaml.tmpl" "$STATE_DIR/kind/bao-kms-provider.yaml"
cp "$DEV_ENV_DIR/kind/encryption-config.yaml" "$STATE_DIR/kind/encryption-config.yaml"
cp "$ca_file" "$STATE_DIR/kind/openbao-ca.pem"

docker exec "$KIND_NODE" sh -c 'mkdir -p /etc/openbao-kms/tls /etc/openbao-kms/pkcs11 /etc/kubernetes/encryption/openbao-kms /var/lib/openbao-kms/pkcs11/hsm /var/lib/openbao-kms/state /run/openbao-kms'
docker cp "$STATE_DIR/kind/provider.yaml" "$KIND_NODE:/etc/openbao-kms/config.yaml"
docker cp "$STATE_DIR/kind/openbao-ca.pem" "$KIND_NODE:/etc/openbao-kms/tls/openbao-ca.pem"
docker cp "$STATE_DIR/kind/encryption-config.yaml" "$KIND_NODE:/etc/kubernetes/encryption/openbao-kms/encryption-config.yaml"
docker cp "$STATE_DIR/pkcs11/tls/." "$KIND_NODE:/etc/openbao-kms/pkcs11"
docker cp "$STATE_DIR/pkcs11/hsm/." "$KIND_NODE:/var/lib/openbao-kms/pkcs11/hsm"

docker exec "$KIND_NODE" sh -c '
  chown root:65532 /etc/openbao-kms/config.yaml &&
  chmod 0640 /etc/openbao-kms/config.yaml &&
  chmod 0644 /etc/openbao-kms/tls/openbao-ca.pem /etc/kubernetes/encryption/openbao-kms/encryption-config.yaml &&
  chown -R root:65532 /etc/openbao-kms/pkcs11 && find /etc/openbao-kms/pkcs11 -type d -exec chmod 0750 {} + && find /etc/openbao-kms/pkcs11 -type f -exec chmod 0640 {} + &&
  chown -R 65532:65532 /var/lib/openbao-kms/pkcs11/hsm && find /var/lib/openbao-kms/pkcs11/hsm -type d -exec chmod 0700 {} + && find /var/lib/openbao-kms/pkcs11/hsm -type f -exec chmod 0600 {} + &&
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

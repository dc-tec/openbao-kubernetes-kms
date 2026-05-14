#!/bin/sh
set -eu
. "$(dirname "$0")/common.sh"

require_cmd base64
require_cmd docker
require_cmd kubectl
require_cmd openssl

secret_name="obk-dev-env-verify"
secret_value="dev-env-secret-$(openssl rand -hex 8)"

kubectl --context "$KIND_CONTEXT" delete secret "$secret_name" --ignore-not-found >/dev/null
kubectl --context "$KIND_CONTEXT" create secret generic "$secret_name" --from-literal=value="$secret_value" >/dev/null

readback="$(kubectl --context "$KIND_CONTEXT" get secret "$secret_name" -o jsonpath='{.data.value}' | base64 -d)"
if [ "$readback" != "$secret_value" ]; then
	printf '%s\n' "Kubernetes Secret readback did not match" >&2
	exit 1
fi

raw="$(docker exec "$KIND_NODE" sh -c "
set -eu
cid=\"\$(crictl ps --name etcd -q | head -n1)\"
if [ -z \"\$cid\" ]; then
  printf '%s\n' 'no etcd container found' >&2
  crictl ps -a >&2
  exit 1
fi
crictl exec \"\$cid\" etcdctl \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key \
  get /registry/secrets/default/$secret_name
")"

if printf '%s' "$raw" | grep -a -q "$secret_value"; then
	printf '%s\n' "raw etcd value contains the Secret plaintext" >&2
	exit 1
fi
if ! printf '%s' "$raw" | grep -a -q 'k8s:enc:kms:v2:'; then
	printf '%s\n' "raw etcd value does not contain a Kubernetes KMS v2 envelope marker" >&2
	exit 1
fi

printf '%s\n' "verified Kubernetes KMS v2 encryption for Secret $secret_name"

#!/bin/sh
set -eu

SECRET_NAME="${SECRET_NAME:?SECRET_NAME is required}"
SECRET_VALUE="${SECRET_VALUE:-}"
SECRET_VALUE_FILE="${SECRET_VALUE_FILE:-}"

if [ -n "$SECRET_VALUE_FILE" ]; then
	SECRET_VALUE="$(cat "$SECRET_VALUE_FILE")"
fi
if [ -z "$SECRET_VALUE" ]; then
	printf '%s\n' "SECRET_VALUE or SECRET_VALUE_FILE is required" >&2
	exit 1
fi

crictl_cmd() {
	crictl --config /dev/null \
		--runtime-endpoint unix:///run/containerd/containerd.sock \
		--image-endpoint unix:///run/containerd/containerd.sock "$@"
}

export KUBECONFIG=/etc/kubernetes/admin.conf
encoded="$(kubectl get secret "$SECRET_NAME" -n default -o jsonpath='{.data.value}')"
decoded="$(printf '%s' "$encoded" | base64 -d)"
if [ "$decoded" != "$SECRET_VALUE" ]; then
	printf '%s\n' "Kubernetes Secret value did not round-trip" >&2
	exit 1
fi

cid="$(crictl_cmd ps --name etcd -q | head -n1)"
if [ -z "$cid" ]; then
	printf '%s\n' "no etcd container found" >&2
	crictl_cmd ps -a >&2 || true
	exit 1
fi

raw="$(crictl_cmd exec "$cid" etcdctl \
	--endpoints=https://127.0.0.1:2379 \
	--cacert=/etc/kubernetes/pki/etcd/ca.crt \
	--cert=/etc/kubernetes/pki/etcd/server.crt \
	--key=/etc/kubernetes/pki/etcd/server.key \
	get "/registry/secrets/default/${SECRET_NAME}")"

if printf '%s' "$raw" | grep -Fq "$SECRET_VALUE"; then
	printf '%s\n' "etcd stored the Kubernetes Secret plaintext" >&2
	exit 1
fi
if ! printf '%s' "$raw" | grep -Fq 'k8s:enc:kms:v2:'; then
	printf '%s\n' "etcd Secret value did not use Kubernetes KMS v2 envelope format" >&2
	exit 1
fi

printf 'KMS v2 envelope verified for %s\n' "$SECRET_NAME"

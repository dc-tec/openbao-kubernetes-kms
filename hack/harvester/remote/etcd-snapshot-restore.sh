#!/bin/sh
set -eu

ACTION="${ACTION:?ACTION is required}"
RESTORE_ID="${RESTORE_ID:?RESTORE_ID is required}"
OPENBAO_LAB_DIR="${OPENBAO_LAB_DIR:-/root/openbao-kms-lab}"
BACKUP_DIR="${OPENBAO_LAB_DIR}/backups"
SNAPSHOT_PATH="${BACKUP_DIR}/etcd-${RESTORE_ID}.db"
ETCD_IMAGE="${ETCD_IMAGE:-registry.k8s.io/etcd:3.6.5-0}"
MANIFEST_DIR="${OPENBAO_LAB_DIR}/manifests-${RESTORE_ID}"

crictl_cmd() {
	crictl --config /dev/null \
		--runtime-endpoint unix:///run/containerd/containerd.sock \
		--image-endpoint unix:///run/containerd/containerd.sock "$@"
}

wait_no_container() {
	container_pattern="$1"
	for _ in $(seq 1 60); do
		if [ -z "$(crictl_cmd ps -a --name "$container_pattern" -q | head -n1)" ]; then
			return 0
		fi
		sleep 2
	done
	printf 'timed out waiting for container to stop: %s\n' "$container_pattern" >&2
	crictl_cmd ps -a >&2 || true
	return 1
}

wait_etcd() {
	for _ in $(seq 1 90); do
		cid="$(crictl_cmd ps --name '^etcd$' -q | head -n1 || true)"
		if [ -n "$cid" ] && crictl_cmd exec "$cid" etcdctl \
			--endpoints=https://127.0.0.1:2379 \
			--cacert=/etc/kubernetes/pki/etcd/ca.crt \
			--cert=/etc/kubernetes/pki/etcd/server.crt \
			--key=/etc/kubernetes/pki/etcd/server.key \
			endpoint health >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	printf 'timed out waiting for etcd health\n' >&2
	crictl_cmd ps -a >&2 || true
	return 1
}

case "$ACTION" in
snapshot)
	install -d -m 0700 "$BACKUP_DIR"
	cid="$(crictl_cmd ps --name '^etcd$' -q | head -n1 || true)"
	if [ -z "$cid" ]; then
		printf 'no etcd container found\n' >&2
		crictl_cmd ps -a >&2 || true
		exit 1
	fi
	container_snapshot="/var/lib/etcd/openbao-kms-${RESTORE_ID}.db"
	crictl_cmd exec "$cid" etcdctl \
		--endpoints=https://127.0.0.1:2379 \
		--cacert=/etc/kubernetes/pki/etcd/ca.crt \
		--cert=/etc/kubernetes/pki/etcd/server.crt \
		--key=/etc/kubernetes/pki/etcd/server.key \
		snapshot save "$container_snapshot" >/dev/null
	cp "$container_snapshot" "$SNAPSHOT_PATH"
	rm -f "$container_snapshot"
	chmod 0600 "$SNAPSHOT_PATH"
	printf 'etcd snapshot written for %s\n' "$RESTORE_ID"
	;;
restore)
	if [ ! -f "$SNAPSHOT_PATH" ]; then
		printf 'etcd snapshot not found: %s\n' "$SNAPSHOT_PATH" >&2
		exit 1
	fi
	previous_etcd_dir=""
	restore_failed() {
		status="$?"
		if [ "$status" -eq 0 ]; then
			return 0
		fi
		if [ ! -d /var/lib/etcd ] && [ -n "$previous_etcd_dir" ] && [ -d "$previous_etcd_dir" ]; then
			mv "$previous_etcd_dir" /var/lib/etcd
		fi
		if [ -f "$MANIFEST_DIR/etcd.yaml" ] && [ ! -f /etc/kubernetes/manifests/etcd.yaml ]; then
			mv "$MANIFEST_DIR/etcd.yaml" /etc/kubernetes/manifests/etcd.yaml
		fi
		if [ -f "$MANIFEST_DIR/kube-apiserver.yaml" ] && [ ! -f /etc/kubernetes/manifests/kube-apiserver.yaml ]; then
			mv "$MANIFEST_DIR/kube-apiserver.yaml" /etc/kubernetes/manifests/kube-apiserver.yaml
		fi
		exit "$status"
	}
	trap restore_failed EXIT

	member_name="$(awk -F= '/--name=/{ print $2; exit }' /etc/kubernetes/manifests/etcd.yaml)"
	member_peer_url="$(awk -F= '/--initial-advertise-peer-urls=/{ print $2; exit }' /etc/kubernetes/manifests/etcd.yaml)"
	member_initial_cluster="${member_name}=${member_peer_url}"

	install -d -m 0700 "$MANIFEST_DIR"
	mv /etc/kubernetes/manifests/kube-apiserver.yaml "$MANIFEST_DIR/kube-apiserver.yaml"
	mv /etc/kubernetes/manifests/etcd.yaml "$MANIFEST_DIR/etcd.yaml"
	wait_no_container '^kube-apiserver$'
	wait_no_container '^etcd$'

	if [ -d /var/lib/etcd ]; then
		previous_etcd_dir="/var/lib/etcd.pre-openbao-kms-${RESTORE_ID}-$(date +%s)"
		mv /var/lib/etcd "$previous_etcd_dir"
	fi
	rm -rf /var/lib/etcd.restore
	ctr -n k8s.io run --rm \
		--mount "type=bind,src=${OPENBAO_LAB_DIR},dst=${OPENBAO_LAB_DIR},options=rbind:rw" \
		--mount type=bind,src=/var/lib,dst=/var/lib-host,options=rbind:rw \
		"$ETCD_IMAGE" "etcd-restore-${RESTORE_ID}" \
		etcdutl snapshot restore "$SNAPSHOT_PATH" \
		--data-dir=/var/lib-host/etcd.restore \
		--name="$member_name" \
		--initial-cluster="$member_initial_cluster" \
		--initial-advertise-peer-urls="$member_peer_url" >/dev/null
	mv /var/lib/etcd.restore /var/lib/etcd
	mv "$MANIFEST_DIR/etcd.yaml" /etc/kubernetes/manifests/etcd.yaml
	wait_etcd
	mv "$MANIFEST_DIR/kube-apiserver.yaml" /etc/kubernetes/manifests/kube-apiserver.yaml
	trap - EXIT
	printf 'etcd snapshot restored for %s\n' "$RESTORE_ID"
	;;
*)
	printf 'unknown ACTION: %s\n' "$ACTION" >&2
	exit 2
	;;
esac

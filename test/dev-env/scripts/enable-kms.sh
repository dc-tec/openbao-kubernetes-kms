#!/bin/sh
set -eu
. "$(dirname "$0")/common.sh"

require_cmd docker
require_cmd kubectl

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
manifest="$tmp_dir/kube-apiserver.yaml"
patched="$tmp_dir/kube-apiserver-patched.yaml"

docker cp "$KIND_NODE:/etc/kubernetes/manifests/kube-apiserver.yaml" "$manifest"

if grep -q -- '--encryption-provider-config=' "$manifest"; then
	cp "$manifest" "$patched"
else
	awk '
		BEGIN {
			encryption_config = "/etc/kubernetes/encryption/openbao-kms/encryption-config.yaml"
			encryption_dir = "/etc/kubernetes/encryption/openbao-kms"
		}
		{
			print
			if ($0 == "    - --tls-private-key-file=/etc/kubernetes/pki/apiserver.key") {
				print "    - --encryption-provider-config=" encryption_config
				command_inserted = 1
			}
			if ($0 == "    - mountPath: /usr/share/ca-certificates") {
				in_ca_mount = 1
			} else if (in_ca_mount && $0 == "      name: usr-share-ca-certificates") {
				in_ca_mount = 2
			} else if (in_ca_mount == 2 && $0 == "      readOnly: true") {
				print "    - mountPath: " encryption_dir
				print "      name: openbao-kms-encryption"
				print "      readOnly: true"
				print "    - mountPath: /run/openbao-kms"
				print "      name: openbao-kms-run"
				mount_inserted = 1
				in_ca_mount = 0
			} else if (in_ca_mount) {
				in_ca_mount = 0
			}
			if ($0 == "      path: /usr/share/ca-certificates") {
				in_ca_volume = 1
			} else if (in_ca_volume && $0 == "      type: DirectoryOrCreate") {
				in_ca_volume = 2
			} else if (in_ca_volume == 2 && $0 == "    name: usr-share-ca-certificates") {
				print "  - hostPath:"
				print "      path: " encryption_dir
				print "      type: DirectoryOrCreate"
				print "    name: openbao-kms-encryption"
				print "  - hostPath:"
				print "      path: /run/openbao-kms"
				print "      type: Directory"
				print "    name: openbao-kms-run"
				volume_inserted = 1
				in_ca_volume = 0
			} else if (in_ca_volume && $0 !~ /^  - hostPath:$/) {
				in_ca_volume = 0
			}
		}
		END {
			if (!command_inserted || !mount_inserted || !volume_inserted) {
				print "kube-apiserver manifest anchor not found" > "/dev/stderr"
				exit 1
			}
		}
	' "$manifest" > "$patched"
fi

docker cp "$patched" "$KIND_NODE:/etc/kubernetes/manifests/kube-apiserver.yaml"
wait_for_readyz 180
kubectl --context "$KIND_CONTEXT" wait --for=condition=Ready "node/$KIND_NODE" --timeout=180s >/dev/null
printf '%s\n' "kube-apiserver KMS encryption config is enabled"

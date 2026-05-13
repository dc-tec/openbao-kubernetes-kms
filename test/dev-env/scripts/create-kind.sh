#!/bin/sh
set -eu
. "$(dirname "$0")/common.sh"

require_cmd docker
require_cmd kind
ensure_state_dirs

if kind get clusters | grep -qx "$KIND_CLUSTER"; then
	printf '%s\n' "Kind cluster $KIND_CLUSTER already exists"
	exit 0
fi

node_image="${DEV_ENV_KIND_NODE_IMAGE:-$(kind_node_image)}"
if [ -z "$node_image" ]; then
	printf '%s\n' "Kind node image could not be resolved from .ci/versions.yaml" >&2
	exit 1
fi

kind create cluster \
	--name "$KIND_CLUSTER" \
	--image "$node_image" \
	--config "$DEV_ENV_DIR/kind/cluster.yaml" \
	--wait 2m

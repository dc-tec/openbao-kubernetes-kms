---
title: "Upgrade"
description: "Upgrade the bao-kms-provider binary or image one control-plane node at a time, verify the KMS path stays healthy, and roll back safely if needed."
weight: 30
---

# Upgrade

Upgrade `bao-kms-provider` one control-plane node at a time. Each provider is
on the API server boot path, so verify the KMS path before continuing to the
next node.

For wire-format compatibility expectations and the upgrade-window history, see [Reference: Release Policy](/reference/release-policy/) and [Reference: Compatibility](/reference/compatibility/).

## Before You Start

Verify:

- the new binary or image is fetched and verified per [Install: Verify Release Artifacts](/getting-started/install/#verify-release-artifacts),
- the cluster is not mid-rotation (check `bao-kms-provider rotation-plan --config /etc/openbao-kms/config.yaml`),
- OpenBao is healthy and the configured auth credentials on every node are valid,
- the existing provider reports a stable `key_id` hash on every control-plane node,
- `bao-kms-provider doctor --config /etc/openbao-kms/config.yaml --encryption-config /etc/kubernetes/encryption-config.yaml` passes,
- the previous binary or image is still available on every node in case of rollback.

Record:

- current provider version,
- current Kubernetes `key_id` hash,
- current Transit key version,
- wire-format expectations from the new release notes.

## Upgrade Procedure

Upgrade one control-plane node at a time. Do not upgrade all provider instances
simultaneously unless the cluster is in a controlled maintenance window and
OpenBao and API server recovery has been tested.

For each node:

1. Run `bao-kms-provider doctor --config /etc/openbao-kms/config.yaml --encryption-config /etc/kubernetes/encryption-config.yaml` with the new binary or image. Resolve any failed checks before continuing.
2. Stop the provider on the target node.
3. Replace the binary or image with the new version.
4. Start the provider.
5. Verify KMS Status and the active `key_id` hash:

   ```sh
   curl -sf http://127.0.0.1:8081/metrics \
     | grep -E 'openbao_kms_status_key_id_hash|openbao_kms_grpc_requests_total'
   ```

6. Confirm `/ready` returns HTTP 200 on the target node.
7. Confirm the `key_id` hash matches the other control-plane nodes.
8. Restart the local API server only if required by the new release notes.
9. Repeat for the next node.

After every node has been upgraded, confirm the metric `openbao_kms_status_key_id_hash` reports the same hash on every node.

## Rollback

Rollback is safe only when the older version understands every active `key_id`,
annotation, and additional authenticated data (AAD) format currently present in
etcd. Mixed wire formats during rollback can produce unknown-`key_id` decrypt
errors.

Before rolling back:

- verify the older version supports the current `key_id` and AAD formats by reviewing release notes for wire-format changes,
- confirm the older version can decrypt the current set of objects on a non-production probe if possible,
- keep the new binary or image available until rollback is fully verified.

For each node:

1. Stop the provider on the target node.
2. Replace the binary or image with the previous version.
3. Run `bao-kms-provider doctor --config /etc/openbao-kms/config.yaml --encryption-config /etc/kubernetes/encryption-config.yaml` with the previous version.
4. Start the provider.
5. Verify Status and the `key_id` hash match the rest of the fleet.
6. Restart the API server only if required.

If rollback produces unknown-`key_id` errors, return to the newer version and investigate per [Operations: Troubleshooting](/operations/troubleshooting/#unknown-key-id).

For systemd deployments, replace the host binary or package and restart
`bao-kms-provider.service`. For static-pod deployments, restore the previous
image digest in the manifest. Confirm that the image is present or pullable on
the node.

## When Not To Upgrade

Defer the upgrade if:

- a Transit key rotation is in progress or pending stability,
- OpenBao is in a high availability (HA) failover or restore window,
- the new release notes call out wire-format changes and storage migration has not been planned,
- backup and restore drills have not been completed for the current release.

## When Not To Roll Back

A rollback is unsafe and must not be attempted if:

- the new version introduced a new wire format and ciphertext is already encrypted under it,
- the older version cannot interpret current annotations or AAD shape,
- a Transit key rotation completed under the new version and the older version did not promote the new active snapshot,
- release notes call out a one-way upgrade.

Apply a patch to the new version in these cases. Do not attempt rollback.

---
title: "Rotation"
description: "Rotate the OpenBao Transit key version, observe the provider's promotion state machine, migrate existing API resources, and verify rotation completed."
weight: 10
---

# Rotation

This runbook covers OpenBao Transit key rotation for `bao-kms-provider`. Transit key rotation and Kubernetes storage migration are separate operations. The plugin observes Transit key versions and exposes a new Kubernetes `key_id` only after the rotation state machine decides the new version is stable. Operators rewrite Kubernetes resources so old encrypted data is updated.

For the design rationale behind the rotation state machine, including the flip-flop guards and observation thresholds, see [Architecture: Rotation Model](/architecture/rotation-model/).

## Before Rotation

Verify:

- OpenBao backup is current.
- etcd backup is current.
- The plugin is healthy on every control-plane node.
- All nodes report the same active `key_id` hash.
- `bao-kms-provider doctor` passes on every control-plane node.
- No `identity` fallback remains unexpectedly in the API server `EncryptionConfiguration`.
- OpenBao `min_decryption_version` allows every version still present in etcd and backups.

Record:

- the current Kubernetes `key_id` hash,
- the current Transit key version,
- the OpenBao backup ID,
- the etcd backup ID,
- the plugin version,
- the control-plane node list.

## Rotate The Transit Key

Rotation is performed by an operator with OpenBao administrative rights:

```sh
bao write -f transit/keys/k8s-workload-a-etcd/rotate
```

The plugin token must not have rotate permission. The provisioned policy excludes this capability by design; see [Reference: Transit Policy Examples](/reference/transit-policy-examples/).

## Observe Promotion

After rotation:

1. The plugin background probe observes the new Transit latest version.
2. The plugin waits for `rotation.requireStableObservationCount` successful observations.
3. The plugin waits `rotation.activationDelay`.
4. The plugin promotes a new active key snapshot.
5. KMS `Status.key_id` changes.
6. New encrypt operations use the explicit Transit `key_version` for the new version.

Watch the rotation state from the CLI:

```sh
bao-kms-provider rotation-plan --config /etc/openbao-kms/config.yaml
```

Watch the metric across all control-plane nodes:

```sh
curl -sf http://127.0.0.1:9090/metrics \
  | grep -E 'openbao_kms_status_key_id_hash|openbao_kms_rotation_state'
```

Expected state:

- the old version remains decryptable,
- the new version becomes active once the stability window passes,
- every control-plane node converges to the same `key_id` hash,
- no node flips back to the old `key_id`.

## Migrate Kubernetes Data

Rewrite targeted resources after Status exposes the new `key_id`.

For Secrets:

```sh
kubectl get secrets --all-namespaces -o json | kubectl replace -f -
```

Repeat for each configured resource type. The pattern is `kubectl get <resource> --all-namespaces -o json | kubectl replace -f -`.

## Verify Rotation

```sh
bao-kms-provider verify-rotation --config /etc/openbao-kms/config.yaml
```

Then:

- restart one API server and verify reads succeed,
- verify new writes carry the new `key_id`,
- check the provider decrypt-error metrics on every control-plane node,
- check the OpenBao decrypt error rate,
- check API server encryption metrics where available,
- inspect etcd in a controlled environment if required.

For the metric and log catalog used during these checks see [Reference: Observability](/reference/observability/).

## min_decryption_version

Do not raise OpenBao `min_decryption_version` until:

- every targeted live object has been rewritten,
- old backups have expired or are known not to need the old version,
- restore testing has proved that the remaining backup set can decrypt,
- `verify-rotation` confidence is acceptable.

Raising `min_decryption_version` too early can make old Kubernetes data unreadable even when the Transit key still exists. The operation is not reversible without restoring backups.

## Rollback

If new encrypt or decrypt behavior fails before migration completes:

1. Stop promotion if the rotation state machine has not yet activated the new version.
2. Keep old Transit key versions decryptable. Do not raise `min_decryption_version`.
3. Restore the previous plugin version and configuration if the failure is plugin-related.
4. Do not delete the new Transit version.
5. Do not recreate the Transit key.
6. Use `doctor` and the metric catalog in [Reference: Observability](/reference/observability/) to identify the failing layer.

If objects have already been rewritten with the new version, rollback still requires the new Transit version to remain decryptable.

## Stop Rotation If

Abort rotation and consult [Operations: Troubleshooting](/operations/troubleshooting/) when:

- nodes report different active `key_id` hashes,
- Status flips old to new to old,
- unknown `key_id` decrypt errors appear in metrics or logs,
- AAD mismatch errors appear,
- OpenBao metadata reads are inconsistent,
- `min_decryption_version` was changed unexpectedly,
- any control-plane API server cannot restart cleanly.

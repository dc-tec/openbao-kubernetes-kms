---
title: "Rotation"
description: "Rotate the OpenBao Transit key version, observe provider promotion, migrate existing API resources, and preserve recovery records."
weight: 10
---

# Rotation

OpenBao Transit key rotation and Kubernetes storage migration are separate
operations. The provider observes Transit key versions and exposes a new
Kubernetes `key_id` only after the rotation state machine determines that the
new version is stable. Operators then rewrite Kubernetes resources to update
data encrypted with old versions.

For the design rationale behind the rotation state machine, including the flip-flop guards and observation thresholds, see [Architecture: Rotation Model](/architecture/rotation-model/).

Rotation changes the active Transit version under an existing Transit key. It
must not change the provider name, cluster ID, OpenBao instance ID, Transit
mount ID, key lineage ID, mount path, or key name. These fields are
identity-bearing. Changing one requires a migration plan; see [Configuration:
Identity-Bearing Fields](/reference/configuration/#identity-bearing-fields).

## Preview Boundary

Current preview tooling reports local registry state and OpenBao Transit metadata.
It does not enumerate Kubernetes resources, inspect etcd, prove that every
targeted object was rewritten, or prove that retained backups no longer require
old Transit versions.

Treat `verify-rotation` as a local preflight signal. Rewrite proof,
backup-retention proof, and any recommendation to raise `min_decryption_version`
remain operator-controlled until a proof-producing command exists.

## Before Rotation

Verify:

- OpenBao backup is current.
- etcd backup is current.
- The provider is healthy on every control-plane node.
- All nodes report the same active `key_id` hash.
- `bao-kms-provider doctor --config /etc/openbao-kms/config.yaml --encryption-config /etc/kubernetes/encryption-config.yaml` passes on every control-plane node.
- No `identity` fallback remains unexpectedly in the API server `EncryptionConfiguration`.
- OpenBao `min_decryption_version` allows every version still present in etcd and backups.

Record:

- the current Kubernetes `key_id` hash,
- the current Transit key version,
- the OpenBao backup ID,
- the etcd backup ID,
- the provider version,
- the control-plane node list.

## Rotate The Transit Key

Rotation is performed by an operator with OpenBao administrative rights:

```sh
bao write -f transit/keys/k8s-workload-a-etcd/rotate
```

The provider token must not have rotate permission. The provisioned policy
excludes this capability by design; see [Reference: Transit Policy
Examples](/reference/transit-policy-examples/).

## Observe Promotion

After rotation:

1. The provider background probe observes the new Transit latest version.
2. The provider waits for `rotation.requireStableObservationCount` successful observations.
3. The provider waits `rotation.activationDelay`.
4. The provider promotes a new active key snapshot.
5. KMS `Status.key_id` changes.
6. New encrypt operations use the explicit Transit `key_version` for the new version.

Watch the rotation state from the CLI:

```sh
bao-kms-provider rotation-plan --config /etc/openbao-kms/config.yaml
```

Watch the metric on each control-plane node:

```sh
curl -sf http://127.0.0.1:8081/metrics \
  | grep -E 'openbao_kms_status_key_id_hash|openbao_kms_key_version|openbao_kms_rotation_state'
```

Expected state:

- the old version remains decryptable,
- the new version becomes active once the stability window passes,
- every control-plane node converges to the same `key_id` hash,
- no node flips back to the old `key_id`.

If `latest_version` jumps over one or more Transit versions, the provider
requires OpenBao to report creation metadata for every skipped version. Complete
metadata lets the provider retain skipped versions as decrypt-only historical
snapshots. Missing intermediate metadata fails closed because another
control-plane node may have already encrypted data under a skipped version.

The rotation metric is intentionally bounded to `state="active"`, `state="pending"`, and `state="unknown"`. Use `rotation-plan` for the detailed promotion reason and timing.

## Migrate Kubernetes Data

Rewrite targeted resources after Status exposes the new `key_id`. Define the
complete resource list from the API server `EncryptionConfiguration` before
starting migration, and keep the command output, timestamps, and resource list
for recovery records.

For Secrets:

```sh
kubectl get secrets --all-namespaces -o json | kubectl replace -f -
```

Repeat for each configured resource type. The pattern is `kubectl get <resource> --all-namespaces -o json | kubectl replace -f -`.

## Verify Rotation

```sh
bao-kms-provider verify-rotation --config /etc/openbao-kms/config.yaml
```

This command confirms the provider's local registry and Transit metadata view.
When it succeeds, it still reports limited confidence because it does not scan
Kubernetes resources, inspect etcd, or evaluate retained backups.

Then collect independent verification:

- run `bao-kms-provider doctor --config /etc/openbao-kms/config.yaml --encryption-config /etc/kubernetes/encryption-config.yaml`,
- restart one API server and verify reads succeed,
- verify new writes carry the new `key_id`,
- verify every configured resource type was included in the rewrite procedure,
- check the provider decrypt-error metrics on every control-plane node,
- check the OpenBao decrypt error rate,
- check API server encryption metrics where available,
- compare `openbao_kms_status_key_id_hash` across all control-plane nodes,
- confirm retained backup sets either still have old Transit versions available
  or no longer need them,
- inspect etcd in a controlled environment if required.

For the metric and log catalog used during these checks see [Reference: Observability](/reference/observability/).

## min_decryption_version

Do not raise OpenBao `min_decryption_version` until:

- every targeted live object has been rewritten,
- old backups have expired or are known not to need the old version,
- restore testing has proved that the remaining backup set can decrypt,
- a human-reviewed change record identifies the exact Transit versions that
  remain required and the rollback plan.

`verify-rotation` is not a recommendation engine for this setting. It cannot
prove that old ciphertext no longer exists in Kubernetes, etcd snapshots, or
retained backups.

Raising `min_decryption_version` too early can make old Kubernetes data
unreadable even when the Transit key still exists. Lowering the value may help
only when the old key version still exists and policy allows it. Treat this as
an emergency recovery step, not a rollback plan.

## Rollback

If new encrypt or decrypt behavior fails before migration completes:

1. Stop promotion by restoring the previous known-good provider configuration and version if the rotation state machine has not yet activated the new version.
2. Keep old Transit key versions decryptable. Do not raise `min_decryption_version`.
3. Restore the previous provider version and configuration if the failure is provider-related.
4. Do not delete the new Transit version.
5. Do not recreate the Transit key.
6. Use `doctor`, `rotation-plan`, and the metric catalog in [Reference: Observability](/reference/observability/) to identify the failing layer.

If objects have already been rewritten with the new version, rollback still requires the new Transit version to remain decryptable.

## Stop Rotation If

Abort rotation and consult [Operations: Troubleshooting](/operations/troubleshooting/) when:

- nodes report different active `key_id` hashes,
- Status flips old to new to old,
- a back-to-back Transit rotation occurs before every node converges,
- unknown `key_id` decrypt errors appear in metrics or logs,
- additional authenticated data (AAD) mismatch errors appear,
- OpenBao metadata reads are inconsistent or missing intermediate Transit
  version creation metadata,
- `min_decryption_version` was changed unexpectedly,
- any control-plane API server cannot restart cleanly.

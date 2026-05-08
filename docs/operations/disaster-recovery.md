# Disaster Recovery

This runbook covers recovery scenarios for OpenBao-backed Kubernetes encryption-at-rest.

## Core Principle

OpenBao Transit key material and Kubernetes etcd data must be recoverable as a compatible pair.

If etcd contains objects encrypted with a Transit key version that no longer exists or is no longer decryptable, Kubernetes may be unable to read those objects. Adding `identity` fallback does not decrypt existing KMS ciphertext.

## Backup Requirements

Back up:

- etcd snapshots,
- OpenBao storage snapshots,
- Transit key metadata and versions through OpenBao backup,
- plugin config,
- key lineage IDs,
- OpenBao JWT auth configuration,
- OpenBao policies,
- CA bundles,
- deployment manifests or systemd units.

Record with every backup set:

- Kubernetes cluster ID,
- provider name,
- OpenBao instance ID,
- Transit mount ID,
- Transit key lineage ID,
- active Transit key version,
- active Kubernetes key ID hash,
- plugin version.

## Restore OpenBao

Steps:

1. Restore OpenBao to a point containing the required Transit key and historical versions.
2. Verify OpenBao unsealed and healthy.
3. Verify JWT auth method and role configuration.
4. Verify plugin policy.
5. Run `bao-kms-provider verify-key`.
6. Run a controlled probe encrypt/decrypt.
7. Start plugin.
8. Start or restart API server.
9. Validate reads of encrypted Kubernetes resources.

If OpenBao is restored to a point before a Transit rotation but etcd contains data encrypted after that rotation, decrypt can fail.

## Restore etcd And OpenBao Together

Preferred:

1. Select an etcd backup and an OpenBao backup from a compatible time window.
2. Restore OpenBao first.
3. Verify Transit key versions required by the etcd snapshot exist and are decryptable.
4. Restore etcd.
5. Start plugin.
6. Start API server.
7. Validate Kubernetes API reads.

Preserve historical Transit versions for at least as long as any retained etcd backup can reference them.

## Transit Key Loss

If Transit key material is lost and no valid backup exists:

- existing KMS-encrypted Kubernetes data cannot be decrypted,
- `identity` fallback cannot recover it,
- recreating the Transit key with the same name does not recover it,
- the only viable recovery is restoring OpenBao backup with the original key material or restoring etcd to a state that does not require the lost key.

Do not delete encrypted etcd data while investigating.

## Key Recreated With Same Name

Symptoms:

- Transit metadata exists,
- decrypt fails for old ciphertext,
- key lineage ID no longer matches,
- old Kubernetes objects fail to read.

Recovery:

1. Stop the plugin.
2. Restore the original OpenBao key material from backup.
3. Restore the original key lineage configuration.
4. Run `verify-key`.
5. Start plugin.
6. Restart API server.

Do not accept a recreated key as compatible with old data.

## Plugin Config Loss

Steps:

1. Restore config from configuration management.
2. Verify immutable fields match previous values.
3. Restore CA bundle and JWT file.
4. Run `doctor`.
5. Start plugin.
6. Confirm Status key ID hash matches other control-plane nodes or recorded backup metadata.

Changing provider name, cluster ID, OpenBao instance ID, Transit mount ID, key lineage ID, mount path, or key name can cause key ID/AAD mismatches.

## JWT Issuer Loss

If the JWT issuer is unavailable:

- existing OpenBao tokens may continue until expiry,
- re-login fails after token expiry,
- API server startup can fail once decrypt requires a fresh token.

Recovery options:

- restore the external issuer,
- issue a valid replacement JWT through emergency process,
- configure OpenBao JWT auth with pinned public keys if appropriate,
- use a time-limited emergency identity with strong audit trail.

Avoid relying only on a ServiceAccount token from the protected cluster for recovery.

## Control-Plane Node Replacement

Steps:

1. Install plugin binary or preload static pod image.
2. Restore `/etc/openbao-kms/config.yaml`.
3. Restore CA bundle.
4. Provision JWT file.
5. Create `/run/openbao-kms` with safe permissions.
6. Ensure kube-apiserver can access the socket.
7. Run `doctor`.
8. Start plugin before API server.
9. Confirm Status key ID hash matches existing nodes.

## API Server Cannot Start

Recovery order:

1. Do not delete encrypted etcd data.
2. Inspect API server logs for KMS connection/decrypt errors.
3. Restore plugin/socket/OpenBao/JWT first.
4. Run `doctor` locally.
5. Start plugin and verify KMS Status.
6. Restart API server.
7. If OpenBao key material is missing, restore OpenBao backup.
8. If no key backup exists, restore a compatible etcd/OpenBao backup pair.

Do not try to fix KMS ciphertext by changing provider name or recreating Transit keys.

## Single-Node Control Plane

Single-node clusters have higher recovery risk because there is no alternate API server or plugin instance. Prefer systemd mode, local image availability, and tested host-level recovery steps.

## Multi-Node Control Plane

Recover one node at a time:

- keep at least one known-good API server running when possible,
- compare active key ID hashes across nodes,
- avoid simultaneous plugin upgrades,
- avoid simultaneous JWT expiry,
- avoid cluster-wide `min_decryption_version` changes during recovery.

## Source References

- [Kubernetes encryption at rest documentation](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)


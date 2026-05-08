# Rotation Runbook

This runbook describes OpenBao Transit key rotation for the Kubernetes KMS provider.

## Principle

Transit key rotation and Kubernetes storage migration are separate operations.

The plugin observes Transit key versions and exposes a new Kubernetes `key_id` only after the rotation state machine decides the new version is stable. Operators still need to rewrite Kubernetes resources so old encrypted data is updated.

## Before Rotation

Verify:

- OpenBao backup is current.
- etcd backup is current.
- Plugin is healthy on every control-plane node.
- All nodes report the same active key ID hash.
- `doctor` passes on every control-plane node.
- No `identity` fallback remains unexpectedly.
- OpenBao `min_decryption_version` allows all versions still present in etcd and backups.

Record:

- current Kubernetes key ID hash,
- current Transit key version,
- OpenBao backup ID,
- etcd backup ID,
- plugin version,
- control-plane node list.

## Rotate Transit Key

Rotation is performed by an operator with OpenBao administrative rights:

```sh
bao write -f transit/keys/k8s-workload-a-etcd/rotate
```

The plugin must not need rotate permission.

## Observe Promotion

After rotation:

1. The plugin background probe observes the new Transit latest version.
2. The plugin waits for the required stable observation count.
3. The plugin waits for activation delay.
4. The plugin promotes a new active key snapshot.
5. KMS `Status.key_id` changes.
6. New encrypt operations use explicit Transit `key_version` for the new version.

Watch:

```sh
bao-kms-provider rotation-plan --config /etc/openbao-kms/config.yaml
```

Expected state:

- old version remains decryptable,
- new version becomes active once stable,
- all control-plane nodes converge to the same key ID hash,
- no node flips back to the old key ID.

## Migrate Kubernetes Data

Rewrite targeted resources after Status exposes the new key ID.

Example for Secrets:

```sh
kubectl get secrets --all-namespaces -o json | kubectl replace -f -
```

Repeat for each configured resource type.

## Verify Rotation

Run:

```sh
bao-kms-provider verify-rotation --config /etc/openbao-kms/config.yaml
```

Then:

- restart one API server and verify reads,
- verify new writes use the new key ID,
- check plugin decrypt errors,
- check OpenBao decrypt errors,
- check API server encryption metrics where available,
- inspect etcd in a controlled environment if required.

## min_decryption_version

Do not raise `min_decryption_version` until:

- every targeted live object has been rewritten,
- old backups have expired or are known not to need the old version,
- restore testing proves the remaining backup set can decrypt,
- `verify-rotation` confidence is acceptable.

Raising `min_decryption_version` too early can make old Kubernetes data unreadable even though the Transit key still exists.

## Rollback

If new encrypt/decrypt behavior fails before migration:

1. Stop promotion if possible.
2. Keep old Transit key versions decryptable.
3. Restore plugin version/config if needed.
4. Do not delete the new Transit version.
5. Do not recreate the Transit key.
6. Use `doctor` and conformance checks to identify the failing layer.

If objects have already been rewritten with the new version, rollback still requires the new Transit version to remain decryptable.

## Negative Conditions

Stop rotation if:

- nodes report different active key ID hashes,
- Status flips old to new to old,
- unknown key ID decrypt errors appear,
- AAD mismatch errors appear,
- OpenBao metadata reads are inconsistent,
- `min_decryption_version` was changed unexpectedly,
- any control-plane API server cannot restart.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [Kubernetes encryption at rest documentation](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)


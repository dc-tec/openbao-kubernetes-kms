---
title: Rotation
description: "Rotate the Transit key version, wait for every provider to promote it, rewrite existing resources, and retire old versions only when backups no longer need them."
eyebrow: Operate · Key lifecycle
weight: 10
verifiedBy:
  - internal/keyregistry
  - cmd/bao-kms-provider/rotation.go
  - test/e2e/provider_rotation_test.go
---

Kubernetes recommends rotating the key at least every 90 days. Rotation adds a
new version to the existing Transit key. The provider promotes
it only after it has observed the version as stable, and Kubernetes data
written with older versions stays readable until you rewrite it. Rotation never
changes identity-bearing values; see
[Reference: Configuration](/docs/reference/configuration/#identity-bearing-fields).
For why promotion works this way, see
[Architecture: Rotation model](/docs/architecture/rotation-model/).

## Before you begin

- Take current OpenBao and etcd backups.
- Run `doctor` with `--encryption-config` on every control-plane node; it must
  pass, and every node must report the same `openbao_kms_status_key_id_hash`.
- Confirm OpenBao `min_decryption_version` still allows every version present
  in etcd and in retained backups.
- Record the current `key_id` hash, Transit key version, backup IDs, provider
  version, and control-plane node list.

## Step 1: Rotate the Transit key

As an OpenBao administrator, using the values from
[Plan identity values](/docs/get-started/plan-values/):

```sh
bao write -f "${TRANSIT_MOUNT}/keys/${KEY_NAME}/rotate"
```

The provider's own token cannot rotate the key; its policy leaves out that
capability.

## Step 2: Wait for promotion on every node

Each provider observes the new version, waits for
`rotation.requireStableObservationCount` successful observations and then
`rotation.activationDelay`, and promotes it. From then on, KMS
`Status.key_id` changes and new encryptions use the new version. Older
versions stay decryptable.

Follow the state on each control-plane node:

```sh
bao-kms-provider rotation-plan --config /etc/openbao-kms/config.yaml
curl -fsS http://127.0.0.1:8081/metrics \
  | grep -E 'openbao_kms_status_key_id_hash|openbao_kms_key_version|openbao_kms_rotation_state'
```

Trust a `rotation-plan` report only when it exits with `0` and shows
`transitMetadataStatus: pass`. If authentication or the metadata read fails,
it exits with `4`, and any local state it still prints says nothing about the
current OpenBao state; fix connectivity or permissions and run it again.

Promotion is complete when every node reports the same new `key_id` hash and
`openbao_kms_rotation_state{state="active"}`. `rotation-plan` shows the reason
and timing while a version is pending.

Stop rotating and go to [Troubleshooting](/docs/operate/troubleshooting/) if:

- nodes report different `key_id` hashes, or a node flips back to the old one,
- another Transit rotation happens before every node has converged,
- unknown `key_id` or AAD mismatch errors appear,
- OpenBao is missing creation metadata for an intermediate version,
- an API server cannot restart cleanly.

If `latest_version` skips versions, the provider needs OpenBao's creation
metadata for each skipped version and fails closed without it, because another
node might already have encrypted with one of them. If you raise
`min_encryption_version` before every node has promoted, Status and readiness
turn unhealthy and encryption stops until promotion completes; raise it only
after convergence.

## Step 3: Rewrite existing resources

After every node reports the new `key_id`, rewrite each resource type listed
in the `EncryptionConfiguration`. For Secrets:

```sh
kubectl get secrets --all-namespaces -o json | kubectl replace -f -
```

Keep the resource list, command output, and timestamps with the rotation
record.

## Step 4: Verify

```sh
bao-kms-provider verify-rotation --config /etc/openbao-kms/config.yaml
```

`verify-rotation` checks only the provider's local registry against Transit
metadata, and like `rotation-plan` exits with `4` when the metadata check
fails. It does not scan Kubernetes objects, etcd, or backups, so also:

- restart one API server and confirm reads succeed,
- confirm new writes carry the new `key_id` and every resource type was
  rewritten,
- compare `openbao_kms_status_key_id_hash` and the decrypt-error metrics on
  every node, and check the OpenBao decrypt error rate,
- confirm retained backups either still have their Transit versions available
  or no longer need them.

For the metrics and logs, see [Reference: Observability](/docs/reference/observability/).

## Retire old versions

Raising OpenBao `min_decryption_version` makes older data permanently
unreadable once no rewritten copy exists. Do it only when:

- every targeted object has been rewritten,
- every backup that needs the old versions has expired,
- a restore test proves the remaining backups decrypt,
- a reviewed change record names the versions that stay and the rollback plan.

No command proves these conditions for you; the decision stays with you.

Before raising the minimum, retire the old versions from every provider's
local registry. This example keeps version `2` and later.

1. Confirm every provider has promoted version `2` or later with no rotation
   pending, and pause Transit rotation.
2. On each node, generate a plan as the provider's OS user:

   ```sh
   bao-kms-provider retire-versions \
     --config /etc/openbao-kms/config.yaml \
     --before-version 2 --output json
   ```

3. Review `removedVersions`, the unchanged `activeKeyIdHash`, and
   `nextStateHash`, and keep the output with the change record.
4. On one node at a time, stop the provider. For a static pod, stop kubelet
   from restarting it. The local API server cannot decrypt while the provider
   is stopped.
5. Apply the reviewed plan as the same OS user:

   ```sh
   bao-kms-provider retire-versions \
     --config /etc/openbao-kms/config.yaml \
     --before-version 2 --apply \
     --expected-state-hash '<stateHash from this node’s reviewed plan>'
   ```

6. Start the provider, confirm readiness, the active `key_id`, and reads and
   writes, then continue with the next node. If the state hash changed,
   generate and review a new plan first.
7. Back up each node's state and checkpoint files. Then raise
   `min_decryption_version` through your change process and check readiness,
   reads, and writes again.

{{< callout type="warning" title="Retirement ends local decryption immediately" >}}
A retired version stops decrypting on that node immediately, even while
OpenBao still allows it, and lowering the OpenBao minimum does not bring it
back. The command does not delete Transit key material or change OpenBao
settings. Recover an accidental local removal with the reviewed restoration
procedure below, while matching Transit key material remains available.
{{< /callout >}}

### Recover an out-of-order retirement

If the OpenBao minimum was raised before local retirement, the provider rejects
unusable historical records. `retire-versions` can remove those records even
when a newer Transit version or pending local rotation exists. It preserves the
active and pending records and requires every retained version to remain
decryptable with matching creation metadata. Normal activation checks still
control promotion and encryption after restart.

Review the same data, backup, and peer evidence before applying this recovery.
The command cannot prove that other nodes stopped writing with a removed key.
Waiting an activation interval does not establish peer convergence. If the
active or pending version is no longer decryptable, correct the backend
restriction first; trimmed or deleted key material requires a valid backup.

### Restore an accidentally removed version

Stop the affected provider and run as its OS user. Select exact versions from
the local `removed` records. Generate a read-only plan:

```sh
bao-kms-provider restore-versions \
  --config /etc/openbao-kms/config.yaml --versions 1 --output json
```

Review `restoredVersions`, `activeKeyIdHash`, `stateHash`, and `nextStateHash`.
Confirm the intended cluster, Transit key, and version history, then apply:

```sh
bao-kms-provider restore-versions \
  --config /etc/openbao-kms/config.yaml --versions 1 --apply \
  --expected-state-hash '<stateHash from this node’s reviewed plan>'
```

Restoration changes only selected historical records from `removed` to `retired`.
It advances the current state/checkpoint chain, preserves the active identity,
and verifies current backend restrictions and version creation metadata again
at apply time. It never rolls back state, changes OpenBao settings, or recreates
key material. Automatic discovery cannot restore removed versions.

Restart the provider and verify retained ciphertext reads through each API
server. Back up the new state/checkpoint pair. If a backend restriction still
blocks a selected version or its creation metadata differs, restoration fails
without changing the pair. Do not erase state to bypass that failure.

`serve`, `retire-versions --apply`, and `restore-versions --apply` share the lock file
`<state.path>.lock`; never delete it while any of them runs. If saving fails, inspect
the state before retrying. Never restore one file of the state and checkpoint
pair alone, and never edit either by hand.

## Roll back

If encryption or decryption fails before the rewrite completes:

1. Stop further rotations, and compare active and pending identities on all
   nodes. A peer might already encrypt with a version that is still pending
   locally.
2. Keep every Transit version decryptable. Do not raise
   `min_decryption_version`, delete the new version, or recreate the key.
3. Before restoring a previous provider binary, confirm it can decrypt every
   version in use; see
   [Reference: Compatibility](/docs/reference/compatibility/#unreleased-rotation-corrections).
4. Use `doctor`, `rotation-plan`, and
   [Reference: Observability](/docs/reference/observability/) to find the
   failing layer.

Objects already rewritten with the new version always need that version to
stay decryptable.

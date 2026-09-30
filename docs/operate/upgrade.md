---
title: Upgrade
description: "Upgrade the provider one control-plane node at a time, confirm each node rejoins with the same key, and roll back only when the older release can read every stored format."
eyebrow: Operate · Change management
weight: 30
verifiedBy:
  - test/deployment/systemd-install.sh
  - test/e2e/provider_upgrade_test.go
---

`0.1.0-preview.3` requires a fresh disposable installation. Do not use this
procedure to upgrade an earlier preview: its unbound registry state is rejected.
Keep the old state, checkpoint, and key material intact. See
[Compatibility](/docs/reference/compatibility/#preview3-fresh-installation-boundary).

Use the procedure below only when the target release documents support for
the installed release's configuration and persisted state.
Every provider sits on its API server's boot path, so upgrade one control-plane
node at a time and confirm each one before moving on. For wire-format promises
between releases, see [Reference: Compatibility](/docs/reference/compatibility/).

## Before you begin

Defer the upgrade while a Transit rotation is pending, OpenBao is failing over
or restoring, or the release notes announce a wire-format change you have not
planned a migration for. Otherwise:

- download and verify the new release as in
  [Download the release](/docs/get-started/download/),
- confirm `rotation-plan` shows no pending rotation and every node reports the
  same `key_id` hash,
- confirm `doctor` with `--encryption-config` passes on every node,
- keep the current package or image on every node for rollback,
- record the current provider version, `key_id` hash, and Transit key version.

## Upgrade each node

1. Run `doctor` with the new binary or image and resolve any failed check:

   ```sh
   bao-kms-provider doctor \
     --config /etc/openbao-kms/config.yaml \
     --encryption-config /etc/kubernetes/openbao-kms/encryption-config.yaml
   ```

2. Install the new release. For systemd, install the new package or tarball
   and run `systemctl restart bao-kms-provider.service`. For a static pod, set
   the new verified image digest in `/etc/kubernetes/manifests/bao-kms-provider.yaml`
   after preloading it.
3. Confirm `/ready` returns HTTP 200 and the `key_id` hash matches the other
   nodes:

   ```sh
   curl -fsS http://127.0.0.1:8082/ready
   curl -fsS http://127.0.0.1:8081/metrics | grep openbao_kms_status_key_id_hash
   ```

4. Restart the local API server only if the release notes require it.

After the last node, confirm every node reports the same `key_id` hash.

## Roll back

Roll back only if the older release can read the current registry schema and
every `key_id`, annotation, and
AAD format now in etcd; otherwise decryption fails with unknown `key_id`
errors. Never roll back when:

- the new release wrote data in a new wire format,
- a rotation completed under the new release that the older release never
  promoted,
- the release notes call the upgrade one-way.

In those cases, fix forward with a patch release.

To roll back, repeat the node procedure with the previous package or image
digest, running `doctor` with the previous binary before starting it. If
unknown `key_id` errors appear, return to the newer release and follow
[Troubleshooting: Unknown key ID](/docs/operate/troubleshooting/#unknown-key-id).

## Migrate a static-pod JWT file mount

For manifests that mount `/var/lib/openbao-kms/identity.jwt` as a file, update
one control-plane node at a time. Schedule an API outage for a single-node
control plane. The provider must restart once to change its mounts.

1. Create `/var/lib/openbao-kms/credentials` with the permissions in
   [Run as a static pod, Step 4](/docs/get-started/static-pod/#step-4-prepare-the-host).
2. Configure the host issuer agent to publish a current JWT as
   `/var/lib/openbao-kms/credentials/identity.jwt` with the permissions in
   [Run as a static pod, Step 6](/docs/get-started/static-pod/#step-6-place-the-runtime-files).
3. Change `auth.jwt.jwtFile` in the host configuration to the new path. Preserve
   all identity values and the state directory.
4. Update both the JWT volume and its mount to the credential directory,
   with hostPath type `Directory` and a read-only mount. Regenerate the manifest
   with `init --model static-pod` or use the current sample. The generator
   rejects credential directories that overlap state or socket directories.
5. Install the updated configuration and manifest. Wait for the new container
   and HTTP 200 from `/ready`. Verify an existing encrypted resource remains
   readable before continuing to the next node.

Subsequent atomic JWT replacements do not require a provider restart. This
path change does not change key IDs, AAD, Transit keys, or encrypted data.
Existing systemd JWT paths remain supported.

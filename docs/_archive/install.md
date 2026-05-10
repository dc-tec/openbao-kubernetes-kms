# Install

This document defines the expected installation model and points at the WS10 sample artifacts.

## Status

No published release exists yet. The repository now includes sample build and deployment artifacts:

- `Dockerfile` for the `bao-kms-provider` container image,
- `deploy/config/provider-systemd.yaml`,
- `deploy/config/provider-static-pod.yaml`,
- `deploy/systemd/bao-kms-provider.service`,
- `deploy/static-pod/bao-kms-provider.yaml`,
- `deploy/kubernetes/encryption-config.yaml`,
- `deploy/package/linux` package layout snippets,
- `deploy/opentofu/openbao-kubernetes-kms` OpenBao Transit and policy setup skeleton.

## Host Layout

Recommended paths:

```text
/usr/bin/bao-kms-provider
/etc/openbao-kms/config.yaml
/etc/openbao-kms/tls/ca.crt
/var/lib/openbao-kms/identity.jwt
/run/openbao-kms/kms.sock
```

Recommended ownership:

```text
/usr/bin/bao-kms-provider          root:root 0755
/etc/openbao-kms                       root:root 0750
/etc/openbao-kms/config.yaml           root:openbao-kms 0640
/etc/openbao-kms/tls/ca.crt            root:root 0644
/var/lib/openbao-kms                   openbao-kms:openbao-kms 0750
/var/lib/openbao-kms/identity.jwt      root:openbao-kms 0640
/var/lib/openbao-kms/state             openbao-kms:openbao-kms 0750
/run/openbao-kms                       openbao-kms:openbao-kms-socket 2750
```

The actual service user/group names may vary by distribution. The API server must be able to connect to the socket; it does not need access to the JWT file.

Static pod mode should use a numeric `server.socketGroup` value that matches the host socket group GID. The container image runs as distroless non-root `65532:65532`.

## Prerequisites

Required:

- Kubernetes control-plane nodes running a supported Kubernetes version.
- OpenBao reachable over HTTPS.
- OpenBao Transit mount, key, and provider policy created manually or through the OpenTofu module.
- OpenBao JWT auth method configured.
- CA bundle for OpenBao TLS verification.
- Host-provisioned JWT file for plugin authentication.
- Kubernetes `EncryptionConfiguration` using KMS v2.

Recommended:

- OpenBao outside the protected cluster dependency path.
- External configuration management for plugin config and JWT provisioning.
- Tested etcd and OpenBao backups.
- Time synchronization on control-plane nodes, OpenBao nodes, and JWT issuer.

## Install Order

1. Configure OpenBao Transit and JWT auth.
2. Install the plugin binary or static pod image on every control-plane node.
3. Place `/etc/openbao-kms/config.yaml`.
4. Place the OpenBao CA bundle.
5. Provision the JWT file.
6. Create the runtime socket directory.
7. Run `bao-kms-provider doctor`.
8. Start the plugin.
9. Configure kube-apiserver with KMS v2.
10. Restart or reload kube-apiserver as required.
11. Verify encrypted resource creation and reads.
12. Migrate existing resources.
13. Remove `identity` fallback after migration verification.

For local image builds:

```sh
make image-smoke
```

The image build uses a pinned Go builder base and a pinned distroless non-root runtime base from `.ci/versions.yaml`.

## Bootstrap Check

Before changing kube-apiserver encryption config, run:

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/encryption-config.yaml
```

Minimum expected checks:

- config permissions safe,
- JWT readable and valid,
- OpenBao TLS valid,
- JWT login succeeds,
- Transit key metadata valid,
- probe encrypt/decrypt succeeds,
- generated key ID stable,
- socket path safe,
- provider name matches encryption config.

## Upgrade Model

Upgrade one control-plane node at a time.

Recommended sequence:

1. Run `doctor` with the new binary/image.
2. Stop plugin on one node.
3. Upgrade binary/image.
4. Start plugin.
5. Verify KMS Status and active key ID hash.
6. Restart the local API server only if required.
7. Repeat for the next node.

Do not upgrade all plugin instances simultaneously unless the cluster is in a controlled maintenance window and OpenBao/API server recovery has been tested.

## Rollback Model

Rollback is safe only when the older version understands all active key ID, annotation, and AAD formats currently present in etcd.

Before rollback:

- verify key ID/AAD format compatibility,
- check release notes for wire-format changes,
- confirm the older version can decrypt current objects,
- keep the current binary/image available until rollback is verified.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [Kubernetes encryption at rest documentation](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)

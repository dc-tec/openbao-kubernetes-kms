# Research Notes

This document records upstream facts checked while preparing the documentation set. It is not a substitute for implementation tests.

Checked on 2026-05-08.

## Kubernetes KMS

Verified from Kubernetes documentation:

- Kubernetes recommends KMS v2 for current clusters.
- KMS v2 is stable from Kubernetes 1.29.
- KMS v1 is deprecated and disabled by default in Kubernetes 1.29 and later.
- The KMS plugin is a gRPC server reached by kube-apiserver over a Unix domain socket.
- KMS v2 provider configuration uses `apiVersion: v2`, `name`, `endpoint`, and `timeout`.
- KMS v2 does not support the old `cachesize` property.
- KMS v2 uses `Status` as the active health and key ID signal.
- `EncryptResponse.key_id` must match the key ID observed through Status, or Kubernetes discards the encrypt result and treats the plugin as unhealthy.
- KMS v2 supports annotations as metadata returned by encrypt and supplied back during decrypt.
- `DecryptRequest` includes ciphertext, key ID, annotations, and UID.

Design impact:

- KMS v2 only by default.
- Cached Status is required.
- Status/encrypt key ID consistency is a release gate.
- Decrypt validates key ID and annotations before calling Transit.

## Kubernetes Encryption At Rest

Verified from Kubernetes documentation:

- Kubernetes encryption-at-rest applies to configured API resources persisted to etcd.
- KMS-backed encryption uses envelope encryption.
- Local static keys protect against offline etcd compromise but not host compromise.
- KMS keeps the key encryption key outside Kubernetes.
- The `identity` provider does not encrypt stored data.
- Encryption applies on write; existing resources must be rewritten to change stored encryption state.
- Kubernetes documents the common `kubectl get ... -o json | kubectl replace -f -` migration pattern.
- Automatic encryption provider config reload can be enabled with the kube-apiserver reload flag.

Design impact:

- Docs distinguish API resource encryption from disk, volume, and traffic encryption.
- Initial migration docs include temporary `identity` fallback.
- Rotation docs require storage migration after key ID change.
- DR docs warn that `identity` fallback cannot decrypt existing KMS ciphertext.

## Kubernetes Static Pods

Verified from Kubernetes documentation:

- Static pod specs cannot refer to API objects such as ServiceAccounts, ConfigMaps, or Secrets.
- kubelet watches a host directory for static pod manifests.

Design impact:

- Static pod docs mount config, CA, JWT, socket directory, and state from the host.
- Static pod deployment is not allowed to depend on Kubernetes Secrets or ConfigMaps.

## OpenBao Transit

Verified from OpenBao documentation:

- Transit does not store caller plaintext or ciphertext.
- Transit encrypt accepts base64 plaintext.
- Transit encrypt supports explicit `key_version`.
- Transit encrypt/decrypt support `associated_data` for compatible AEAD ciphers.
- Transit exposes key settings including exportability, plaintext backup, deletion, and version restrictions.
- `min_encryption_version` and `min_decryption_version` restrict key version use.
- Transit global key config supports `disable_upsert` to prevent automatic key creation on encrypt.
- Exportable and plaintext backup settings are security-sensitive and cannot simply be undone after enabling.

Design impact:

- Encrypt always sends explicit `key_version`.
- AAD is required for v0.1 deployments.
- `verify-key` checks key profile and version restrictions.
- OpenBao policy excludes create, rotate, export, backup, and delete capabilities.
- `disable_upsert` is part of the OpenBao setup guide.

## OpenBao JWT Auth

Verified from OpenBao documentation:

- OpenBao JWT auth can validate JWTs without OIDC browser flow.
- JWT roles can bind subject and audience.
- Roles can also bind arbitrary claims and map claims into token metadata.

Design impact:

- JWT-first auth remains a good default.
- OpenBao role examples include bound audience and subject.
- DR docs warn against relying only on a JWT issuer that depends on the protected API server.

## OpenBao Operator CI Pattern

Verified from the local OpenBao Operator repository:

- CI uses local `make` targets for parity with workflow gates.
- E2E lane metadata is centralized in a suite manifest instead of duplicated across workflows.
- OpenBao and Kubernetes validation versions are pinned, not floating `latest`.
- The operator uses explicit CI lanes, nightly profiles, and release-gate profiles.
- Supply-chain controls include vendored Go builds, license checks, static security scans, image scans, SBOMs, provenance, signing, and reproducibility checks.
- Release documentation separates compatibility, release policy, support policy, release management, distribution, and supply-chain security.

Design impact:

- Added [CI and supply chain](ci-supply-chain.md).
- Added [Release policy](release-policy.md) and [Support policy](support-policy.md).
- Updated compatibility to OpenBao `2.5.3` and Kubernetes `1.34` release-line validation.
- Added ADR for exact-pinned CI version matrix.
- Updated workstreams so CI/supply-chain work starts early instead of being a final release polish task.

## Source Links

- [Kubernetes: Using a KMS provider for data encryption](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [Kubernetes: Encrypting confidential data at rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [Kubernetes: Static Pods](https://kubernetes.io/docs/tasks/configure-pod-container/static-pod/)
- [OpenBao: Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao: Transit documentation](https://openbao.org/docs/secrets/transit/)
- [OpenBao: JWT auth](https://openbao.org/docs/2.4.x/auth/jwt/)
- Local reference: `/Users/roelc/projects/work/openbao-operator/docs/contribute/ci.md`
- Local reference: `/Users/roelc/projects/work/openbao-operator/docs/contribute/supply-chain-security.md`
- Local reference: `/Users/roelc/projects/work/openbao-operator/docs/reference/compatibility.md`

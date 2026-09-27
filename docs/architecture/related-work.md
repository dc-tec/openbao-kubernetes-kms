---
title: Related work
description: "A revision-pinned comparison with the Vault Transit KMS plugin, including identity, authentication, startup, and recovery tradeoffs."
eyebrow: Architecture
weight: 60
verifiedBy:
  - internal/kmsv2/server.go
  - internal/scaffold/staticpod.go
  - internal/socket/listener.go
  - docs/reference/compatibility.md
---

The closest related project is
[`FalcoSuessgott/vault-kubernetes-kms`](https://github.com/FalcoSuessgott/vault-kubernetes-kms),
a Kubernetes KMS plugin for HashiCorp Vault Transit. Its deployment model
informed this project's treatment of bootstrap dependencies, local sockets,
token renewal, and observability.

This comparison was reviewed on **27 September 2026** against upstream
[`v1.4.0`, commit `a4ce7ca`](https://github.com/FalcoSuessgott/vault-kubernetes-kms/tree/a4ce7ca04aa211fefa6610dd2d695b89f9c65a32).
It compares source behavior, not deployment history or release qualification.
For `bao-kms-provider`, support is limited to the tested matrix and artifacts
listed in [Compatibility](/docs/reference/compatibility/) for the selected
release. Changes marked unreleased on that page need their own qualification.

## KMS and identity

`bao-kms-provider` is a separate implementation with an OpenBao-specific
configuration, policy, and identity contract.

| Area | `bao-kms-provider` | Compared Vault provider |
|---|---|---|
| KMS API | KMS v2 only | KMS v1 and v2 |
| Status | Reads cached health and the active `key_id`; background probes have bounded freshness | Reads key metadata and performs an encrypt/decrypt health check |
| Encrypt | Takes `key_id`, AAD, and an explicit Transit `key_version` from one active snapshot | Encrypts with implicit latest, then reads the latest version for `key_id` |
| Identity | A scoped hash includes key lineage and version identity | The Transit version number identifies the key |
| Ciphertext context | Required additional authenticated data (AAD) binds the configured provider, cluster, instance, lineage, and version | The Transit request does not supply AAD |
| Local socket | Refuses unsafe paths and live or uncertain listeners; removes only verified stale sockets | An optional force flag removes an existing socket path |

Upstream source: [KMS v2 service](https://github.com/FalcoSuessgott/vault-kubernetes-kms/blob/a4ce7ca04aa211fefa6610dd2d695b89f9c65a32/pkg/plugin/plugin_v2.go),
[Transit client](https://github.com/FalcoSuessgott/vault-kubernetes-kms/blob/a4ce7ca04aa211fefa6610dd2d695b89f9c65a32/pkg/vault/transit.go),
and [socket handling](https://github.com/FalcoSuessgott/vault-kubernetes-kms/blob/a4ce7ca04aa211fefa6610dd2d695b89f9c65a32/pkg/socket/socket.go).

An explicit version keeps encryption and its reported identity consistent
when Transit rotates during a request. Required AAD is this project's
additional binding contract; KMS v2 does not require every plugin to use it.
Hashed identifiers avoid directly exposing configured names, but do not make
public identity inputs secret.

## Authentication and startup

The compared provider supports token, AppRole, userpass, file certificate,
file JWT, and native SPIFFE JWT-SVID authentication. Its
[JWT source](https://github.com/FalcoSuessgott/vault-kubernetes-kms/blob/a4ce7ca04aa211fefa6610dd2d695b89f9c65a32/pkg/vault/jwt.go)
can request a JWT-SVID from the SPIFFE Workload API.

`bao-kms-provider` supports file JWTs and native OAuth client credentials.
PKCS#11 certificate authentication is limited to releases that publish and
qualify the corresponding artifact. Native SPIFFE JWT-SVID acquisition is
not implemented here. The SPIFFE certificate source is a different integration
and remains unsupported.

JWT authentication in either implementation can avoid TokenReview against
the protected Kubernetes API server. The issuer, OpenBao, and the path that
renews host credentials must remain available independently of that server.

Our systemd deployment lets the provider start without kubelet or the
container runtime. Its unit orders process execution, not provider readiness.
The API server can start while the provider bootstraps and must retry.
Static pods add kubelet, the runtime, and a preloaded image to the provider's
dependencies. See [Startup](/docs/architecture/overview/#startup).

## Recovery and migration

Our registry preserves accepted identities and rotation decisions across
restarts. It supplies monotonicity checks and historical identity validation,
but adds recovery state. The compared provider has no equivalent local
identity registry.

After rotation, our state and checkpoint are not disposable caches. Preserve
them with compatible identity configuration, OpenBao key history, and etcd
backups. The checkpoint detects inconsistent restores; it cannot prevent a
privileged administrator from replacing both files. Follow
[Disaster recovery](/docs/operate/disaster-recovery/).

The providers' key IDs, annotations, and AAD contracts differ. Replacing the
binary against the same Transit key does not establish ciphertext
compatibility. A migration must retain the old decrypt path while resources
are rewritten through the new provider. Verify readback after an API server
restart and keep old keys and backups until the migration is complete. See
[Encryption configuration](/docs/reference/encryption-config/#migration-files).

Both projects have automated tests and release signing. Those controls do
not establish support for an untested combination or prove equivalent
recovery behavior. Use the selected release's evidence for operational claims.

---
title: "Related Work"
description: "Existing Vault Transit Kubernetes KMS plugin work that informed bao-kms-provider, and the project-specific design boundaries this implementation chooses."
weight: 60
---

# Related Work

The closest existing project in this space is [`FalcoSuessgott/vault-kubernetes-kms`](https://github.com/FalcoSuessgott/vault-kubernetes-kms), a Kubernetes KMS plugin that integrates with HashiCorp Vault Transit. It is useful related work for this project because it demonstrates that a Transit-backed KMS plugin can operate in real Kubernetes control-plane deployments.

The surrounding ecosystem influenced this design, but project constraints led to a separate OpenBao-native implementation. This record identifies that lineage and explains the project's release boundary.

## Design Influences

The existing Vault Transit plugin work reinforced several choices in this project:

- the KMS plugin must be available before `kube-apiserver` can reliably start with encrypted data,
- static-pod deployment needs special care because static pods cannot depend on ConfigMaps, Secrets, or ServiceAccounts,
- the remote Transit service should be reachable independently of the protected Kubernetes API server,
- Unix socket placement and permissions are part of the security boundary,
- token renewal and runtime observability are operational requirements,
- KMS v2 is the default target for current Kubernetes clusters.

Those lessons are reflected in the deployment, hardening, and operations docs for this provider.

## Project-Specific Boundaries

`bao-kms-provider` makes these explicit design choices:

| Boundary | Choice in `bao-kms-provider` | Reason |
|---|---|---|
| KMS API | KMS v2 only. | Keeps the implementation focused on the stable current Kubernetes contract. |
| OpenBao integration | OpenBao-native naming, configuration, policy examples, and operational docs. | Keeps the public contract specific to OpenBao. |
| Authentication | JSON Web Token (JWT) auth by default, with certificate auth backed by a PKCS#11 hardware or software token. | Avoids TokenReview dependency on the protected API server during bootstrap and recovery. |
| Deployment model | systemd is the recommended default when operators control the host OS; static pod remains supported. | systemd removes kubelet and container runtime from the provider boot path. |
| Status path | Status returns cached health and active `key_id`. | Kubernetes polls Status continually, so live Transit encrypt/decrypt work belongs in background probes. |
| Encrypt versioning | Encrypt passes an explicit Transit `key_version` from the active snapshot. | Avoids implicit-latest races during Transit rotation. |
| Kubernetes `key_id` | Opaque scoped `key_id`, never a raw Transit version or key name. | Avoids topology leakage and preserves Kubernetes key-rotation invariants. |
| AAD and annotations | Requires additional authenticated data (AAD) metadata and rejects non-required AAD modes. | Binds ciphertext to provider, cluster, OpenBao instance, key lineage, and key version. |
| Socket handling | Unsafe paths fail closed; only verified-dead Unix sockets are removed. | Prevents accidental or malicious socket path replacement. |
| Recovery docs | Disaster recovery, rotation, and troubleshooting are first-class docs. | KMS failures can block API server startup, so operators need runbooks before incidents. |

## Shared Operating Lessons

Where the projects align, the alignment is intentional:

- a Transit-backed KMS plugin is a practical model for Kubernetes encryption at rest,
- the plugin must run on every control-plane node that hosts `kube-apiserver`,
- the API server talks to the plugin over a local Unix domain socket,
- the remote Transit service belongs outside the protected API-server dependency path,
- deployment manifests must be designed for early control-plane boot.

`bao-kms-provider` builds from those shared lessons while narrowing the implementation around OpenBao, KMS v2, provider auth without TokenReview, explicit key-version handling, AAD-backed decrypt validation, and documented release evidence.

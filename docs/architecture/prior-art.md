---
title: "Prior Art"
description: "Comparison between bao-kms-provider and the existing FalcoSuessgott vault-kubernetes-kms project, and why an OpenBao-native rewrite is preferred."
weight: 60
---

# Prior Art

The closest existing project in this space is [`FalcoSuessgott/vault-kubernetes-kms`](https://github.com/FalcoSuessgott/vault-kubernetes-kms), a Kubernetes KMS plugin that integrates with HashiCorp Vault Transit. It is a useful proof point that Kubernetes KMS plugins can be built on Vault Transit, and the project's README and history informed several design choices in `bao-kms-provider`.

This page records the differences between that project and this OpenBao-native design and the reasons for not cloning the existing implementation.

## Existing Project Overview

The current FalcoSuessgott project description:

- a plugin for encrypting Kubernetes etcd objects with HashiCorp Vault Transit,
- intended to run as a static pod on every control-plane node,
- documents that the plugin must be running before the API server starts,
- recommends placing Vault outside the protected cluster when the plugin is deployed as a static pod,
- advertises support for Vault token, AppRole, and userpass authentication,
- registers KMS v1 and KMS v2,
- supports automatic token renewal,
- exposes Prometheus metrics.

The repository release page shows recent development activity, with a v1.1.0 release in April 2026 adding userpass authentication and dependency updates.

## Why Not Reuse The Existing Project

The OpenBao-native design does not clone that implementation. Key differences:

| Area | Existing project behavior | `bao-kms-provider` behavior |
|---|---|---|
| KMS API | Registers KMS v1 and KMS v2 unless disabled. | KMS v2 only by default; KMS v1 reserved for a separate legacy mode if ever needed. |
| Auth | Supports token, AppRole, and userpass in code; no JWT-first design. | JWT-first with file reload, short OpenBao tokens, and re-login or renewal. See [Security: Auth Model](/security/auth-model/). |
| Client identity | Uses HashiCorp Vault API client and Vault naming. | OpenBao-native naming, documentation, and client integration. |
| Status | KMS v2 Status calls health logic that performs real encrypt and decrypt work. | Cheap cached Status; background probes perform OpenBao checks. See [Architecture: Overview: Status](/architecture/overview/#status). |
| Encrypt key version | Encrypt writes plaintext to Transit and then reads the latest key version. | Encrypt passes an explicit Transit `key_version` from the active KeySnapshot. |
| `key_id` | Uses Transit `latest_version` string as the KMS key ID. | Opaque scoped `key_id`; no raw Transit version or key topology leakage. See [Reference: Key ID And AAD](/reference/key-id-and-aad/). |
| Decrypt validation | The current KMS v2 wrapper does not pass request `key_id` or annotations into internal decrypt logic. | Decrypt validates request `key_id`, annotations, and required v0.1 AAD. See [Security: AAD And Decrypt Validation](/security/aad-and-decrypt-validation/). |
| Annotations | KMS v2 encrypt response does not populate annotations. | KMS v2 annotations carry non-secret metadata for AAD reconstruction. |
| Socket safety | A force-socket-overwrite path removes an existing socket path when enabled. | Removes only verified-dead Unix sockets; fails closed on unsafe paths. |
| Recovery documentation | Troubleshooting refers rollback to Kubernetes documentation. | Detailed disaster recovery and bootstrap playbooks. See [Operations: Disaster Recovery](/operations/disaster-recovery/). |

## Maturity Signal

The existing project's documentation is internally inconsistent on maturity. The README describes the project as stable and production-grade; a separate documentation page still says the project is in an early stage and not recommended for production. Both statements may be partially true depending on environment.

Either way, the inconsistency reinforces the case for an independent OpenBao-native design with its own threat model, test plan, and release-gate definition. `bao-kms-provider` ships with explicit [Reference: Support Policy](/reference/support-policy/) and [Reference: Release Policy](/reference/release-policy/) so adopters know exactly what is and is not validated.

## What This Project Owes To Prior Art

The differences above are not a critique of FalcoSuessgott's project; the project is older, has solved real problems, and exists in a different ecosystem. `bao-kms-provider` benefits from that work in several ways:

- it confirms Kubernetes KMS v2 plugins on Transit are viable in production,
- it validated several deployment patterns (static-pod first, Vault outside the protected cluster) that this design also adopts,
- it surfaced concrete gaps (Status doing live encrypt, raw Transit version as `key_id`, missing annotations) that this design addresses explicitly.

Where the existing project and this design align (KMS v2 plugin model, Unix socket transport, OpenBao or Vault outside the protected cluster), the alignment is intentional and useful. The differences above are where the OpenBao-native design takes a different position.

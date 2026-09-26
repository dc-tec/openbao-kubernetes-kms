---
title: Related work
description: "The Vault Transit KMS plugin that informed this project, the lessons both share, and where bao-kms-provider draws different boundaries."
eyebrow: Architecture
weight: 60
---

The closest related project is
[`FalcoSuessgott/vault-kubernetes-kms`](https://github.com/FalcoSuessgott/vault-kubernetes-kms),
a Kubernetes KMS plugin for HashiCorp Vault Transit. It shows that a
Transit-backed plugin works in real control planes, and shaped several choices
here: the plugin must be up before `kube-apiserver` can start with encrypted
data, static pods need special care, Transit must stay reachable without the
protected API server, socket placement is part of the security boundary, and
token renewal and observability are operational requirements.

`bao-kms-provider` is a separate, OpenBao-native implementation with these
boundaries:

| Boundary | Choice | Reason |
|---|---|---|
| KMS API | KMS v2 only | Focus on the stable Kubernetes contract. |
| Integration | OpenBao-native naming, configuration, and policy | A contract specific to OpenBao. |
| Authentication | JWT by default, PKCS#11 certificate auth as an option | No TokenReview dependency on the protected API server. |
| Deployment | systemd by default, static pod supported | systemd keeps kubelet and the container runtime off the boot path. |
| Status | Cached health and `key_id` | Kubernetes polls Status continually; live Transit work belongs in background probes. |
| Encrypt | Explicit Transit `key_version` | No implicit-latest race during rotation. |
| `key_id` | Opaque, scoped hash | No topology leakage; stable across restarts. |
| AAD | Always required | Binds ciphertext to provider, cluster, instance, lineage, and version. |
| Socket | Unsafe paths fail closed; only verified-dead sockets removed | No socket path replacement. |
| Recovery | Rotation, recovery, and troubleshooting runbooks | KMS failures can block API server startup. |

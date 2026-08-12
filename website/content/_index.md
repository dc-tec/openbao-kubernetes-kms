---
title: "OpenBao Kubernetes KMS Provider"
description: "Encrypt Kubernetes API resources at rest with OpenBao Transit through a Kubernetes KMS v2 provider."
hero_label: "Documentation"
primary_href: "getting-started/overview/"
primary_label: "Start Here"
secondary_href: "architecture/overview/"
secondary_label: "Read Architecture"
---

The `bao-kms-provider` process implements the Kubernetes Key Management Service
(KMS) v2 protocol on a local Unix socket. It forwards encrypt and decrypt
operations to OpenBao Transit. Kubernetes uses the provider for envelope
encryption of selected API resources before persisting them to etcd. The
provider does not encrypt etcd disk blocks, application volumes, or node
filesystems.

Operators usually move through [Start Here](/getting-started/),
[Deployment](/deployment/), [Operations](/operations/),
[Reference](/reference/), and [Security](/security/). Maintainers should use
[Architecture](/architecture/) for design rationale and trust boundaries.
Contributors should use [Development](/development/) for local workflow,
continuous integration, and release process.

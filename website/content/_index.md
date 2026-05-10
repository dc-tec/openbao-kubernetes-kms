---
title: "OpenBao Kubernetes KMS Provider"
description: "Encrypt Kubernetes API resources at rest with OpenBao Transit through a hardened, KMS v2 provider plugin."
hero_label: "Documentation"
primary_href: "getting-started/overview/"
primary_label: "Start Here"
secondary_href: "architecture/overview/"
secondary_label: "Read Architecture"
---

The `bao-kms-provider` plugin terminates the Kubernetes KMS v2 protocol on a local Unix socket and forwards encrypt and decrypt operations to OpenBao Transit. It participates in Kubernetes envelope encryption for selected API resources before they are persisted to etcd. It does not encrypt etcd disk blocks, application volumes, or node filesystems.

Operators usually move through [Start Here](/getting-started/), [Deployment](/deployment/), [Operations](/operations/), [Reference](/reference/), and [Security](/security/). Maintainers should use [Architecture](/architecture/) for design rationale and trust boundaries, while contributors should use [Development](/development/) for local workflow, CI, and release process.

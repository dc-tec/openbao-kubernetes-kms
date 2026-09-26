---
title: "Reference"
description: "Exact behavior, configuration shape, KMS v2 contract, key_id and additional authenticated data format, observability surface, and policy boundaries."
weight: 40
---

These pages answer behavior-specific questions. Use them when the workflow guidance has already pointed you to a concept and you need exact field, command, metric, or contract detail.

## Lookups

- [CLI](/docs/reference/cli/) for command, flag, and exit-code behavior.
- [Configuration](/docs/reference/configuration/) for the provider configuration file shape, defaults, and validation rules.
- [KMS v2 Contract](/docs/reference/kms-v2-contract/) for the gRPC protocol surface the Kubernetes API server consumes.
- [Key ID And AAD](/docs/reference/key-id-and-aad/) for the `key_id` format, annotation rules, and additional authenticated data (AAD) envelope.
- [EncryptionConfiguration](/docs/reference/encryption-config/) for the Kubernetes API server `EncryptionConfiguration` shape used with this provider.
- [Observability](/docs/reference/observability/) for the principles of metrics, logs, error classes, and health endpoints.
- [Metrics](/docs/reference/metrics/) for the metric-by-metric and log-field reference.
- [Compatibility](/docs/reference/compatibility/) for the supported Kubernetes and OpenBao version envelope.
- [Support Policy](/docs/reference/support-policy/) for the supported configurations and version-pinning expectations.
- [Release Policy](/docs/reference/release-policy/) for release channels, artifact policy, and verification materials.
- [Transit Policy Examples](/docs/configure/openbao-auth/) for least-privilege OpenBao policies for the provider hot path.

## Use Another Section If

- the question is about how to install or wire the provider: go to [Get Started](/docs/get-started/).
- the question is about an operational task or runbook: go to [Operate](/docs/operate/).
- the question is about why a given behavior is the way it is: go to [Architecture](/docs/architecture/).

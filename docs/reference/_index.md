---
title: "Reference"
description: "Exact behavior, configuration shape, KMS v2 contract, key_id and AAD format, observability surface, and policy boundaries."
weight: 40
browse:
  - "/reference/cli"
  - "/reference/configuration"
  - "/reference/kms-v2-contract"
  - "/reference/key-id-and-aad"
  - "/reference/encryption-config"
  - "/reference/observability"
  - "/reference/metrics"
  - "/reference/compatibility"
  - "/reference/support-policy"
  - "/reference/release-policy"
  - "/reference/transit-policy-examples"
---

# Reference

These pages answer behavior-specific questions. Use them when the workflow guidance has already pointed you to a concept and you need exact field, command, metric, or contract detail.

## Lookups

- [CLI](/reference/cli/) for command, flag, and exit-code behavior.
- [Configuration](/reference/configuration/) for the provider configuration file shape, defaults, and validation rules.
- [KMS v2 Contract](/reference/kms-v2-contract/) for the gRPC protocol surface the Kubernetes API server consumes.
- [Key ID And AAD](/reference/key-id-and-aad/) for the key_id format, annotation rules, and AAD envelope.
- [EncryptionConfiguration](/reference/encryption-config/) for the Kubernetes API server `EncryptionConfiguration` shape used with this provider.
- [Observability](/reference/observability/) for the principles of metrics, logs, error classes, and health endpoints.
- [Metrics](/reference/metrics/) for the metric-by-metric and log-field reference.
- [Compatibility](/reference/compatibility/) for the supported Kubernetes and OpenBao version envelope.
- [Support Policy](/reference/support-policy/) for the supported configurations and version-pinning expectations.
- [Release Policy](/reference/release-policy/) for the release cadence and deprecation window.
- [Transit Policy Examples](/reference/transit-policy-examples/) for least-privilege OpenBao policies for the provider hot path.

## Use Another Section If

- the question is about how to install or wire the provider: go to [Start Here](/getting-started/).
- the question is about an operational task or runbook: go to [Operations](/operations/).
- the question is about why a given behavior is the way it is: go to [Architecture](/architecture/).

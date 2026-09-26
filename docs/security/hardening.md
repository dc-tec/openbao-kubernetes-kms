---
title: Hardening
description: "The controls a hardened deployment must have in OpenBao, on the host, for auth material, in logs and metrics, and in Kubernetes."
eyebrow: Security · Host
weight: 20
verifiedBy:
  - deploy/systemd/bao-kms-provider.service
  - deploy/static-pod/bao-kms-provider.yaml
  - internal/config/validation.go
  - internal/logging
  - internal/metrics
---

Following [Get started](/docs/get-started/) with the maintained samples
produces most of these controls. Use this page to review a deployment, and
keep the controls in place when you customize it.

## OpenBao

Required:

- TLS with the CA bundle pinned in the provider configuration and server name
  verification.
- A dedicated Transit mount with `disable_upsert=true`, and a key with export,
  plaintext backup, and deletion disabled.
- A provider policy limited to key metadata read, encrypt, decrypt,
  `disable_upsert` inspection, and its own capabilities and renewal. No create,
  rotate, trim, delete, export, backup, restore, rewrap, or configuration
  permission; `doctor` checks these paths for the configured key.
- OpenBao HA outside the protected cluster's dependency path, with audit
  logging enabled and monitored.

Recommended: one Transit key and one auth role per cluster or trust domain,
tested OpenBao backup and restore, and change control around rotation and
`min_decryption_version`.

## Host

Required:

- File ownership and modes from [Linux identity model](/docs/security/linux-identity-model/):
  configuration and auth material readable only by root and the provider, the
  socket writable only by the provider and the API server identity.
- Metrics and health endpoints bound to localhost, and debug correlation off
  except during a bounded incident.
- Synchronized clocks.

Recommended: keep the sandboxing in the maintained systemd unit and static pod
manifest unchanged. The settings and their purpose are listed in
[Run with systemd](/docs/get-started/systemd/#about-the-unit) and
[Run as a static pod](/docs/get-started/static-pod/#about-the-manifest). Audit
changes to configuration and auth material, and upgrade one node at a time.

## Auth material

Required for JWT auth:

- An OpenBao role that binds issuer, audience, and subject or strong claims,
  with a short token TTL, a limited maximum TTL, no default policy, and only the
  provider policy.
- A JWT that expires, is checked before login, and is never logged.

Recommended for JWT auth: an issuer independent of the protected API server,
short JWT lifetimes with reliable renewal, `auth.jwt.expectedIssuer`,
`expectedAudience`, and `expectedSubject` set, issuer key rotation overlap, and
a documented emergency issuance process.

Required for certificate auth:

- An OpenBao listener that requests client certificates, and cert auth binding
  left enabled so renewal stays tied to the login identity.
- A role bound to the expected certificate identity.
- A PKCS#11 source; the provider rejects PEM private key files. Keys stay
  non-exportable, and the PIN file is a local file readable only by the
  provider.

Recommended for certificate auth: `ocsp_fail_open=false` when OCSP is on, and
alerts on `openbao_kms_certificate_ttl_seconds`.

For the reasoning behind these rules, see [Auth model](/docs/security/auth-model/).

## Logs and metrics

The provider never logs plaintext, JWTs, OpenBao tokens, full ciphertext, key
material, or, by default, raw OpenBao paths and key names. Metrics never carry
raw `key_id` values, OpenBao paths, key names, request UIDs, Kubernetes object
names, certificate subjects, or free-form error strings as labels. Keep custom
dashboards and log pipelines within the same rules. See
[Reference: Observability](/docs/reference/observability/).

## Kubernetes

Required:

- A KMS v2 provider on a local Unix socket, with a provider name that never
  changes while encrypted data exists.
- An API server that can reach only the socket, and can traverse but not
  modify the socket directory.
- An identical `EncryptionConfiguration`, provider name, and socket path on
  every control-plane node.

Recommended: keep the `identity` fallback only during migration, audit the
`EncryptionConfiguration` afterwards, and test an API server restart after
enabling encryption.

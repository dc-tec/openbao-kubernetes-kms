---
title: "Hardening"
description: "Required and recommended hardening for production deployments of bao-kms-provider: OpenBao, plugin host, file permissions, JWT, logging, metrics, and Kubernetes-side."
weight: 20
---

# Hardening

This page lists hardening requirements for production deployments. For the threat coverage see [Threat Model](/security/threat-model/). For the file ownership and group model the host-side requirements rely on, see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

## OpenBao

Required:

- TLS enabled,
- CA bundle pinned in plugin configuration,
- server name verified,
- Transit key export disabled,
- plaintext backup disabled,
- key deletion disabled,
- Transit upsert disabled at the dedicated mount where feasible,
- plugin policy limited to metadata read, encrypt update, decrypt update, and `disable_upsert` inspection,
- audit logging enabled and monitored.

Recommended:

- OpenBao HA deployment outside the protected Kubernetes dependency path,
- tested OpenBao backup and restore procedure,
- a separate Transit key per Kubernetes cluster or trust domain,
- a separate JWT auth role per cluster or trust domain,
- change control around key rotation and `min_decryption_version`.

## Plugin Host

Required:

- configuration file readable only by root and the required service identity,
- JWT readable only by the plugin process,
- socket writable only by the plugin and the API server identity,
- metrics and health endpoints bound to localhost by default,
- no debug endpoints in production,
- time synchronized through NTP or chrony.

Recommended:

- systemd sandboxing where systemd mode is used,
- distroless non-root image and read-only container filesystem where static-pod mode is used,
- immutable image digests,
- host audit for configuration and JWT changes,
- one-node-at-a-time upgrades.

## File Permissions

Recommended:

```text
/etc/openbao-kms/config.yaml        root:openbao-kms                0640
/etc/openbao-kms/tls/ca.crt         root:root                       0644
/var/lib/openbao-kms/identity.jwt   root:openbao-kms                0640
/var/lib/openbao-kms/state          openbao-kms:openbao-kms         0750
/run/openbao-kms                    openbao-kms:openbao-kms-socket  2750
/run/openbao-kms/kms.sock           openbao-kms:openbao-kms-socket  0660
```

For the rationale and runtime directory creation pattern see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

## JWT

Required:

- bound issuer,
- bound audience,
- bound subject,
- expiry checked before login,
- JWT file re-read before re-login,
- no JWT logging.

Recommended:

- external issuer independent of the protected API server,
- short JWT lifetime with reliable renewal,
- `auth.expectedIssuer`, `auth.expectedAudience`, and `auth.expectedSubject` set as early misconfiguration diagnostics when the expected service-account token identity is stable,
- issuer key rotation overlap,
- documented emergency issuance process,
- pinned public keys for recovery where appropriate.

The portable OpenBao/provider e2e lanes exercise bound-claim rejection and
pinned public-key rollover. JWKS/OIDC discovery behavior remains
issuer-environment specific and should be validated during issuer integration.

For the trust-boundary discussion see [Auth Model](/security/auth-model/).

## Logging

The provider must never log:

- plaintext,
- JWTs,
- OpenBao tokens,
- full ciphertext,
- raw Transit key material,
- raw OpenBao paths by default,
- raw key names by default.

Use bounded error classes and hashed `key_id` values. For the full log shape and field reference see [Reference: Observability](/reference/observability/) and [Reference: Metrics](/reference/metrics/).

## Metrics

Do not label metrics with:

- raw `key_id` values,
- raw OpenBao paths,
- raw key names,
- request UID values,
- Kubernetes namespace or object name values,
- unbounded error message strings.

## Kubernetes

Required:

- the KMS provider uses `apiVersion: v2`,
- the provider name is stable across the lifetime of encrypted data,
- the endpoint is a local Unix socket,
- the API server can access only the required socket path,
- the API server socket access group can traverse the socket directory but cannot create, delete, or replace entries in it.

Recommended:

- `identity` fallback only during migration,
- `EncryptionConfiguration` audited after migration,
- API server restart tested after enabling encryption,
- etcd plaintext inspection performed in a controlled environment.

## Static Pod Specific

- Do not reference ConfigMaps, Secrets, or ServiceAccounts.
- Use hostPath mounts for all required files.
- Preload images in air-gapped environments.
- Use read-only mounts for configuration, CA, and JWT.
- Keep the previous image available for rollback.

## systemd Specific

Recommended hardening directives:

- `NoNewPrivileges=true`
- `ProtectSystem=strict`
- `ProtectHome=true`
- `PrivateTmp=true`
- `MemoryDenyWriteExecute=true`
- `LockPersonality=true`
- `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`
- minimal `ReadWritePaths`

Verify hardening does not prevent access to the configuration file, JWT, CA bundle, socket directory, or the optional state file.

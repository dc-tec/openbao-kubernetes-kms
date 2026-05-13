---
title: "Hardening"
description: "Required and recommended hardening for production-oriented deployments of bao-kms-provider: OpenBao, plugin host, file permissions, auth material, logging, metrics, and Kubernetes-side."
weight: 20
---

# Hardening

This page lists hardening requirements for production-oriented deployments. For the threat coverage see [Threat Model](/security/threat-model/). For the file ownership and group model the host-side requirements rely on, see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

## OpenBao

Required:

- TLS enabled,
- CA bundle pinned in plugin configuration,
- server name verified,
- Transit key export disabled,
- plaintext backup disabled,
- key deletion disabled,
- OpenBao HA deployment outside the protected Kubernetes dependency path,
- Transit upsert disabled at the dedicated mount,
- plugin policy limited to metadata read, encrypt update, decrypt update, and `disable_upsert` inspection,
- no Transit create, delete, rotate, export, backup, or configuration write permissions for the plugin token,
- audit logging enabled and monitored.

Recommended:

- tested OpenBao backup and restore procedure,
- a separate Transit key per Kubernetes cluster or trust domain,
- a separate auth mount or role per Kubernetes cluster or trust domain,
- change control around key rotation and `min_decryption_version`.

## Plugin Host

Required:

- configuration file readable only by root and the required service identity,
- file-backed auth material readable only by the plugin process,
- socket writable only by the plugin and the API server identity,
- metrics and health endpoints bound to localhost by default,
- no debug endpoints in production,
- debug correlation disabled except during bounded incident response,
- time synchronized through NTP or chrony.

Recommended:

- systemd sandboxing where systemd mode is used,
- distroless non-root image and read-only container filesystem where static-pod mode is used,
- immutable image digests,
- pinned release artifacts with verified checksums,
- host audit for configuration and auth material changes,
- one-node-at-a-time upgrades.

## File Permissions

Recommended:

```text
/etc/openbao-kms/config.yaml        root:openbao-kms                0640
/etc/openbao-kms/tls/ca.crt         root:root                       0644
/var/lib/openbao-kms/identity.jwt   root:openbao-kms                0640
/etc/openbao-kms/client/client-chain.pem root:openbao-kms           0640
/etc/openbao-kms/pkcs11/pin         root:openbao-kms                0640
/var/lib/openbao-kms/state          openbao-kms:openbao-kms         0750
/run/openbao-kms                    openbao-kms:openbao-kms-socket  2750
/run/openbao-kms/kms.sock           openbao-kms:openbao-kms-socket  0660
```

For the rationale and runtime directory creation pattern see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

## Auth Material

Required for JWT auth:

- bound issuer,
- bound audience,
- bound subject or strong bound claims,
- no default policy on the OpenBao token,
- one dedicated Transit policy,
- expiry checked before login,
- JWT file re-read before re-login,
- OpenBao client token stored in memory only,
- no JWT logging.

Recommended for JWT auth:

- external issuer independent of the protected API server,
- short JWT lifetime with reliable renewal,
- `auth.jwt.expectedIssuer`, `auth.jwt.expectedAudience`, and `auth.jwt.expectedSubject` set as early misconfiguration diagnostics when the expected service-account token identity is stable,
- issuer key rotation overlap,
- documented emergency issuance process,
- pinned public keys for recovery where appropriate.

Required for certificate auth:

- OpenBao listener requests TLS client certificates,
- cert auth role binds the expected certificate identity,
- cert auth method binding remains enabled for renewal,
- OpenBao client token stored in memory only,
- no certificate private key or PIN logging,
- no PEM private key file source.

Recommended for certificate auth:

- SPIFFE roles bind `allowed_uri_sans` to the exact configured SPIFFE ID,
- PKCS#11 private keys stay non-exportable,
- PKCS#11 PIN files are local regular files with provider-only read access,
- OCSP fail-open remains disabled when OCSP is enabled,
- certificate TTL monitoring uses `openbao_kms_certificate_ttl_seconds`.

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
- SPIFFE IDs or certificate subject values.

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
- provider name, socket path, and `EncryptionConfiguration` consistent across all control-plane nodes,
- API server restart tested after enabling encryption,
- etcd plaintext inspection performed in a controlled environment.

## Static Pod Specific

- Do not reference ConfigMaps, Secrets, or ServiceAccounts.
- Set `automountServiceAccountToken: false`.
- Use hostPath mounts for all required files.
- Preload images in air-gapped environments.
- Use read-only mounts for configuration, CA, JWT, certificate chain, and PKCS#11 PIN files.
- Run as a non-root numeric user and numeric supplemental group that matches the host socket group.
- Set `seccompProfile: RuntimeDefault`.
- Set `allowPrivilegeEscalation: false`.
- Drop all Linux capabilities.
- Set `readOnlyRootFilesystem: true`.
- Keep the previous image available for rollback.

## systemd Specific

Recommended hardening directives:

- `NoNewPrivileges=true`
- `ProtectSystem=strict`
- `ProtectHome=true`
- `PrivateTmp=true`
- `PrivateDevices=true`
- `MemoryDenyWriteExecute=true`
- `LockPersonality=true`
- `RestrictSUIDSGID=true`
- `RestrictRealtime=true`
- `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`
- `SystemCallArchitectures=native`
- `CapabilityBoundingSet=`
- `AmbientCapabilities=`
- `ReadOnlyPaths=/etc/openbao-kms`
- minimal `ReadWritePaths`

Verify hardening does not prevent access to the configuration file, selected auth material, CA bundle, socket directory, or the optional state file.

## Validate Hardening

Run these checks before enabling the provider in an API server:

```sh
bao-kms-provider verify-key --config /etc/openbao-kms/config.yaml
bao-kms-provider doctor --config /etc/openbao-kms/config.yaml --encryption-config /etc/kubernetes/encryption-config.yaml
curl -sf http://127.0.0.1:8082/ready
```

After the API server is configured, confirm that newly written Secret data is not stored as plaintext in etcd and that the provider metrics on `127.0.0.1:8081` show successful KMS `encrypt` and `decrypt` requests.

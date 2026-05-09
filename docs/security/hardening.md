# Hardening Guide

This guide lists hardening requirements for production-like deployments.

## OpenBao

Required:

- TLS enabled.
- CA bundle pinned in plugin config.
- Server name verified.
- Transit key export disabled.
- Plaintext backup disabled.
- Key deletion disabled.
- Transit upsert disabled for the dedicated mount where feasible.
- Plugin policy limited to metadata read, encrypt, decrypt, and optional config read.
- Audit logging enabled and monitored.

Recommended:

- OpenBao HA deployment outside the protected Kubernetes dependency path.
- Tested OpenBao backup and restore.
- Separate Transit key per Kubernetes cluster or trust domain.
- Separate JWT auth role per cluster/trust domain.
- Change control around key rotation and `min_decryption_version`.

## Plugin Host

Required:

- Config readable only by root and required service identity.
- JWT readable only by plugin.
- Socket writable only by plugin and API server.
- Metrics and health endpoints bound to localhost by default.
- No debug endpoints in production.
- Time synchronized through NTP or chrony.

Recommended:

- systemd sandboxing where systemd mode is used,
- distroless non-root image and read-only container filesystem where static pod mode is used,
- immutable image digests,
- host audit for config and JWT changes,
- one-node-at-a-time upgrades.

## File Permissions

Recommended:

```text
/etc/openbao-kms/config.yaml        root:openbao-kms 0640
/etc/openbao-kms/tls/ca.crt         root:root 0644
/var/lib/openbao-kms/identity.jwt   root:openbao-kms 0640
/var/lib/openbao-kms/state          openbao-kms:openbao-kms 0750
/run/openbao-kms                    openbao-kms:openbao-kms-socket 2750
/run/openbao-kms/kms.sock           openbao-kms:openbao-kms-socket 0660
```

## JWT

Required:

- Bound audience.
- Bound subject.
- Expiry checked before login.
- Re-read file before re-login.
- No JWT logging.

Recommended:

- external issuer independent of protected API server,
- short JWT lifetime with reliable renewal,
- `auth.expectedIssuer`, `auth.expectedAudience`, and `auth.expectedSubject` set as early misconfiguration diagnostics when the expected service-account token identity is stable,
- issuer key rotation overlap,
- emergency issuance process documented,
- pinned public keys for recovery where appropriate.

## Logging

Never log:

- plaintext,
- JWT,
- OpenBao token,
- full ciphertext,
- raw Transit key material,
- raw OpenBao mount paths by default,
- raw key names by default.

Use bounded error classes and hashed key IDs.

## Metrics

Do not label metrics with:

- raw key IDs,
- raw OpenBao paths,
- raw key names,
- UID values,
- namespace/name values,
- unbounded error messages.

## Kubernetes

Required:

- KMS provider uses `apiVersion: v2`.
- Provider name is stable.
- Endpoint is a local Unix socket.
- API server can access only the required socket path.
- API server socket group can traverse the socket directory but cannot create, delete, or replace entries in it.

Recommended:

- `identity` fallback only during migration,
- encryption config audited after migration,
- API server restart tested after encryption,
- etcd plaintext inspection performed in a controlled environment.

## Static Pod Specific

- Do not reference ConfigMaps, Secrets, or ServiceAccounts.
- Use hostPath mounts for all required files.
- Preload images in air-gapped environments.
- Use read-only mounts for config, CA, and JWT.
- Keep previous image available for rollback.

## systemd Specific

Recommended hardening:

- `NoNewPrivileges=true`
- `ProtectSystem=strict`
- `ProtectHome=true`
- `PrivateTmp=true`
- `MemoryDenyWriteExecute=true`
- `LockPersonality=true`
- `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`
- minimal `ReadWritePaths`

Verify hardening does not prevent access to config, JWT, CA bundle, socket directory, or optional state file.

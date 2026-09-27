---
title: Configuration
description: "Every provider configuration field with its default and meaning, the identity-bearing fields, validation rules, and environment overrides."
eyebrow: Reference
weight: 20
verifiedBy:
  - internal/config/config.go
  - internal/config/validation.go
  - internal/config/schema.go
  - cmd/bao-kms-provider/certauth_enabled.go
  - internal/auth/pkcs11_provider.go
  - deploy/config/provider-systemd.yaml
---

The provider reads one YAML file, `/etc/openbao-kms/config.yaml` by default.
Start from `deploy/config/provider-systemd.yaml` or
`deploy/config/provider-static-pod.yaml`, and see
[Plan identity values](/docs/get-started/plan-values/#provider-configuration)
for the fields every deployment changes. `bao-kms-provider config schema`
prints the JSON Schema, which rejects unknown fields.

String fields need YAML strings: quote values YAML would read as numbers or
booleans, such as `keyName: "0123"` or `socketGroup: "1234"`. Durations need a
unit, such as `120s`; a bare `120` is rejected.

## Fields

Fields marked **required** have no usable default.

### server

| Field | Default | Meaning |
|---|---|---|
| `configVersion` | `v1alpha1` | Required. The only accepted version. |
| `server.socketPath` | `/run/openbao-kms/kms.sock` | Required. Unix socket for the API server; must sit in a safe runtime directory. |
| `server.socketMode` | `"0660"` | Required. Socket mode; broader modes are rejected. |
| `server.socketGroup` | none | Required. Socket group, as a name (systemd) or decimal GID (static pod). |
| `server.metricsAddress` | `127.0.0.1:8081` | Prometheus listener. A fixed endpoint must differ from `server.healthAddress`. An empty value disables the listener; port `0` requests an available port. |
| `server.healthAddress` | `127.0.0.1:8082` | `/live` and `/ready` listener. An empty value disables the listener; port `0` requests an available port. |
| `server.maxConcurrentStatus`, `maxConcurrentEncrypt`, `maxConcurrentDecrypt` | `16`, `32`, `64` | Active handlers per KMS method, `1` to `1024`. Excess calls fail immediately with `ResourceExhausted`; there is no queue. |

### openbao

| Field | Default | Meaning |
|---|---|---|
| `openbao.address` | none | Required. HTTPS endpoint that serves requests directly. Every HTTP redirect is rejected, so for HA use server-side request forwarding or route to the active node. |
| `openbao.caCertFile` | none | Required. CA bundle for OpenBao TLS. |
| `openbao.tlsServerName` | none | Required. Expected server name in the OpenBao certificate. |
| `openbao.instanceId` | none | Required, identity-bearing. |
| `openbao.namespace` | empty | Identity-bearing. A relative namespace such as `admin/workload-a`, sent as `X-Vault-Namespace`. Mount paths stay relative to it. |
| `openbao.timeout` | `2s` | Deadline for one OpenBao request. Dial, TLS, and header timeouts are fixed. |

### auth

| Field | Default | Meaning |
|---|---|---|
| `auth.method` | `jwt` | `jwt`, or `cert` in certificate-auth builds; see [Compatibility](/docs/reference/compatibility/#auth-methods). |
| `auth.loginBeforeTokenExpiry` | `5m` | Inside this window before expiry, a request starts one shared renewal or login while others keep using the valid token. |
| `auth.tokenRenewalIncrement` | `1h` | TTL requested on renewal. Must exceed `auth.loginBeforeTokenExpiry`. Keep it within the role's maximum TTL; OpenBao can grant a shorter TTL. |
| `auth.loginTimeout` | `0s` | Deadline for one shared renewal or login, including recovery after a rejected token, independent of the request deadline. Also bounds each PKCS#11 session pool wait. `0s` means `max(openbao.timeout, 5s)`; it does not enable an unlimited pool wait. See the native-call limit below. |
| `auth.jwt.mountPath`, `auth.jwt.role` | none | Required for `jwt`. Mount path including `auth/`, and role name. |
| `auth.jwt.source` | legacy file inference | `file` or `oauth2`. An omitted source selects `file` only when `jwtFile` is set and no `oauth2` section is present. Explicit empty values are invalid. |
| `auth.jwt.jwtFile` | none | Required for source `file`; forbidden for `oauth2`. Absolute path to a regular file, not a symlink. Re-read before every login. |
| `auth.jwt.minRemainingTtl` | `2m` | Minimum JWT lifetime left for a login. |
| `auth.jwt.clockSkewLeeway` | `30s` | Leeway for `nbf`, `iat`, and `exp`. |
| `auth.jwt.expectedIssuer`, `expectedAudience`, `expectedSubject` | empty | Local claim checks before login. Issuer and at least one audience are required for source `oauth2`. |
| `auth.jwt.oauth2.tokenUrl` | none | Required for `oauth2`. HTTPS token endpoint without user info or fragment. Redirects are rejected. |
| `auth.jwt.oauth2.clientId`, `clientSecretFile` | none | Required for `oauth2`. Client ID and absolute path to a private, regular client-secret file. The secret is reread before every grant. |
| `auth.jwt.oauth2.authMethod` | none | Required for `oauth2`: `client_secret_basic` or `client_secret_post`. |
| `auth.jwt.oauth2.scopes` | empty | List of OAuth scopes, joined with spaces in the request. |
| `auth.jwt.oauth2.audience` | empty | Optional issuer-specific `audience` request parameter. |
| `auth.jwt.oauth2.resources` | empty | List of absolute resource URIs sent as repeated RFC 8707 `resource` parameters. |
| `auth.jwt.oauth2.caCertFile` | empty | Token endpoint CA bundle. Empty uses system roots. |
| `auth.cert.mountPath`, `auth.cert.source` | none | Required for `cert`. `source` must be `pkcs11`. |
| `auth.cert.name` | empty | OpenBao cert role name; empty lets OpenBao try every role. |
| `auth.cert.minRemainingTtl` | `24h` | Minimum certificate lifetime left for a login. |
| `auth.cert.clockSkewLeeway` | `30s` | Leeway for certificate validity. |
| `auth.cert.pkcs11.certificateFile`, `modulePath`, `tokenLabel`, `keyLabel`, `pinFile` | none | Required for `pkcs11`. The certificate file holds only PEM `CERTIFICATE` blocks; the PIN file is an absolute, regular file with one line, not a symlink. See [credential file permissions](/docs/configure/openbao-auth/#credential-files). |
| `auth.cert.pkcs11.maxSessions` | none | At least `2` for `pkcs11`. |

After a `401` or `403`, the provider logs in again and retries the request
once, at most once every five seconds and with exponential backoff for failed
logins; see [Security: Auth model](/docs/security/auth-model/#plugin-authentication-lifecycle).

For PKCS#11, the effective `auth.loginTimeout` limits each wait for a free
session. A pool timeout fails that signing attempt; the provider retries login
with its normal backoff. `maxSessions` includes one session reserved for token
login state, so `maxSessions: 2` permits one concurrent signing operation.

The pool timeout and Go context deadlines cannot interrupt a native call
already running inside the PKCS#11 module. They do not bound module
initialization, session creation, signing, or shutdown inside that module.
Configure finite connection and operation timeouts in the HSM vendor client.
If a native call never returns, the provider may need a restart after HSM
connectivity recovers. See [Troubleshooting](/docs/operate/troubleshooting/#auth-login-fails).

A full certificate-auth configuration:

```yaml
auth:
  method: cert
  cert:
    mountPath: auth/k8s-workload-a-cert
    name: openbao-kms-control-plane
    source: pkcs11
    pkcs11:
      certificateFile: /etc/openbao-kms/client/client-chain.pem
      modulePath: /usr/lib/softhsm/libsofthsm2.so
      tokenLabel: openbao-kms
      keyLabel: openbao-kms-client
      pinFile: /etc/openbao-kms/pkcs11/pin
      maxSessions: 4
```

### transit, state, and runtime behavior

| Field | Default | Meaning |
|---|---|---|
| `transit.mountPath`, `transit.keyName` | none | Required, identity-bearing. A key name starts and ends with an ASCII letter, digit, or underscore. Interior characters can also include dots and hyphens. |
| `transit.keyIdScope.providerName`, `clusterId`, `transitMountId`, `keyLineageId` | none | Required, identity-bearing. |
| `state.path` | `/var/lib/openbao-kms/state/key-registry.json` | Absolute path of the local registry state; the checkpoint and lock sit next to it. |
| `bootstrap.graceTimeout`, `bootstrap.retryInterval` | `60s`, `5s` | How long and how often startup retries its first probes before exiting. |
| `status.probeInterval` | `30s` | Metadata probe interval, also the per-process limit for unknown-`key_id` discovery. Must be shorter than `status.statusMaxStaleness`; allow margin for request latency and scheduling. |
| `status.deepProbeInterval` | `5m` | Encrypt and decrypt probe interval. |
| `status.statusMaxStaleness` | `2m` | Oldest cached Status that still counts as healthy. |
| `rotation.mode` | `observed` | Promotion follows observed Transit versions. |
| `rotation.activationDelay` | `2m` | Full elapsed-time wait after durable stable observation. Restarts wait the full delay again after metadata validation. |
| `rotation.requireStableObservationCount` | `3` | Successful observations needed before promotion. |
| `rotation.rejectVersionRollback` | `true` | Reject a Transit `latest_version` that moves backwards. |
| `logging.level`, `logging.format` | `info`, `json` | Log level and format. |
| `logging.logOpenBaoRequestIDs` | `true` | Log safe OpenBao request IDs. |
| `logging.debugCorrelation.enabled`, `ttl`, `incidentId` | `false`, `15m`, empty | Incident-only correlation; see [Observability](/docs/reference/observability/#debug-correlation). |

## Identity-bearing fields

`transit.keyIdScope.providerName`, `clusterId`, `transitMountId`, and
`keyLineageId`, `openbao.instanceId`, `openbao.namespace`, `transit.mountPath`,
`transit.keyName`, and the `EncryptionConfiguration` provider name feed every
`key_id` and AAD. Changing one after encryption begins can make existing data
unreadable, so treat them as immutable; a change needs a migration plan.
`bao-kms-provider config` prints an identity fingerprint over them to compare
across nodes without exposing the values.

## Validation

Startup fails closed when:

- the configuration file permissions are unsafe,
- the socket path, its parent directory, or its mode is unsafe, or the path is
  a symlink or regular file,
- the state path is not absolute,
- the CA file is missing, the OpenBao address is invalid or carries user info,
  a query, or a fragment, or the TLS server name is empty,
- a required or identity-bearing field is empty, contains surrounding
  whitespace or control characters, or the namespace is malformed,
- the JWT is unreadable, expired, too close to expiry, or has `nbf` or `iat`
  outside the leeway,
- `cert` auth is selected in a build without it, the certificate source is
  unavailable, the certificate is invalid, too close to expiry, lacks
  client-auth usage, is weakly signed, or does not match its signer, or a
  PKCS#11 setting is unsafe,
- the SPIFFE certificate source is selected.

There is no setting to disable AAD, enable debug endpoints, enable decrypt
micro-batching, or log raw OpenBao paths.

## Environment overrides

Only these settings can come from the environment. Secrets and
identity-bearing fields cannot.

| Variable | Setting |
|---|---|
| `BAO_KMS_PROVIDER_CONFIG`, `BAO_KMS_PROVIDER_CONFIG_PATH` | Configuration file path |
| `BAO_KMS_PROVIDER_LOG_LEVEL`, `BAO_KMS_PROVIDER_LOGGING_LEVEL` | `logging.level` |
| `BAO_KMS_PROVIDER_SERVER_METRICS_ADDRESS`, `BAO_KMS_PROVIDER_SERVER_METRICSADDRESS` | `server.metricsAddress` |
| `BAO_KMS_PROVIDER_SERVER_HEALTH_ADDRESS`, `BAO_KMS_PROVIDER_SERVER_HEALTHADDRESS` | `server.healthAddress` |

---
title: "Configuration"
description: "Authoritative reference for the bao-kms-provider configuration file: example, defaults, validation rules, identity-bearing fields, and unsafe options."
weight: 20
---

# Configuration

This page is the authoritative reference for the `bao-kms-provider` configuration file. Identity-bearing fields and default values are stable across preview patch releases.

## Example

```yaml
configVersion: v1alpha1
server:
  socketPath: /run/openbao-kms/kms.sock
  socketMode: "0660"
  socketGroup: openbao-kms-socket
  metricsAddress: "127.0.0.1:8081"
  healthAddress: "127.0.0.1:8082"

openbao:
  address: https://bao.example.internal:8200
  namespace: ""
  caCertFile: /etc/openbao-kms/tls/ca.crt
  tlsServerName: bao.example.internal
  timeout: 2s
  instanceId: bao-prod-a

auth:
  method: jwt
  loginBeforeTokenExpiry: 5m
  tokenRenewalIncrement: 1h
  loginTimeout: 0s
  jwt:
    mountPath: auth/k8s-workload-a-jwt
    role: openbao-kms-control-plane
    jwtFile: /var/lib/openbao-kms/identity.jwt
    minRemainingTtl: 2m
    clockSkewLeeway: 30s
    expectedIssuer: ""
    expectedAudience: []
    expectedSubject: ""

transit:
  mountPath: transit
  keyName: k8s-workload-a-etcd
  keyIdScope:
    providerName: openbao-kms-workload-a
    clusterId: workload-a
    transitMountId: transit-prod-primary
    keyLineageId: "01HXEXAMPLEKEYLINEAGEID"

bootstrap:
  graceTimeout: 60s
  retryInterval: 5s

status:
  probeInterval: 30s
  deepProbeInterval: 5m
  statusMaxStaleness: 2m

state:
  path: /var/lib/openbao-kms/state/key-registry.json

rotation:
  mode: observed
  activationDelay: 2m
  requireStableObservationCount: 3
  rejectVersionRollback: true

logging:
  level: info
  format: json
  logOpenBaoRequestIDs: true
  debugCorrelation:
    enabled: false
    ttl: 15m
    incidentId: ""
```

## Required Fields

The following fields must be set explicitly:

- `configVersion`
- `server.socketPath`
- `server.socketMode`
- `server.socketGroup`
- `openbao.address`
- `openbao.caCertFile`
- `openbao.tlsServerName`
- `openbao.instanceId`
- `auth.method`
- when `auth.method` is `jwt`:
  - `auth.jwt.mountPath`
  - `auth.jwt.role`
  - `auth.jwt.jwtFile`
- when `auth.method` is `cert`:
  - `auth.cert.mountPath`
  - `auth.cert.source`
  - for `auth.cert.source: pkcs11`:
    - `auth.cert.pkcs11.certificateFile`
    - `auth.cert.pkcs11.modulePath`
    - `auth.cert.pkcs11.tokenLabel`
    - `auth.cert.pkcs11.keyLabel`
    - `auth.cert.pkcs11.pinFile`
- `transit.mountPath`
- `transit.keyName`
- `transit.keyIdScope.providerName`
- `transit.keyIdScope.clusterId`
- `transit.keyIdScope.transitMountId`
- `transit.keyIdScope.keyLineageId`

`transit.keyName` is treated as one OpenBao path segment. Configuration
validation rejects `/` and `%` in the key name so the provider, diagnostics,
and generated policy paths all address the same Transit key.

`openbao.namespace` is optional. Leave it empty for the root namespace. When
set, it is sent as `X-Vault-Namespace` on OpenBao auth and Transit requests and
is treated as identity-bearing provider scope.

## Defaults

| Field | Default |
|---|---|
| `configVersion` | `v1alpha1` |
| `server.socketPath` | `/run/openbao-kms/kms.sock` |
| `server.socketMode` | `"0660"` |
| `server.metricsAddress` | `127.0.0.1:8081` |
| `server.healthAddress` | `127.0.0.1:8082` |
| `openbao.timeout` | `2s` |
| `auth.method` | `jwt` |
| `auth.loginBeforeTokenExpiry` | `5m` |
| `auth.tokenRenewalIncrement` | `1h` |
| `auth.loginTimeout` | `max(openbao.timeout, 5s)` when unset or `0s` |
| `auth.jwt.minRemainingTtl` | `2m` |
| `auth.jwt.clockSkewLeeway` | `30s` |
| `auth.jwt.expectedIssuer` | empty |
| `auth.jwt.expectedAudience` | empty |
| `auth.jwt.expectedSubject` | empty |
| `auth.cert.minRemainingTtl` | `24h` |
| `auth.cert.clockSkewLeeway` | `30s` |
| `auth.cert.name` | empty, which lets OpenBao try every configured certificate role |
| `auth.cert.pkcs11.maxSessions` | unset, but validation requires at least `2` when the PKCS#11 source is used |
| `bootstrap.graceTimeout` | `60s` |
| `bootstrap.retryInterval` | `5s` |
| `status.probeInterval` | `30s` |
| `status.deepProbeInterval` | `5m` |
| `status.statusMaxStaleness` | `2m` |
| `state.path` | `/var/lib/openbao-kms/state/key-registry.json` |
| `rotation.mode` | `observed` |
| `rotation.activationDelay` | `2m` |
| `rotation.requireStableObservationCount` | `3` |
| `rotation.rejectVersionRollback` | `true` |
| `logging.level` | `info` |
| `logging.format` | `json` |
| `logging.logOpenBaoRequestIDs` | `true` |
| `logging.debugCorrelation.enabled` | `false` |
| `logging.debugCorrelation.ttl` | `15m` |
| `logging.debugCorrelation.incidentId` | empty |

## Auth Timing

`auth.method` selects how the provider obtains its OpenBao token. The default
preview release artifacts are JWT-only and use `jwt`. The `cert` method is
available only in binaries built with a certificate-auth build tag, and PKCS#11
certificate auth is a supported preview path only when the selected release
includes matching opt-in artifacts and E2E evidence.

`auth.loginBeforeTokenExpiry` is the refresh-ahead threshold. Once the remaining OpenBao token TTL drops below this value, the provider renews or re-logs in before the next request.

`auth.tokenRenewalIncrement` is the requested TTL increment sent to OpenBao during `auth/token/renew-self`. Keep it larger than the refresh-ahead threshold and within the auth role's maximum token TTL.

`auth.loginTimeout` can be left at `0s` to derive `max(openbao.timeout, 5s)`.

`auth.jwt.minRemainingTtl` controls how much JWT lifetime must remain before the provider will use a JWT for login. The JWT file is re-read before each re-login.

`auth.cert.minRemainingTtl` controls how much client certificate lifetime must remain before the provider will attempt OpenBao cert auth. The provider validates the certificate locally before login and records the observed certificate TTL for metrics.

`bootstrap.graceTimeout` controls how long startup retries the initial status probe before the process exits. It exists to handle boot races such as auth material projection, DNS or routing settling, OpenBao restart, and clock synchronization.

## Certificate Auth

Certificate auth logs in to OpenBao through the TLS Certificate auth method by sending `POST /v1/<auth.cert.mountPath>/login` over a TLS connection that presents the configured client certificate. `auth.cert.name` maps to OpenBao's optional cert role `name` request field. Leave it empty only when the mount is intentionally configured so one matching role is unambiguous.

PKCS#11-backed certificate auth:

```yaml
auth:
  method: cert
  loginBeforeTokenExpiry: 5m
  tokenRenewalIncrement: 1h
  loginTimeout: 0s
  cert:
    mountPath: auth/k8s-workload-a-cert
    name: openbao-kms-control-plane
    minRemainingTtl: 24h
    clockSkewLeeway: 30s
    source: pkcs11
    pkcs11:
      certificateFile: /etc/openbao-kms/client/client-chain.pem
      modulePath: /usr/lib/softhsm/libsofthsm2.so
      tokenLabel: openbao-kms
      keyLabel: openbao-kms-client
      pinFile: /etc/openbao-kms/pkcs11/pin
      maxSessions: 4
```

This is the only certificate source inside the current preview support envelope
when the selected release includes the matching PKCS#11 artifact evidence and
E2E result.

OpenBao must be configured to request TLS client certificates on the listener used by the provider. In OpenBao listener terms, do not set `tls_disable` or `tls_disable_client_certs` to true for that listener. Role constraints should bind certificate identity, for example through `allowed_uri_sans` for URI identities. Keep cert auth binding enabled during token renewal and keep OCSP fail-open disabled when OCSP is used.

For the PKCS#11 source, `auth.cert.pkcs11.certificateFile` must be a PEM chain
containing only `CERTIFICATE` blocks. Do not place a PEM private key in that
file; the private key must remain behind the PKCS#11 module. The PIN file must
be an absolute, regular, tightly permissioned file containing one PIN line, with
only an optional trailing newline.

Current CI exercises PKCS#11 certificate auth end-to-end with SoftHSM and
OpenBao. The SPIFFE lane exercises the real SPIRE Workload API provider source
and local certificate validation, but `auth.cert.source: spiffe` is not a
supported user configuration until the supported OpenBao version can derive
cert-auth identity aliases from URI SANs.

## Debug Correlation

`logging.debugCorrelation` is an incident-response mode for short-window troubleshooting. Do not enable it for steady-state operation.

It only validates when:

- `logging.level` is `debug`,
- `logging.logOpenBaoRequestIDs` is `true`,
- `logging.debugCorrelation.incidentId` is set,
- `logging.debugCorrelation.ttl` is positive and no greater than one hour.

While active, logs may include safe request UID hashes and safe OpenBao request IDs for correlation with kube-apiserver and OpenBao audit records. The mode expires automatically after the configured TTL and still must not log plaintext, JWTs, OpenBao tokens, full ciphertext, raw OpenBao paths, or raw key names.

## Identity-Bearing Fields

Changing these fields after encryption begins can make existing data unreadable or force Kubernetes to treat data as encrypted with a different provider:

- `transit.keyIdScope.providerName`
- `transit.keyIdScope.clusterId`
- `openbao.instanceId`
- `openbao.namespace`
- `transit.keyIdScope.transitMountId`
- `transit.keyIdScope.keyLineageId`
- `transit.keyName`
- `transit.mountPath`
- Kubernetes `EncryptionConfiguration` provider name

Treat these values as immutable. Any change requires a documented migration plan; see [Operations: Disaster Recovery](/operations/disaster-recovery/) for the procedure.

Use `openbao.namespace` when one OpenBao cluster serves multiple Kubernetes
clusters through separate namespaces. The namespace must be a relative OpenBao
namespace path such as `admin/workload-a`; validation rejects leading slashes,
empty segments, dot segments, surrounding whitespace, control characters, and
percent encoding. Auth and Transit mount paths remain relative to that
namespace and must not include the namespace prefix.

The implementation exposes an identity fingerprint for these fields. Record it during rollout and compare it during troubleshooting without exposing raw cluster, OpenBao, or Transit topology values.

## Validation

Startup fails closed when any of the following conditions hold:

- config file permissions are unsafe,
- socket path is outside an approved runtime directory,
- socket parent directory is unsafe,
- socket path is a symlink or regular file,
- state path is not absolute,
- JWT auth is selected and the JWT file is unreadable,
- JWT auth is selected and the JWT is expired or too close to expiry,
- JWT auth is selected and the JWT `nbf` or `iat` claims are outside the configured clock-skew leeway,
- cert auth is selected without a cert-auth build variant,
- cert auth is selected and the configured certificate source is unavailable,
- cert auth is selected and the client certificate is expired, not yet valid, too close to expiry, missing client-auth usage, weakly signed, or mismatched with its signer,
- PKCS#11 cert auth is selected and the certificate file, module path, token label, key label, PIN file, or session count is unsafe,
- unsupported SPIFFE cert auth is selected,
- CA file is missing,
- OpenBao address is invalid or includes user info, query, or fragment data,
- TLS server name is empty,
- auth role or identity fields contain surrounding whitespace or control characters,
- provider name is empty,
- cluster ID is empty,
- OpenBao instance ID is empty,
- OpenBao namespace is malformed,
- Transit mount ID is empty,
- key lineage ID is empty,
- Transit mount or key names are empty,
- required AAD scope inputs are missing,
- socket mode is broader than configured policy allows,

## Permissions

Recommended local permissions:

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

JWT files, certificate chain files, and PKCS#11 PIN files should be readable only by the provider process. The socket should be readable and writable only by the provider and the local API server identity. The socket directory should be writable only by the provider identity.

`server.socketGroup` accepts a local group name or a decimal numeric GID. Use a group name for systemd or host-binary deployments. Use a numeric GID in static pod mode so the distroless non-root container does not depend on host group names being present inside the image.

For the full identity model and rationale see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

## Unsafe Options

The provider intentionally does not expose runtime switches for unsupported
release-boundary behavior. In particular, there is no config field to:

- enable unsafe debug endpoints,
- disable Transit associated data,
- enable KMS decrypt micro-batching,
- select alternate AAD read modes,
- log raw OpenBao paths.

Broad socket permissions remain rejected by validation.

## Environment Variables

The primary configuration source is the config file. Environment variables may be supported for container deployment ergonomics. Secrets must not be required through environment variables.

Allowed environment overrides are limited to:

- config path: `BAO_KMS_PROVIDER_CONFIG` or `BAO_KMS_PROVIDER_CONFIG_PATH`,
- log level: `BAO_KMS_PROVIDER_LOG_LEVEL` or `BAO_KMS_PROVIDER_LOGGING_LEVEL`,
- metrics listen address: `BAO_KMS_PROVIDER_SERVER_METRICS_ADDRESS` or `BAO_KMS_PROVIDER_SERVER_METRICSADDRESS`,
- health listen address: `BAO_KMS_PROVIDER_SERVER_HEALTH_ADDRESS` or `BAO_KMS_PROVIDER_SERVER_HEALTHADDRESS`,
- feature flags used only in tests.

Identity-bearing fields such as `openbao.namespace`, `auth.jwt.expectedIssuer`,
`auth.jwt.expectedAudience`, `auth.jwt.expectedSubject`, `auth.cert.name`,
`transit.keyName`, `transit.mountPath`, and `transit.keyIdScope.*` are not
environment overrides. Keep them in the reviewed config file so deployment
environments cannot silently drift the KMS identity contract.

## Schema Export

The CLI prints the JSON Schema used by documentation and tooling:

```sh
bao-kms-provider config schema
```

The schema rejects unknown top-level and nested fields, reserves `configVersion: v1alpha1`, and documents the supported configuration surface.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao JWT/OIDC auth API](https://openbao.org/api-docs/auth/jwt/)
- [OpenBao TLS certificates auth method](https://openbao.org/docs/auth/cert/)
- [OpenBao TCP listener configuration](https://openbao.org/docs/configuration/listener/tcp/)
- [SPIFFE Workload API](https://spiffe.io/docs/latest/spiffe-specs/spiffe_workload_api/)

---
title: "Configuration"
description: "Authoritative reference for the bao-kms-provider configuration file: example, defaults, validation rules, identity-bearing fields, and unsafe options."
weight: 20
---

# Configuration

This page is the authoritative reference for the v0.1 `bao-kms-provider` configuration file. Identity-bearing fields and default values are stable across v0.1 patch releases.

## Example

```yaml
configVersion: v1alpha1
server:
  socketPath: /run/openbao-kms/kms.sock
  socketMode: "0660"
  socketGroup: openbao-kms-socket
  metricsAddress: "127.0.0.1:8081"
  healthAddress: "127.0.0.1:8082"
  adminAddress: ""
  unsafeDebugEndpoints: false

openbao:
  address: https://bao.example.internal:8200
  namespace: ""
  caCertFile: /etc/openbao-kms/tls/ca.crt
  tlsServerName: bao.example.internal
  timeout: 2s
  instanceId: bao-prod-a

auth:
  method: jwt
  mountPath: auth/k8s-workload-a-jwt
  role: openbao-kms-control-plane
  jwtFile: /var/lib/openbao-kms/identity.jwt
  minJwtRemainingTtl: 2m
  clockSkewLeeway: 30s
  loginBeforeTokenExpiry: 5m
  tokenRenewalIncrement: 1h
  loginTimeout: 0s
  expectedIssuer: ""
  expectedAudience: []
  expectedSubject: ""
  tokenStorage: memory

transit:
  mountPath: transit
  keyName: k8s-workload-a-etcd
  keyIdScope:
    providerName: openbao-kms-workload-a
    clusterId: workload-a
    transitMountId: transit-prod-primary
    keyLineageId: "01HXEXAMPLEKEYLINEAGEID"
  useAssociatedData: true

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

performance:
  decryptMicroBatching:
    enabled: false
    maxBatchSize: 32
    maxWait: 2ms

logging:
  level: info
  format: json
  redactOpenBaoPaths: true
  logOpenBaoRequestIDs: true
  debugCorrelation:
    enabled: false
    ttl: 15m
    incidentId: ""
```

## Required Fields

The following fields must be set explicitly:

- `configVersion` (when authoring new configs; omitted legacy configs default to `v1alpha1`)
- `server.socketPath`
- `server.socketMode`
- `server.socketGroup`
- `openbao.address`
- `openbao.caCertFile`
- `openbao.tlsServerName`
- `openbao.instanceId`
- `auth.method`
- `auth.mountPath`
- `auth.role`
- `auth.jwtFile`
- `transit.mountPath`
- `transit.keyName`
- `transit.keyIdScope.providerName`
- `transit.keyIdScope.clusterId`
- `transit.keyIdScope.transitMountId`
- `transit.keyIdScope.keyLineageId`

## Defaults

| Field | Default |
|---|---|
| `configVersion` | `v1alpha1` |
| `server.socketPath` | `/run/openbao-kms/kms.sock` |
| `server.socketMode` | `"0660"` |
| `server.metricsAddress` | `127.0.0.1:8081` |
| `server.healthAddress` | `127.0.0.1:8082` |
| `server.adminAddress` | empty |
| `server.unsafeDebugEndpoints` | `false` |
| `openbao.timeout` | `2s` |
| `auth.method` | `jwt` |
| `auth.minJwtRemainingTtl` | `2m` |
| `auth.clockSkewLeeway` | `30s` |
| `auth.loginBeforeTokenExpiry` | `5m` |
| `auth.tokenRenewalIncrement` | `1h` |
| `auth.loginTimeout` | `max(openbao.timeout, 5s)` when unset or `0s` |
| `auth.expectedIssuer` | empty |
| `auth.expectedAudience` | empty |
| `auth.expectedSubject` | empty |
| `auth.tokenStorage` | `memory` |
| `transit.useAssociatedData` | `true` |
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
| `performance.decryptMicroBatching.enabled` | `false` |
| `performance.decryptMicroBatching.maxBatchSize` | `32` |
| `performance.decryptMicroBatching.maxWait` | `2ms` |
| `logging.level` | `info` |
| `logging.format` | `json` |
| `logging.redactOpenBaoPaths` | `true` |
| `logging.logOpenBaoRequestIDs` | `true` |
| `logging.debugCorrelation.enabled` | `false` |
| `logging.debugCorrelation.ttl` | `15m` |
| `logging.debugCorrelation.incidentId` | empty |

## Auth Timing

`auth.loginBeforeTokenExpiry` is the refresh-ahead threshold. Once the remaining OpenBao token TTL drops below this value, the provider renews or re-logs in before the next request.

`auth.tokenRenewalIncrement` is the requested TTL increment sent to OpenBao during `auth/token/renew-self`. Keep it larger than the refresh-ahead threshold and within the JWT role's maximum token TTL.

`auth.loginTimeout` can be left at `0s` to derive `max(openbao.timeout, 5s)`.

`bootstrap.graceTimeout` controls how long startup retries the initial status probe before the process exits. It exists to handle boot races such as JWT file projection, DNS or routing settling, OpenBao restart, and clock synchronization.

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
- `transit.keyIdScope.transitMountId`
- `transit.keyIdScope.keyLineageId`
- `transit.keyName`
- `transit.mountPath`
- Kubernetes `EncryptionConfiguration` provider name

Treat these values as immutable. Any change requires a documented migration plan; see [Operations: Disaster Recovery](/operations/disaster-recovery/) for the procedure.

The implementation exposes an identity fingerprint for these fields. Record it during rollout and compare it during troubleshooting without exposing raw cluster, OpenBao, or Transit topology values.

## Validation

Startup fails closed when any of the following conditions hold:

- config file permissions are unsafe,
- socket path is outside an approved runtime directory,
- socket parent directory is unsafe,
- socket path is a symlink or regular file,
- state path is not absolute,
- JWT file is unreadable,
- JWT is expired or too close to expiry,
- JWT `nbf` or `iat` claims are outside the configured clock-skew leeway,
- CA file is missing,
- OpenBao address is invalid or includes user info, query, or fragment data,
- TLS server name is empty,
- auth role or identity fields contain surrounding whitespace or control characters,
- provider name is empty,
- cluster ID is empty,
- OpenBao instance ID is empty,
- Transit mount ID is empty,
- key lineage ID is empty,
- Transit mount or key names are empty,
- AAD is enabled and required scope inputs are missing,
- socket mode is broader than configured policy allows,
- an unsupported compatibility mode is configured.

## Permissions

Recommended local permissions:

```text
/etc/openbao-kms/config.yaml        root:openbao-kms                0640
/etc/openbao-kms/tls/ca.crt         root:root                       0644
/var/lib/openbao-kms/identity.jwt   root:openbao-kms                0640
/var/lib/openbao-kms/state          openbao-kms:openbao-kms         0750
/run/openbao-kms                    openbao-kms:openbao-kms-socket  2750
/run/openbao-kms/kms.sock           openbao-kms:openbao-kms-socket  0660
```

The JWT file should be readable only by the provider process. The socket should be readable and writable only by the provider and the local API server identity. The socket directory should be writable only by the provider identity.

`server.socketGroup` accepts a local group name or a decimal numeric GID. Use a group name for systemd or host-binary deployments. Use a numeric GID in static pod mode so the distroless non-root container does not depend on host group names being present inside the image.

For the full identity model and rationale see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

## Unsafe Options

The following must not be enabled in production without a written exception:

- `server.unsafeDebugEndpoints`
- `performance.decryptMicroBatching.enabled`
- `transit.useAssociatedData: false`
- compatibility mode without explicit allowed epochs
- logging raw OpenBao paths
- broad socket permissions

For v0.1, `transit.useAssociatedData: false` is not a supported deployment mode. The field exists to reserve the compatibility surface for future testing and migration work.

## Environment Variables

The primary configuration source is the config file. Environment variables may be supported for container deployment ergonomics. Secrets must not be required through environment variables.

Allowed environment overrides are limited to:

- config path,
- log level,
- listen addresses for metrics and health,
- feature flags used only in tests.

## Schema Export

The CLI prints the JSON Schema used by documentation and tooling:

```sh
bao-kms-provider config schema
```

The schema rejects unknown top-level and nested fields, reserves `configVersion: v1alpha1`, and documents the supported v0.1 surface.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao JWT auth](https://openbao.org/docs/2.4.x/auth/jwt/)

# Configuration Reference

This document defines the intended plugin configuration model. The exact parser may change during implementation, but identity-bearing fields and defaults should remain stable unless an ADR changes them.

## Example

```yaml
server:
  socketPath: /run/openbao-kms/kms.sock
  socketMode: "0660"
  socketGroup: kube-apiserver
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
  loginBeforeTokenExpiry: 5m
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

status:
  probeInterval: 30s
  deepProbeInterval: 5m
  statusMaxStaleness: 2m

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
```

## Required Fields

Required for MVP:

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
| `server.socketPath` | `/run/openbao-kms/kms.sock` |
| `server.socketMode` | `"0660"` |
| `server.metricsAddress` | `127.0.0.1:8081` |
| `server.healthAddress` | `127.0.0.1:8082` |
| `server.adminAddress` | empty |
| `server.unsafeDebugEndpoints` | `false` |
| `auth.method` | `jwt` |
| `auth.tokenStorage` | `memory` |
| `transit.useAssociatedData` | `true` |
| `status.probeInterval` | `30s` |
| `status.deepProbeInterval` | `5m` |
| `status.statusMaxStaleness` | `2m` |
| `rotation.mode` | `observed` |
| `rotation.rejectVersionRollback` | `true` |
| `performance.decryptMicroBatching.enabled` | `false` |
| `logging.format` | `json` |
| `logging.redactOpenBaoPaths` | `true` |

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

Treat these values as immutable. Any change requires a documented migration plan.

## Validation

Startup must fail closed when:

- config file permissions are unsafe,
- socket path is outside an approved runtime directory,
- socket parent directory is unsafe,
- socket path is a symlink or regular file,
- JWT file is unreadable,
- JWT is expired or too close to expiry,
- CA file is missing,
- OpenBao address is invalid,
- TLS server name is empty,
- provider name is empty,
- cluster ID is empty,
- OpenBao instance ID is empty,
- Transit mount ID is empty,
- key lineage ID is empty,
- Transit mount/key names are empty,
- AAD is enabled and required scope inputs are missing,
- socket mode is broader than configured policy allows,
- unsupported compatibility mode is configured.

## Permissions

Recommended local permissions:

```text
/etc/openbao-kms/config.yaml        root:openbao-kms 0640
/etc/openbao-kms/tls/ca.crt         root:root 0644
/var/lib/openbao-kms/identity.jwt   root:openbao-kms 0640
/var/lib/openbao-kms/state          openbao-kms:openbao-kms 0750
/run/openbao-kms                    openbao-kms:openbao-kms-socket 2770
/run/openbao-kms/kms.sock           openbao-kms:openbao-kms-socket 0660
```

The JWT file should be readable only by the plugin process. The socket should be readable/writable only by the plugin and the local API server identity.

## Unsafe Options

The following should not be enabled in production without a written exception:

- `server.unsafeDebugEndpoints`
- `performance.decryptMicroBatching.enabled`
- `transit.useAssociatedData: false`
- compatibility mode without explicit allowed epochs,
- logging raw OpenBao paths,
- broad socket permissions.

For v0.1, `transit.useAssociatedData: false` is not a supported deployment mode. The field exists to reserve the compatibility surface for future testing and migration work.

## Environment Variables

The primary configuration source should be the config file. Environment variables may be supported for container deployment ergonomics, but secrets must not be required through environment variables.

Allowed environment overrides should be limited to:

- config path,
- log level,
- listen addresses for metrics/health,
- feature flags used only in tests.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao JWT auth](https://openbao.org/docs/2.4.x/auth/jwt/)

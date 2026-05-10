# Kubernetes Encryption Configuration

This guide defines how the plugin should be referenced from Kubernetes `EncryptionConfiguration`.

## Scope

Kubernetes encryption-at-rest applies to selected Kubernetes API resources before they are persisted to etcd. It does not encrypt node disks, workload volumes, pod filesystems, or arbitrary network traffic.

## KMS v2 Provider Example

Example:

```yaml
apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
      - configmaps
    providers:
      - kms:
          apiVersion: v2
          name: openbao-kms-workload-a
          endpoint: unix:///run/openbao-kms/kms.sock
          timeout: 3s
      - identity: {}
```

The maintained sample lives at [`deploy/kubernetes/encryption-config.yaml`](../deploy/kubernetes/encryption-config.yaml).

The `identity` fallback allows reads of existing plaintext data during initial migration. It should be removed after all targeted resources have been rewritten and verified.

## Provider Name

The provider name is identity-bearing:

```yaml
name: openbao-kms-workload-a
```

It must match:

- `transit.keyIdScope.providerName` in plugin config,
- the value used for key ID derivation,
- the value used for AAD scope.

Do not change it after encryption begins without a migration plan.

## Endpoint

The endpoint must use the plugin Unix socket:

```yaml
endpoint: unix:///run/openbao-kms/kms.sock
```

The socket path must match `server.socketPath` in plugin config. Every control-plane node must have a local plugin instance serving the same endpoint path.

## Timeout

Start with:

```yaml
timeout: 3s
```

Tightening this value should be based on benchmark and failure-mode testing. The plugin should target much lower normal latency, but startup decrypt storms and OpenBao failover can create tail latency.

## Resource Selection

Recommended starting point:

```yaml
resources:
  - secrets
```

Common additions:

```yaml
resources:
  - secrets
  - configmaps
```

CRDs can be encrypted, but operators should understand the size, read/write volume, and recovery impact before adding broad resource sets.

## Initial Migration

Encryption applies on write. Existing objects are not automatically rewritten just because the encryption configuration changes.

Initial enablement sequence:

1. Configure KMS v2 with `identity` fallback last.
2. Start plugin on every control-plane node.
3. Restart or reload kube-apiserver.
4. Create a new test Secret and confirm it is readable.
5. Confirm etcd does not contain the plaintext test Secret value.
6. Rewrite existing targeted resources.
7. Verify reads after API server restart.
8. Remove `identity` fallback.
9. Restart or reload kube-apiserver.
10. Verify reads again.

Common rewrite command for Secrets:

```sh
kubectl get secrets --all-namespaces -o json | kubectl replace -f -
```

Adjust commands for each encrypted resource type.

## Removing Identity Fallback

After migration, remove:

```yaml
- identity: {}
```

Leaving `identity` fallback in place indefinitely increases the chance that future misconfiguration or provider ordering mistakes produce plaintext data.

Removing fallback too early can break reads of plaintext objects that were not migrated. Verify first.

## Verification

Recommended verification:

```sh
kubectl create secret generic openbao-kms-test \
  --from-literal=value='do-not-store-plaintext'

kubectl get secret openbao-kms-test -o jsonpath='{.data.value}' | base64 -d
```

Then inspect etcd directly in a controlled admin environment and confirm the plaintext string is not present.

Do not run ad hoc etcd inspections from untrusted shells or store Secret plaintext in logs.

## Automatic Reload

Kubernetes supports automatic reload of encryption provider configuration when `kube-apiserver` is configured with:

```text
--encryption-provider-config-automatic-reload=true
```

Use reload carefully. Socket path, provider name, and provider ordering mistakes can still affect API server behavior.

## Source References

- [Kubernetes encryption at rest documentation](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)

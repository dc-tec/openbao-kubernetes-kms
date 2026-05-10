# Architecture

This document defines the expected high-level architecture for the OpenBao Kubernetes KMS provider.

## Purpose

The provider adapts Kubernetes KMS v2 to OpenBao Transit. Kubernetes talks to a local KMS plugin over gRPC. The plugin talks to OpenBao Transit over HTTPS.

```text
kube-apiserver
  -> Unix domain socket
  -> bao-kms-provider
  -> OpenBao Transit
  -> encrypted Kubernetes API resource data in etcd
```

The provider participates in Kubernetes envelope encryption for selected API resources. It does not encrypt raw etcd disk blocks or any workload storage outside the Kubernetes API resource persistence path.

## Components

### kube-apiserver

`kube-apiserver` is configured with an `EncryptionConfiguration` that contains a KMS v2 provider entry. The provider name and endpoint are part of the Kubernetes encryption format and must be treated as stable after encryption begins.

### bao-kms-provider

The plugin process is responsible for:

- serving the Kubernetes KMS v2 gRPC API over a Unix domain socket,
- maintaining the active key snapshot,
- returning cached KMS `Status`,
- validating decrypt `key_id` and annotations,
- constructing Transit associated data when enabled,
- authenticating to OpenBao,
- calling Transit encrypt and decrypt APIs,
- exposing local health and metrics endpoints,
- producing structured redacted logs.

### OpenBao

OpenBao provides:

- JWT authentication,
- short-lived OpenBao tokens,
- Transit key metadata,
- Transit encrypt/decrypt operations,
- audit records for cryptographic operations.

OpenBao must be available independently of the protected Kubernetes API server. Running OpenBao only inside the same protected cluster creates a bootstrap dependency during API server recovery.

## Data Flow

### Encrypt

1. `kube-apiserver` sends `Encrypt(plaintext, uid)` to the plugin.
2. The plugin reads the active key snapshot.
3. The plugin builds non-secret annotations.
4. If AAD is enabled, the plugin builds canonical associated data.
5. The plugin calls OpenBao Transit encrypt with explicit `key_version`.
6. The plugin returns ciphertext, the active Kubernetes `key_id`, and annotations.
7. `kube-apiserver` stores the encrypted resource data in etcd.

Encrypt must not use implicit Transit latest-version behavior.

### Decrypt

1. `kube-apiserver` sends `Decrypt(ciphertext, key_id, annotations, uid)` to the plugin.
2. The plugin verifies the `key_id` is known.
3. The plugin verifies annotations are present and internally consistent.
4. If AAD is enabled for the object epoch, the plugin reconstructs the same AAD.
5. The plugin calls OpenBao Transit decrypt.
6. The plugin returns plaintext to `kube-apiserver`.

Decrypt must not brute-force unknown keys or try every historical key. Unknown `key_id` values fail before Transit is called.

### Status

`Status` is the Kubernetes health and active key signal. It returns the plugin API version, health state, and active `key_id`.

`Status` must be cheap. It must read from cached state populated by background probes, not perform a live Transit encrypt/decrypt on every call.

## Trust Boundaries

Important boundaries:

- Kubernetes API server to local plugin socket.
- Plugin host process to OpenBao HTTPS endpoint.
- OpenBao policy boundary for Transit operations.
- Local host filesystem boundary for config, JWT file, CA bundle, and socket.
- etcd persistence boundary for ciphertext and KMS annotations.

The plugin sees plaintext material passed through KMS calls. It must be treated as a control-plane critical component.

## Deployment Models

### systemd

Recommended for hardened production deployments where the operator controls host services. It avoids depending on kubelet and container runtime availability to start the KMS plugin.

### Static Pod

Useful for kubeadm-style deployments and Kubernetes-native packaging. The manifest must be self-contained because static Pods cannot depend on API objects such as ConfigMaps, Secrets, or ServiceAccounts.

### DaemonSet

Not recommended for protecting the same cluster's API server. A DaemonSet depends on a functioning API server and scheduler path, which makes it unsuitable for the API server bootstrap dependency.

## State

The implementation should maintain an active key snapshot containing:

- provider name,
- cluster ID,
- OpenBao instance ID,
- Transit mount ID,
- Transit key lineage ID,
- Transit key version,
- Transit key version creation timestamp,
- Kubernetes `key_id`,
- AAD mode,
- observed rotation state.

The design prefers deriving historical key IDs from stable config and Transit metadata. If that is not enough to prove restart and rotation safety, the implementation must add a small local key registry state file with strict permissions and backup guidance.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [Kubernetes encryption at rest documentation](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [Kubernetes static Pods](https://kubernetes.io/docs/tasks/configure-pod-container/static-pod/)


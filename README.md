# OpenBao Kubernetes KMS

`bao-kms-provider` is an OpenBao-backed Kubernetes KMS v2 provider. It lets
`kube-apiserver` envelope-encrypt selected Kubernetes API resources at rest in
etcd using the OpenBao Transit secrets engine.

This project is pre-release. The current target is a v0.1 engineering preview.
Do not assume production readiness, published support, signed release artifacts,
or release provenance until the documented release gates have passed.

## What It Does

The provider runs on each Kubernetes control-plane node and exposes the
Kubernetes KMS v2 gRPC API over a local Unix domain socket. The API server calls
that socket during encryption and decryption, and the provider authenticates to
OpenBao before calling Transit.

```text
kube-apiserver
  -> local Unix domain socket
  -> bao-kms-provider
  -> OpenBao JWT auth
  -> OpenBao Transit encrypt/decrypt
  -> KMS v2 envelope stored in etcd
```

This protects selected Kubernetes API resources through Kubernetes encryption at
rest. It does not encrypt raw etcd disk blocks, etcd snapshots, node
filesystems, PersistentVolumes, pod filesystems, or Kubernetes network traffic.

## Status

| Area | Current state |
|---|---|
| Release channel | No published release yet. |
| Maturity | v0.1 engineering-preview implementation track. |
| Kubernetes target | KMS v2 on the Kubernetes 1.34 release line, with current CI/lab pins documented in `.ci/versions.yaml`. |
| OpenBao target | OpenBao `2.5.3` with Transit and JWT auth. |
| Deployment models | systemd and kubelet-managed static pod. |
| Same-cluster DaemonSet | Not supported for protecting that cluster's own API server. |
| Supply chain | Vendor, SBOM, scan, and reproducibility scaffolding exists. First signed artifacts, provenance verification output, and release evidence are still pending. |

The provider is control-plane critical. If the provider socket, JWT credential,
OpenBao endpoint, or Transit key is unavailable, `kube-apiserver` may be unable
to decrypt previously encrypted resources during startup.

## Recommended Reading

Start with the published docs:

1. [Overview](https://dc-tec.github.io/openbao-kubernetes-kms/getting-started/overview/)
2. [OpenBao setup](https://dc-tec.github.io/openbao-kubernetes-kms/getting-started/openbao-setup/)
3. [Install](https://dc-tec.github.io/openbao-kubernetes-kms/getting-started/install/)
4. [Kubernetes EncryptionConfiguration](https://dc-tec.github.io/openbao-kubernetes-kms/getting-started/kubernetes-encryption-config/)
5. [First encrypt](https://dc-tec.github.io/openbao-kubernetes-kms/getting-started/first-encrypt/)

## Deployment Model Summary

| Model | Use when | Notes |
|---|---|---|
| systemd | You control the host OS lifecycle and want the provider available before kubelet starts the static-pod API server. | Preferred for lower bootstrap dependency when host management is available. |
| Static pod | The control plane is kubeadm-style and operators want a node-local manifest managed by kubelet. | Requires kubelet, container runtime, hostPath mounts, and preloaded or reliably available images. |
| DaemonSet | Not for protecting the same cluster's API server. | DaemonSets depend on the Kubernetes API server they would be required to unlock. |

See [Deployment: Choosing A Model](https://dc-tec.github.io/openbao-kubernetes-kms/deployment/choosing-a-model/)
for the full rationale.

## Validation Coverage

The repository contains portable and local-only validation layers:

- unit, race, fuzz, lint, static security, license, vendor, and vulnerability checks,
- KMS v2 conformance tests over the real Unix-socket server path,
- OpenBao `2.5.3` Transit and JWT auth E2E tests,
- JWT bound-claim and pinned signing-key rollover tests,
- provider/OpenBao failure, HA failover, restore, rotation, load-soak, and upgrade/rollback E2E tests,
- pinned Kind KMS v2 smoke, multi-control-plane convergence, static-pod upgrade/rollback, and DR runbook tests,
- local-only Harvester kubeadm VM gates for systemd, static pod, OpenBao outage, paired restore, upgrade/rollback, load smoke, and multi-control-plane recovery.

Run the local core gate with:

```sh
make ci-core
```

Selected E2E entrypoints:

```sh
make test-e2e-openbao-ci
make test-e2e-provider-ha-openbao-ci
make test-e2e-kind-smoke
```

The Harvester kubeadm lab is intentionally local-only and must not be added to
public CI. See [Harvester Kubeadm Lab](https://dc-tec.github.io/openbao-kubernetes-kms/development/harvester-kubeadm-lab/).

## Build From Source

The repository commits `vendor/` and CI uses `GOFLAGS=-mod=vendor`.

```sh
make build
bin/bao-kms-provider version
```

Build a local container image:

```sh
make image
```

Release packages and bundles are produced by the release targets, but no
published release channel exists yet:

```sh
make release-distribution
```

## Upstream References

- [Kubernetes: Using a KMS provider for data encryption](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [Kubernetes: Encrypting confidential data at rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [Kubernetes: Static Pods](https://kubernetes.io/docs/tasks/configure-pod-container/static-pod/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao JWT auth](https://openbao.org/docs/2.4.x/auth/jwt/)

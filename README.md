# OpenBao Kubernetes KMS

Design-stage OpenBao-native Kubernetes KMS v2 provider for encrypting selected Kubernetes API resources at rest in etcd using the OpenBao Transit secrets engine.

This repository currently contains design and planning documentation. It is not yet a production implementation.

## Scope

The provider is intended to run on each Kubernetes control-plane node as a local gRPC KMS plugin. `kube-apiserver` connects to the plugin over a Unix domain socket. The plugin authenticates to OpenBao, calls Transit encrypt/decrypt APIs, and returns Kubernetes KMS v2 responses.

Target flow:

```text
kube-apiserver
  -> local Unix domain socket
  -> bao-kms-provider
  -> OpenBao JWT auth
  -> OpenBao Transit encrypt/decrypt
  -> encrypted Kubernetes API resource data in etcd
```

This plugin is for Kubernetes encryption-at-rest of selected API resources. It does not encrypt etcd disk blocks, node filesystems, persistent volumes, pod filesystems, or Kubernetes network traffic.

## Current Status

Status: design and documentation phase.

No implementation, binary, image, Helm chart, or release artifact should be assumed to exist yet. The documents in this repository define the expected behavior before implementation starts.

## Design Direction

The design is intentionally conservative:

- Kubernetes KMS v2 only by default.
- Kubernetes 1.34 release-line validation target for v0.1.
- OpenBao Transit as the remote key encryption service.
- JWT-first OpenBao authentication from a host-mounted JWT file.
- Explicit Transit `key_version` on every encrypt operation.
- Opaque Kubernetes `key_id` values.
- Strict decrypt-side `key_id` and annotation validation.
- Associated data required for v0.1 deployments.
- Cheap cached KMS `Status`; OpenBao probing runs in the background.
- No Transit key creation, deletion, export, backup, or rotation from the hot-path plugin.
- systemd and static pod deployment models.

## Documentation Map

Start here:

- [Design](docs/design.md)
- [Documentation index](docs/index.md)
- [Architecture](docs/architecture.md)
- [KMS v2 contract](docs/kms-v2-contract.md)
- [Key ID and AAD](docs/key-id-and-aad.md)
- [Configuration](docs/configuration.md)
- [OpenBao setup](docs/openbao-setup.md)
- [Code quality](docs/code-quality.md)
- [Testing strategy](docs/testing-strategy.md)
- [Release gates](docs/release-gates.md)
- [Release policy](docs/release-policy.md)
- [Support policy](docs/support-policy.md)
- [Research notes](docs/research-notes.md)
- [Implementation backlog](docs/workstreams/implementation-backlog.md)

Operator docs:

- [Install](docs/install.md)
- [Kubernetes encryption config](docs/kubernetes-encryption-config.md)
- [systemd deployment](docs/deployment/systemd.md)
- [Static pod deployment](docs/deployment/static-pod.md)
- [Linux identity model](docs/deployment/linux-identity-model.md)
- [Rotation](docs/operations/rotation.md)
- [Disaster recovery](docs/operations/disaster-recovery.md)
- [Troubleshooting](docs/troubleshooting.md)

Security and maintenance:

- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Threat model](docs/security/threat-model.md)
- [Hardening](docs/security/hardening.md)
- [Observability](docs/observability.md)
- [CLI reference](docs/cli.md)
- [Development guide](docs/development.md)
- [Code quality](docs/code-quality.md)
- [Compatibility](docs/compatibility.md)
- [CI and supply chain](docs/ci-supply-chain.md)
- [Architecture decisions](docs/adr)
- [Workstreams](docs/workstreams)

## Upstream References

The documentation is based on upstream Kubernetes and OpenBao behavior:

- [Kubernetes: Using a KMS provider for data encryption](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [Kubernetes: Encrypting confidential data at rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [Kubernetes: Static Pods](https://kubernetes.io/docs/tasks/configure-pod-container/static-pod/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao JWT auth](https://openbao.org/docs/2.4.x/auth/jwt/)

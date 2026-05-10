---
title: "Harvester Kubeadm Lab"
description: "Local-only Harvester VM harness for kubeadm, systemd, static-pod, and OpenBao release-gate testing."
weight: 55
---

# Harvester Kubeadm Lab

The Harvester kubeadm lab is a local-only release-gate harness. It is not part
of CI and must not be wired into pull-request checks. It creates real Harvester
VMs so host-level kubeadm, kubelet, systemd, package install, reboot, and static
pod behavior can be tested outside Kind.

The harness uses Helm to create Harvester/KubeVirt VM resources. Helm is used
only for the outer lab substrate. It is not used to install the KMS provider into
the protected kubeadm cluster.

## Layout

```text
deploy/harvester/openbao-kms-lab/   Helm chart for lab VMs
hack/harvester/lab.sh               Thin wrapper around the Go lab orchestrator
hack/harvester/remote/              Guest-side Ubuntu/OpenBao/kubeadm payloads
hack/tools/harvester_lab/           Host-side lifecycle orchestration
artifacts/harvester/                Generated SSH config and runtime metadata
kubeconfig.yaml                     Local Harvester kubeconfig, ignored by git
```

Default VM roles:

| VM | Role |
|---|---|
| `obk-openbao-1` | OpenBao host |
| `obk-kubeadm-systemd-1` | kubeadm control plane using systemd provider deployment |
| `obk-kubeadm-static-1` | kubeadm control plane using static-pod provider deployment |

## Prepare Values

The local values file is ignored by git.

```sh
cp hack/harvester/env.example hack/harvester/env.local
. hack/harvester/env.local

export HARVESTER_IMAGE_NAME=image-wjmvv
export HARVESTER_NETWORK_NAME=default/vm4000
# Only set this for local Harvester endpoints with a mismatched serving cert.
# export HARVESTER_INSECURE_SKIP_TLS_VERIFY=true
make harvester-lab-values
```

Review `hack/harvester/values.local.yaml` before creating VMs.

## Create VMs

Render first:

```sh
make harvester-lab-lint
make harvester-lab-render
make harvester-lab-dry-run
```

Create:

```sh
make harvester-lab-create
make harvester-lab-status
make harvester-lab-wait
make harvester-lab-wait-ssh
```

For a full local run after the values file is prepared:

```sh
make harvester-lab-e2e
```

This target is intentionally local-only. Do not add it to pull-request CI.

The SSH config is written to:

```text
artifacts/harvester/ssh-config
```

Example:

```sh
ssh -F artifacts/harvester/ssh-config obk-openbao
ssh -F artifacts/harvester/ssh-config obk-kubeadm-systemd
ssh -F artifacts/harvester/ssh-config obk-kubeadm-static
```

## Bootstrap Guests

After VM lifecycle and SSH are working, bootstrap the guest software:

```sh
make harvester-lab-bootstrap-guests
make harvester-lab-verify-guests
```

This installs the pinned OpenBao version from `.ci/versions.yaml` on
`obk-openbao-1`, configures TLS, Transit, and JWT auth, then installs the pinned
kubeadm, kubelet, and kubectl version from `.ci/versions.yaml` on both kubeadm
VMs.

Generated lab identity material, the OpenBao CA, and kubeadm admin kubeconfigs
are written under `artifacts/harvester/`, which is ignored by git. OpenBao
initialization material stays on the OpenBao VM under `/root/openbao-kms-lab/`
and is not copied back to the workstation.

## Wire Provider

After guest bootstrap verifies cleanly, wire the provider into both kubeadm
clusters:

```sh
make harvester-lab-wire-provider
make harvester-lab-verify-kms
```

The systemd VM receives the provider as `/usr/bin/bao-kms-provider` managed by
`bao-kms-provider.service`. The static-pod VM receives a locally built
`linux/amd64` provider image imported into containerd and installed as a
kubelet-managed static pod.

Both paths install a generated provider config, the OpenBao CA, the generated
lab JWT, and a Kubernetes `EncryptionConfiguration`. The kube-apiserver static
pod manifest is patched to mount `/run/openbao-kms` and the encryption config.
Verification writes a new Kubernetes Secret, checks Kubernetes readback, and
checks raw etcd storage for the Kubernetes KMS v2 envelope marker without
printing the Secret value.

## Destroy

```sh
make harvester-lab-destroy
```

By default the destroy target also deletes lab PVCs selected by the Helm release
label. Set `DELETE_PVCS=false` to preserve disks while removing the release.

## Next Layer

## Implementation Model

The host-side harness is implemented in Go under `hack/tools/harvester_lab`.
Make targets call `hack/harvester/lab.sh`, which runs
`harvester_lab lab <step>`. This keeps local state handling, templating,
Kubernetes object parsing, HTTP verification, provider asset generation, and
command sequencing in one testable place.

Shell is kept only for guest-side payloads under `hack/harvester/remote/`,
where it is the most direct way to install apt packages, write systemd units,
and run kubeadm inside the Ubuntu VMs.

## Next Layer

This setup creates the VMs, bootstraps OpenBao plus kubeadm, wires both provider
deployment modes, and validates raw etcd envelope storage. Follow-up local
release-gate layers should add:

- API server restart and VM reboot recovery,
- OpenBao outage, restart, and restore behavior.

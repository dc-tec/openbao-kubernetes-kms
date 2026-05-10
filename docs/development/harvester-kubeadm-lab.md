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

Optional multi-control-plane VM roles:

| VM | Role |
|---|---|
| `obk-kubeadm-mcp-1` | kubeadm control plane using static-pod provider deployment |
| `obk-kubeadm-mcp-2` | kubeadm control plane using static-pod provider deployment |
| `obk-kubeadm-mcp-3` | kubeadm control plane using static-pod provider deployment |

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

To add the optional multi-control-plane topology, enable it before rendering
values:

```sh
export HARVESTER_ENABLE_MULTI_CONTROL_PLANE=true
make harvester-lab-values
```

This adds three extra kubeadm control-plane VMs. It is intended for the local
production confidence gate, not the smaller default lab.

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
ssh -F artifacts/harvester/ssh-config obk-kubeadm-mcp-1
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

If the optional multi-control-plane topology is enabled, bootstrap it after the
base guests are healthy:

```sh
make harvester-lab-bootstrap-mcp
```

The first multi-control-plane VM runs `kubeadm init` with uploaded
control-plane certificates. The remaining multi-control-plane VMs join with
`kubeadm join --control-plane`. The generated per-node kubeconfigs under
`artifacts/harvester/` point at each node's own API server so failover checks can
exercise every control-plane endpoint directly without requiring an external
load balancer.

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

If the optional multi-control-plane topology is enabled, wire the static-pod
provider into every multi-control-plane node:

```sh
make harvester-lab-wire-mcp
```

All multi-control-plane nodes use the same provider cluster ID and Transit key
lineage so the kube-apiserver instances agree on KMS `Status` and can decrypt
Secrets written through another API server.

## Production Confidence Gate

After the base lab has passed, run the local-only production confidence gate:

```sh
make harvester-lab-production-gate
```

This target assumes the VMs are already created, bootstrapped, and wired. It is
intentionally destructive within the lab: it restarts the systemd and static-pod
providers, restarts kube-apiserver containers, restarts and unseals OpenBao,
reboots the kubeadm VMs, reboots the OpenBao VM, stops OpenBao to verify cached
API server writes stay envelope-encrypted and cold writes fail closed after API
server restart, restores a paired OpenBao, provider state, and etcd backup,
exercises provider upgrade and rollback for both deployment modes, and creates a
small set of KMS-encrypted Kubernetes Secrets on both clusters.

The gate is split into smaller targets so failures can be rerun without
recreating the lab:

```sh
make harvester-lab-verify-recovery
make harvester-lab-verify-openbao-outage
make harvester-lab-verify-upgrade-rollback
make harvester-lab-verify-paired-restore
make harvester-lab-verify-mcp-recovery
make harvester-lab-verify-load
```

Set `HARVESTER_LOAD_SECRET_COUNT` to change the load-smoke size. The default is
`25` Secrets per kubeadm cluster. These targets must remain local-only and must
not be added to public pull-request CI.

When `HARVESTER_ENABLE_MULTI_CONTROL_PLANE=true`, the
`harvester-lab-production-gate` target also runs the multi-control-plane
recovery gate.
That gate writes and reads KMS-encrypted Secrets through each API server,
verifies the raw etcd envelope on every member, restarts every provider static
pod, restarts every kube-apiserver, and reboots every multi-control-plane VM one
at a time while verifying writes through the surviving API servers during each
outage.

## Destroy

```sh
make harvester-lab-destroy
```

By default the destroy target also deletes lab PVCs selected by the Helm release
label. Set `DELETE_PVCS=false` to preserve disks while removing the release.

## Implementation Model

The host-side harness is implemented in Go under `hack/tools/harvester_lab`.
Make targets call `hack/harvester/lab.sh`, which runs
`harvester_lab lab <step>`. This keeps local state handling, templating,
Kubernetes object parsing, HTTP verification, provider asset generation, and
command sequencing in one testable place.

Shell is kept only for guest-side payloads under `hack/harvester/remote/`,
where it is the most direct way to install apt packages, write systemd units,
run kubeadm inside the Ubuntu VMs, and perform guest-local backup and restore
operations for OpenBao, provider state, and etcd.

## Remaining Gaps

This setup creates the VMs, bootstraps OpenBao plus kubeadm, wires both provider
deployment modes, validates raw etcd envelope storage, and runs destructive
local recovery checks. OpenBao HA failover is covered by the portable
provider/OpenBao integrated-raft e2e lane. The optional Harvester
multi-control-plane topology closes the remaining kubeadm recovery gap in a real
VM substrate while staying outside public CI.

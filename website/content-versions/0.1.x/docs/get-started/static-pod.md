---
title: Run as a static pod
description: "Stage the kit and preload the provider image, then run the generated node setup phases to prepare the host, install its files, check it, and start the provider as a kubelet static pod on each control-plane node."
eyebrow: Get started · Step 5
weight: 50
verifiedBy:
  - deploy/static-pod/bao-kms-provider.yaml
  - deploy/config/provider-static-pod.yaml
  - hack/harvester/remote/install-provider-static-pod.sh
  - test/dev-env/scripts/stage-provider.sh
  - internal/scaffold/nodescript.go
---

Repeat this procedure on every control-plane node. At the end, kubelet runs the
provider next to the API server as UID `65532`, the provider listens on
`/run/openbao-kms/kms.sock`, and it has passed `doctor` against OpenBao.

Static pods cannot use ConfigMaps, Secrets, or ServiceAccounts, because the
API server might need the provider before those objects are readable.
Everything the provider needs comes from host files.

## Before you begin

- Use a kubeadm-style control plane that runs `kube-apiserver` as a static pod
  on containerd.
- Download and verify the static-pod bundle in
  [Download the release](/docs/get-started/download/), and keep that shell with
  `VERSION` and `ARCH` set.
- Have the reviewed output of `init --model static-pod` from
  [Generate installation files](/docs/get-started/plan-values/), and complete
  [Prepare OpenBao](/docs/get-started/openbao/).
- Obtain the OpenBao CA bundle as `ca.crt` and the provider host JWT as
  `identity.jwt`. The JWT must be renewable without the protected API server.

If your values use `auth.jwt.source: oauth2`, stage the client secret and
issuer CA bundle instead of `identity.jwt`; see
[OAuth 2.0 client credentials](/docs/configure/oauth2/). The generated pod
mounts the credential directory read-only for secret rotation.

## Step 1: Stage the kit and image

Extract the verified kit on the node and install its host binary. `node-setup.sh`
runs its checks with this binary:

```sh
tar -xzf "bao-kms-provider_${VERSION}_static-pod_linux_${ARCH}.tar.gz"
cd "bao-kms-provider_${VERSION}_static-pod_linux_${ARCH}"
sudo install -o root -g root -m 0755 bin/bao-kms-provider /usr/bin/bao-kms-provider
```

Preload the provider image into containerd, so the provider can start during
recovery without registry access:

```sh
sudo crictl pull "$(cat image-ref.txt)"
```

`image-ref.txt` pins the image by `@sha256` digest. The kit's signed checksum
already covers that digest, and containerd pulls only content that matches it,
so no separate image check is required. For an additional signature and build
provenance check, see
[Security: Verify release artifacts](/docs/security/verify-release-artifacts/#verify-the-provider-image).

For air-gapped nodes, export the image once and import it on each node; see
[Transfer to a disconnected environment](/docs/security/verify-release-artifacts/#transfer-to-a-disconnected-environment).
Keep the previous release's image on every node for rollback.

## Step 2: Copy the generated files

Copy the reviewed `generated/` directory from `init --model static-pod` to the
node, next to `ca.crt` and the credential. Read `generated/node-setup.sh`
before you run it: it is plain `sh`, and every path, owner, mode, the image
digest, and the socket GID come from the same generation as the manifest.

Run it without arguments to print its phases and the identity fingerprint:

```sh
sh generated/node-setup.sh
```

Compare the fingerprint with `generated/installation.json` and your recorded
cluster identity. Every node must print the same fingerprint.

## Step 3: Run the node setup phases

Run each phase as root, in order. Each phase prints what it changed and the
command for the next one, and stops with a non-zero exit status when something
is wrong.

```sh
sudo sh generated/node-setup.sh prepare
sudo sh generated/node-setup.sh install --ca ca.crt --credential identity.jwt
sudo sh generated/node-setup.sh check
sudo sh generated/node-setup.sh start
```

| Phase | What it does |
|---|---|
| `prepare` | Checks the host tools, the installed binary, the preloaded image, and that `openbao-kms-socket` has the GID used at generation. Creates the directories with numeric owner `65532` and the tmpfiles entry that recreates the socket directory after every reboot. |
| `install` | Installs `config.yaml`, the CA bundle, and the credential with group `65532`. Stops if any of them already exists. |
| `check` | Runs `config`, `verify-key`, and `doctor` as UID `65532` with the socket group, and confirms the fingerprint. |
| `start` | Installs `bao-kms-provider.yaml` in `/etc/kubernetes/manifests`, then waits for HTTP 200 from `/ready`. Runs only after a passed `check` within the last hour. |

If `prepare` reports a different socket GID on this node, generate this node's
files with the command it prints, as described in
[Reuse the identity on other nodes](/docs/get-started/plan-values/#reuse-the-identity-on-other-nodes).

For native OAuth, pass the client secret as `--credential`, and the issuer CA
bundle as `--issuer-ca` when the configuration sets
`auth.jwt.oauth2.caCertFile`; see
[OAuth 2.0 client credentials](/docs/configure/oauth2/).

The pod mounts the credential directory read-only. Configure the host issuer
agent to replace the credential atomically within that directory and preserve
its permissions. Keep unrelated files out of this directory. A file-only bind
mount retains the old JWT after atomic replacement.

`check` exits with status `4` when `verify-key` or `doctor` reports a `[fail]`
check. Read any `[warn]` lines before you run `start`. If `start` times out
waiting for `/ready`, inspect the pod with
`sudo crictl ps -a --name bao-kms-provider` and `sudo crictl logs <container-id>`.

After the local API server starts using the socket, probe it from the
extracted kit with the API server's own UID and groups:

```sh
sudo sh bin/probe-apiserver
```

The helper requires `pgrep`, `awk`, and `setpriv`, and exactly one running local
`kube-apiserver`. Status, Encrypt, and Decrypt must pass. Do not substitute the
provider UID to claim consumer access.

Continue with [Enable encryption](/docs/get-started/enable-encryption/) once the
provider runs on every control-plane node.

## About the manifest

The maintained manifest is `deploy/static-pod/bao-kms-provider.yaml`. The
settings that matter for the control-plane boot path:

| Setting | Purpose |
|---|---|
| `hostNetwork: true` | Reaches OpenBao before the Container Network Interface (CNI) is available. |
| `priorityClassName: system-node-critical` | Keeps the provider scheduled with other control-plane components. |
| `automountServiceAccountToken: false` | Avoids any dependency on protected-cluster ServiceAccount tokens. |
| `runAsUser: 65532`, `supplementalGroups` | Runs as the distroless non-root user, with socket access through the host socket GID. |
| `readOnlyRootFilesystem`, `capabilities.drop: [ALL]`, `allowPrivilegeEscalation: false` | Limits writes to the hostPath mounts and removes Linux capabilities. |
| Image by digest, `imagePullPolicy: IfNotPresent` | Starts from the preloaded image without registry access or tag drift. |
| Startup probe on `/live` | Allows about two minutes for bootstrap before liveness and readiness probes start. |

The startup probe budget covers the default 60-second `bootstrap.graceTimeout`.
If you raise that timeout or the auth and request timeouts, raise the probe's
`failureThreshold` to match.

Static-pod mode adds kubelet, the container runtime, and the local image to
the provider's boot path. If any of them is broken, the provider does not
start and the API server cannot decrypt existing resources. For single-node
control planes, prefer [Run with systemd](/docs/get-started/systemd/). For the
full hardening surface, see [Security: Hardening](/docs/security/hardening/).

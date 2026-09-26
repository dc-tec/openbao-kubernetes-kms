---
title: Run as a static pod
description: "Verify and preload the provider image, prepare host files with numeric ownership, validate with doctor, and start the provider as a kubelet-managed static pod on each control-plane node."
eyebrow: Get started · Step 6
weight: 70
verifiedBy:
  - deploy/static-pod/bao-kms-provider.yaml
  - deploy/config/provider-static-pod.yaml
  - hack/harvester/remote/install-provider-static-pod.sh
  - test/dev-env/scripts/stage-provider.sh
---

Repeat this procedure on every control-plane node. At the end, kubelet runs the
provider next to the API server as UID `65532`, the provider listens on
`/run/openbao-kms/kms.sock`, and it has passed `doctor` against OpenBao.

Static pods cannot use ConfigMaps, Secrets, or ServiceAccounts, because the
API server might need the provider before those objects are readable.
Everything the provider needs comes from host files.

## Before you begin

- Download and verify the static-pod bundle in
  [Download the release](/docs/get-started/download/), and keep that shell with
  `VERSION`, `ARCH`, `REPO`, and `WORKFLOW_IDENTITY` set.
- Download and verify the systemd tarball of the same release as well. You use
  its `bao-kms-provider` binary on the host to run `doctor`.
- Have the values from [Plan identity values](/docs/get-started/plan-values/)
  and the lineage ID from [Prepare OpenBao](/docs/get-started/openbao/).
- Obtain the OpenBao CA bundle as `ca.crt` and the provider host JWT as
  `identity.jwt`. The JWT must be renewable without the protected API server.
- Use a kubeadm-style control plane that runs `kube-apiserver` as a static pod
  on containerd.

## Step 1: Extract the bundle

```sh
tar -xzf "bao-kms-provider_${VERSION}_static-pod.tar.gz"
cd "bao-kms-provider_${VERSION}_static-pod"
cat image-ref.txt
```

`image-ref.txt` holds the provider image reference, ending in
`@sha256:<digest>`. Place the diagnostic binary from the systemd tarball on the
host:

```sh
tar -xzf "../bao-kms-provider_${VERSION}_systemd_linux_${ARCH}.tar.gz" \
  --strip-components=2 "bao-kms-provider_${VERSION}_systemd_linux_${ARCH}/bin/bao-kms-provider"
sudo install -o root -g root -m 0755 bao-kms-provider /usr/bin/bao-kms-provider
```

## Step 2: Verify the provider image

Set `IMAGE` to the digest reference, replacing `<digest>` with the digest from
`image-ref.txt`, then verify the image signature from the release workflow and
its build provenance from the reusable build workflow:

```sh
IMAGE="ghcr.io/dc-tec/bao-kms-provider@sha256:<digest>"

cosign verify \
  --new-bundle-format=true \
  --certificate-identity "${WORKFLOW_IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "${IMAGE}"

gh attestation verify "oci://${IMAGE}" \
  --repo "${REPO}" \
  --signer-workflow "${REPO}/.github/workflows/reusable-build.yml" \
  --source-ref "refs/tags/${VERSION}" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com \
  --deny-self-hosted-runners
```

Both commands exit with status `0`. Stop if either fails.

## Step 3: Preload the image

Pull the verified digest into containerd on every control-plane node, so the
provider can start during recovery without registry access:

```sh
sudo crictl pull "${IMAGE}"
```

For air-gapped nodes, export the image once and import it on each node with
`ctr -n k8s.io images import`. Keep the previous release's image on every node
for rollback.

## Step 4: Prepare the host

Create the socket group if it does not exist and record its numeric group ID
(GID). The distroless image has no host group names, so the pod and the
provider configuration both use this number:

```sh
getent group openbao-kms-socket >/dev/null || sudo groupadd --system openbao-kms-socket
SOCKET_GID=$(getent group openbao-kms-socket | cut -d: -f3)
echo "${SOCKET_GID}"
```

Create the directories with numeric ownership for the container user `65532`,
and a tmpfiles entry that recreates the socket directory under `/run` after
every reboot:

```sh
sudo sh -eu -c "
install -d -m 0750 -o root -g root /etc/openbao-kms
install -d -m 0755 -o root -g root /etc/openbao-kms/tls
install -d -m 0750 -o 65532 -g 65532 /var/lib/openbao-kms
install -d -m 0750 -o 65532 -g 65532 /var/lib/openbao-kms/state
install -d -m 0755 -o root -g root /etc/kubernetes/openbao-kms
printf 'd /run/openbao-kms 2750 65532 ${SOCKET_GID} -\n' > /etc/tmpfiles.d/openbao-kms-static-pod.conf
systemd-tmpfiles --create /etc/tmpfiles.d/openbao-kms-static-pod.conf
"
```

The state directory must stay owned by `65532` without group or world write
permission; the provider holds a lock file there across restarts.

## Step 5: Write the provider configuration

Copy the configuration sample from the bundle:

```sh
cp config/provider-static-pod.yaml provider.yaml
```

Edit `provider.yaml` and replace the sample values in the fields listed in
[Plan identity values: Provider configuration](/docs/get-started/plan-values/#provider-configuration).
Set `server.socketGroup` to the value of `SOCKET_GID`, as a quoted string.

## Step 6: Place the runtime files

From the directory that holds `provider.yaml`, `ca.crt`, and `identity.jwt`:

```sh
sudo install -m 0640 -o root -g 65532 provider.yaml /etc/openbao-kms/config.yaml
sudo install -m 0644 -o root -g root ca.crt /etc/openbao-kms/tls/ca.crt
sudo install -m 0640 -o root -g 65532 identity.jwt /var/lib/openbao-kms/identity.jwt
```

## Step 7: Validate the configuration

Resolve the configuration, check the Transit key profile, and run the full
bootstrap check against OpenBao:

```sh
sudo bao-kms-provider config --config /etc/openbao-kms/config.yaml
sudo bao-kms-provider verify-key --config /etc/openbao-kms/config.yaml
sudo bao-kms-provider doctor --config /etc/openbao-kms/config.yaml
```

Each command exits with status `0`, and neither `verify-key` nor `doctor`
reports a `[fail]` check. Every control-plane node must print the same identity
fingerprint. These checks run as root, so file permission problems for UID
`65532` show up only when the pod starts in the next step.

## Step 8: Start the static pod

Edit `static-pod/bao-kms-provider.yaml` from the bundle:

- set `image` to the verified `IMAGE` digest reference,
- replace the `supplementalGroups` entry `1234` with `SOCKET_GID`.

Then hand the manifest to kubelet and wait for readiness:

```sh
sudo install -m 0644 -o root -g root static-pod/bao-kms-provider.yaml \
  /etc/kubernetes/manifests/bao-kms-provider.yaml
curl -fsS --retry 60 --retry-delay 2 --retry-all-errors http://127.0.0.1:8082/ready
```

`/ready` returns HTTP 200 and `/run/openbao-kms/kms.sock` exists. If the pod
does not become ready, inspect it with `sudo crictl ps -a --name bao-kms-provider`
and `sudo crictl logs <container-id>`. The provider is ready for
[Enable encryption](/docs/get-started/enable-encryption/) once it runs on every
control-plane node.

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

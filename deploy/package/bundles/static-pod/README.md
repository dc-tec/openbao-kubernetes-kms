# Static-pod installation kit

This kit contains the matching Linux host binary, minimal values files,
manifest inputs, and the provider image digest. Choose the kit for the host
architecture. Verify the archive before extracting or running it. The OCI
image is a separate download; `image-ref.txt` pins its multi-platform digest.

Use this preview procedure only for fresh disposable evaluation clusters.
If an API server already has an encryption configuration, stop. These samples
do not import or migrate an existing encryption configuration.

## Prepare each host

Run from the extracted kit on a Linux host with GNU `install`, `getent`,
`groupadd`, `systemd-tmpfiles`, containerd, and kubelet. Install the binary and
create the socket group. Record the GID separately on each node:

<!-- static-pod-kit-install -->
```sh
set -eu
install -o root -g root -m 0755 bin/bao-kms-provider /usr/bin/bao-kms-provider
getent group openbao-kms-socket >/dev/null || groupadd --system openbao-kms-socket
SOCKET_GID=$(getent group openbao-kms-socket | cut -d: -f3)
install -d -m 0750 -o root -g 65532 /etc/openbao-kms
install -d -m 0755 -o root -g root /etc/openbao-kms/tls
install -d -m 0750 -o root -g 65532 /etc/openbao-kms/credentials
install -d -m 0750 -o 65532 -g 65532 /var/lib/openbao-kms
install -d -m 0750 -o 65532 -g 65532 /var/lib/openbao-kms/state
install -d -m 0750 -o root -g 65532 /var/lib/openbao-kms/credentials
install -d -m 0755 -o root -g root /etc/kubernetes/openbao-kms
printf 'd /run/openbao-kms 2750 65532 %s -\n' "$SOCKET_GID" > /etc/tmpfiles.d/openbao-kms-static-pod.conf
systemd-tmpfiles --create /etc/tmpfiles.d/openbao-kms-static-pod.conf
```

This block runs as root. It does not install runtime configuration or activate
the provider. Reuse the socket group; do not add it to credential permissions.

## Generate and review files

Copy `config/init-values-file.yaml` or `config/init-values-oauth2.yaml` to a
private working directory as `values.yaml`. Replace the example addresses and
identities. Provision credentials independently of the protected API server.
Keep secrets out of the values file.

For a new Transit key, generate once with `--new-key`:

```sh
bao-kms-provider init --values values.yaml --out generated --new-key \
  --model static-pod --socket-gid "$SOCKET_GID" --image "$(cat image-ref.txt)"
```

For another node, use `generated/config.yaml` as the input, select a new output
directory, and omit `--new-key`. Set that node's socket GID. Compare the shared
fingerprint and lineage in `installation.json`; they must match across nodes.

Review `openbao-policy.hcl` and `openbao-setup.sh`, then apply them through the
OpenBao administrator. Place the generated configuration at
`/etc/openbao-kms/config.yaml` with owner `root:65532` and mode `0640`. Install
the configured CA files and credential using their recorded paths. Credentials
must be readable by UID/GID `65532` and inaccessible to other users. The host
JWT agent must renew with atomic file replacement; OAuth client secrets must
be replaced atomically when rotated.

## Stage and activate

Verify the image signature and provenance using the selected release's
verification instructions, then preload the digest from `image-ref.txt`:

```sh
crictl pull "$(cat image-ref.txt)"
```

Validate configuration and credential access under UID/GID `65532` with the
supplemental socket GID. `doctor` performs local and OpenBao checks; it does
not prove that the running provider or Kubernetes API server works.

Install `generated/bao-kms-provider.yaml` under `/etc/kubernetes/manifests/`
only after reviewing it. Wait for HTTP 200 from `http://127.0.0.1:8082/ready`
on every node. Stage `encryption-config-readers.yaml` on every API server
before promoting any server to `encryption-config.yaml`. Retain `identity`.
Verify readiness and Secret reads/writes through every API server directly.

Use the documentation for the selected release for the complete activation
and recovery procedure. No command in this kit declares migration complete.

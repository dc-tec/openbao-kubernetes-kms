# Static-pod installation kit

This kit contains the matching Linux host binary, minimal values files,
manifest inputs, and the provider image digest. Choose the kit for the host
architecture. Verify the archive before extracting or running it. The OCI
image is a separate download; `image-ref.txt` pins its multi-platform digest.

Use this preview procedure only for fresh disposable evaluation clusters.
If an API server already has an encryption configuration, stop. These samples
do not import or migrate an existing encryption configuration.

## Prepare each host

Run from the extracted kit on a Linux host with GNU `install`, `getent`, and
`groupadd`. Install the binary and create the socket group, then record the GID
separately on each node:

<!-- static-pod-kit-install -->
```sh
set -eu
install -o root -g root -m 0755 bin/bao-kms-provider /usr/bin/bao-kms-provider
getent group openbao-kms-socket >/dev/null || groupadd --system openbao-kms-socket
SOCKET_GID=$(getent group openbao-kms-socket | cut -d: -f3)
```

This block runs as root. It does not install runtime configuration or activate
the provider. The generated `node-setup.sh` creates the provider directories.

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
directory, and omit `--new-key`. Set that node's socket GID. The shared
fingerprint and lineage in `installation.json` must match across nodes.

Review `openbao-policy.hcl` and `openbao-setup.sh`, then apply them through the
OpenBao administrator.

## Stage and activate

Verify the image signature and provenance using the selected release's
verification instructions, then preload the digest from `image-ref.txt`:

```sh
crictl pull "$(cat image-ref.txt)"
```

Review `generated/node-setup.sh`, then run its phases as root in order. Pass the
CA bundle and the JWT or OAuth client secret to `install`:

```sh
sh generated/node-setup.sh prepare
sh generated/node-setup.sh install --ca ca.crt --credential identity.jwt
sh generated/node-setup.sh check
sh generated/node-setup.sh start
```

`start` installs the manifest and waits for `/ready`. After the local API server
uses the socket, run `sh bin/probe-apiserver` as root from this kit. Status,
Encrypt, and Decrypt must pass. Stage `encryption-config-readers.yaml` on every
API server before promoting any server to `encryption-config.yaml`. Retain
`identity`. Verify readiness and Secret reads/writes through every API server
directly.

Use the documentation for the selected release for the complete activation
and recovery procedure. No command in this kit declares migration complete.

# Unreleased

## Security and Go toolchain

Builds use Go 1.27.3. The module, development environment, CI, and container
builders use the same pinned version. Source builds require Go 1.27.3 or later.
Staticcheck 0.8.2 and golangci-lint 2.15.0 support the updated toolchain.

Go 1.27.3 rejects certificate chains containing any of the eight compromised
eMudhra intermediate CA public keys. OpenBao connections trust only the
configured CA bundle. OAuth token requests use system roots when
`auth.jwt.oauth2.caCertFile` is omitted; setting that field replaces system
roots with the configured bundle. Connections that trust an affected chain
fail verification after this update. Replace affected certificates rather
than disabling TLS verification.

The update also includes the Go 1.27.2 security fixes and updates
`golang.org/x/net` to v0.60.0 for the October HTTP/2 security fixes. Rebuild
provider binaries and images to include these fixes. This does not update the
separate OpenBao or Kubernetes API server binaries.

See the [Go security fix](https://github.com/golang/go/issues/82345),
[Go release history](https://go.dev/doc/devel/release#go1.27.3), and
[HTTP/2 advisory](https://pkg.go.dev/vuln/GO-2026-6617).

## File JWT compatibility

Existing file JWT configurations can omit `auth.jwt.source` when `jwtFile`
is set and no `oauth2` section is present. These configurations continue to
use file authentication. Empty source values and mixed file/OAuth settings
fail validation. OAuth authentication never falls back to a JWT file.

## Preview.3 installation boundary

`0.1.0-preview.3` requires fresh disposable installations with new Transit keys
and provider identities. It does not support in-place upgrades from earlier
previews. Registry schema `v1alpha2` binds the configuration identity
fingerprint, including the Transit key name and mount path. Older unbound state
is rejected without rewriting state or checkpoint; do not delete either file
to bypass the check. KMS key IDs, annotations, and AAD bytes are unchanged.

Checkpoint validation now accepts only the same state or its immediate
hash-linked successor. Copy the registry and checkpoint together when restoring
from backup or a healthy peer. A divergent higher-generation registry cannot
replace a surviving checkpoint.

For static-pod setup, `init --socket-gid` now selects the current node's socket
group when reusing another node's generated values. The shared identity and
key lineage remain unchanged. Generated-kit acceptance covers three API
servers with different socket GIDs, runtime diagnostics, staged activation,
and cold Secret reads.

## Operator-Controlled Key Retirement

`retire-versions` plans removal of obsolete historical versions from local
decrypt lookup. Applying a reviewed plan requires `--apply`, its exact
`--expected-state-hash`, and a stopped provider. Operators must verify migration
and backup evidence and apply the transition on every node before raising
OpenBao minimum versions.

Normal rotation now rejects loss of accepted active or historical key identities.
Removed identities remain in hashed state and cannot reappear during later
rotation. Older binaries that do not recognize `removed` records reject this
state. Every node must understand retirement records before retirement begins;
do not downgrade to a binary that rejects them.

`serve` and retirement share a state writer lock acquired before bootstrap.
The state directory must be owned by the provider's OS user. Key ID derivation,
annotations, AAD, configuration fields, and metrics do not change.

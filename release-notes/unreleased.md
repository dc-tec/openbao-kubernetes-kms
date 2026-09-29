# Unreleased

## Go toolchain

Builds use Go 1.27.1. The module, development environment, CI, and container
builders use the same pinned version. Source builds require Go 1.27.1 or later.
Staticcheck 0.8.1 and golangci-lint 2.13.2 support the updated toolchain.

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

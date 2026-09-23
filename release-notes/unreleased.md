# Unreleased

## Operator-Controlled Key Retirement

`retire-versions` plans removal of obsolete historical versions from local
decrypt lookup. Applying a reviewed plan requires `--apply`, its exact
`--expected-state-hash`, and a stopped provider. Operators must verify migration
and backup evidence and apply the transition on every node before raising
OpenBao minimum versions.

Normal rotation now rejects loss of accepted active or historical key identities.
Removed identities remain in hashed state and cannot reappear during later
rotation. Older binaries that do not recognize `removed` records reject this
state; upgrade every provider before retirement and do not downgrade afterward
without a reviewed recovery procedure.

`serve` and retirement share a state writer lock acquired before bootstrap.
The state directory must be owned by the provider's OS user. Key ID derivation,
annotations, AAD, configuration fields, and metrics do not change.

# 0008: AAD Required For MVP

## Status

Accepted for design.

## Context

There is no released pre-AAD ciphertext format for this project. Supporting optional AAD reads from day one would add complexity without preserving any real deployed compatibility.

OpenBao Transit supports associated data for compatible AEAD key types.

## Decision

AAD is enabled and required for v0.1 MVP deployments.

Compatibility mode remains a documented future capability, but it is not part of the default v0.1 path.

## Consequences

- Decrypt rejects missing or malformed AAD annotations.
- Key ID/AAD golden fixtures are blocking.
- Future compatibility mode must be explicit, bounded, observable, and tested.
- New deployments get metadata binding from the first release.


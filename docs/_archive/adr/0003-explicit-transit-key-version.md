# 0003: Explicit Transit Key Version

## Status

Accepted for design.

## Context

OpenBao Transit can encrypt with the latest key version implicitly, but Kubernetes KMS v2 relies on stable `Status.key_id` and matching encrypt responses.

Implicit latest-version encryption can race with metadata observation and cause the plugin to encrypt with a version that does not match the active Kubernetes key ID.

## Decision

Every Transit encrypt call must include the explicit `key_version` from the active key snapshot.

## Consequences

- The plugin needs a reliable active key snapshot.
- Rotation must promote versions only after stable observation.
- Tests must assert the exact version sent to Transit.
- Encrypt fails closed if no active version is available.


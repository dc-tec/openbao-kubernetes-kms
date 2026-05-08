# 0002: JWT-First Authentication

## Status

Accepted for design.

## Context

The plugin needs to authenticate to OpenBao during API server startup and recovery. If authentication depends on the protected Kubernetes API server, recovery can become circular.

OpenBao supports JWT auth roles with bound subjects, audiences, and claims.

## Decision

The default authentication mode is OpenBao JWT auth from a host-mounted JWT file.

The plugin re-reads the JWT file before login or re-login and stores OpenBao tokens only in memory.

## Consequences

- Operators must provision a JWT file on every control-plane node.
- JWT issuer design is part of bootstrap and disaster recovery.
- ServiceAccount tokens from the protected cluster are not recommended as the only recovery identity.
- Certificate auth may be added later as a non-default alternative.


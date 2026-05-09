# Workstreams

This directory tracks the implementation backlog for the OpenBao Kubernetes KMS provider.

Canonical backlog:

- [Implementation backlog](implementation-backlog.md)

Supporting planning docs:

- [MVP board](mvp-board.md)

## Backlog Rules

- Task IDs are stable and should be reused in issues, branches, commits, and PR descriptions.
- A task is not complete unless its acceptance criteria and tests are complete.
- Changes to key ID, annotation, AAD, or KMS protocol behavior must update the relevant contract doc and ADR.
- Production-readiness work is not optional; it is gated separately from the v0.1 engineering preview.

## Workstream Summary

| Workstream | Area |
|---|---|
| WS00 | Repository and project foundation |
| WS01 | Configuration and validation |
| WS02 | Key registry, key ID, annotations, and AAD |
| WS03 | OpenBao Transit client |
| WS04 | JWT authentication and token lifecycle |
| WS05 | KMS v2 protocol server |
| WS06 | Status cache, health, and rotation watcher |
| WS07 | Socket and runtime service behavior |
| WS08 | CLI tooling |
| WS09 | Observability and redaction |
| WS10 | Deployment artifacts and packaging |
| WS11 | Test infrastructure and validation |
| WS12 | Security hardening and supply chain |
| WS13 | Documentation, examples, and release process |

## Version Policy

Workstreams must use exact-pinned validation versions. Initial v0.1 targets are OpenBao `2.5.3` and the Kubernetes `1.34` release line. The Kind lane currently pins `kindest/node:v1.34.3` by digest in `.ci/versions.yaml` while tracking upstream `1.34.7` as the latest patch.

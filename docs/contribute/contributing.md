---
title: Contributing
description: "Set up the pinned development environment, run the required checks, and keep code, contracts, and docs in step."
eyebrow: Contribute
weight: 10
verifiedBy:
  - devenv.nix
  - mk/checks.mk
  - mk/deployment.mk
---

The module is `github.com/dc-tec/openbao-kubernetes-kms`, the binary is
`bao-kms-provider`, and the Go toolchain is pinned in `.go-version` and
`.ci/versions.yaml`. The repository's `CONTRIBUTING.md` covers commit format
and the Developer Certificate of Origin.

## Set up

Install Nix and devenv 2.1 or later, then from the repository root check the
toolchain and install the repository-managed tools:

```sh
devenv test
devenv tasks run kms:bootstrap
```

The shell sets `GOTOOLCHAIN=local` and starts no services or credentials. Use
`devenv shell` for an interactive shell, or `devenv --profile editor shell` to
add the Go language server and debugger.

## Check your change

Every pull request must pass the core gate, which runs `make ci-core`:

```sh
devenv tasks run kms:ci-core
```

Depending on what you change, also run:

| Change | Command |
|---|---|
| OpenBao client code | `go test -tags=integration ./internal/openbao -run TestOpenBaoTransitIntegration -count=1` (hermetic HTTPS fakes, no credentials) |
| OpenBao, Kubernetes, deployment, rotation, or packaging behavior | The matching lane from [E2E framework](/contribute/e2e-framework/), starting with `make test-e2e-openbao-ci` |
| Deployment samples or package metadata | `make deployment-samples-check` and `make package-build-check` |
| Install guides or bundles | `make systemd-install-check` |
| Documentation | `make docs-check` and `make docs-build` |

`deployment-samples-check` verifies the systemd unit with `systemd-analyze`
when it is installed. `package-build-check` builds throwaway `.deb` and `.rpm`
packages with the pinned nFPM.

Code follows [Code quality](/contribute/code-quality/), and tests follow
[Testing](/contribute/testing/).

## Keep contracts and docs in step

A change to the provider name handling, `key_id` derivation, annotations, AAD
canonicalization, or historical key lookup is a wire-format break; follow
[Reference: Compatibility](/docs/reference/compatibility/#breaking-changes).

Update the docs in the same change as the behavior:

| Change | Page |
|---|---|
| Configuration | [Reference: Configuration](/docs/reference/configuration/) |
| KMS protocol behavior | [Reference: KMS v2 contract](/docs/reference/kms-v2-contract/) |
| `key_id` or AAD | [Reference: Key ID and AAD](/docs/reference/key-id-and-aad/) |
| Metrics, logs, or error classes | [Reference: Observability](/docs/reference/observability/) |
| Tested versions | [Reference: Compatibility](/docs/reference/compatibility/) |
| Operations or deployment | The matching Get started or Operate page |

See [Docs style guide](/contribute/docs-style-guide/) for how to write them.

## Dependencies

Prefer the official Kubernetes KMS protobuf package, official or
OpenBao-compatible API clients, standard library parsers for structured data,
and small, well-maintained dependencies. Avoid ad hoc parsing of YAML, JSON, or
JWTs, dependencies that log requests by default, and dependencies that make TLS
verification hard to control.

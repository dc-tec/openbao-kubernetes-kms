# E2E Tests

This directory contains the Ginkgo/Gomega E2E suite.

The default OpenBao E2E target uses an ephemeral OpenBao CI environment:

```sh
make test-e2e-openbao-ci
```

The suite manifest is `suites.yaml`. It defines active and planned lanes, while concrete OpenBao and Kubernetes versions remain centralized in `.ci/versions.yaml`.

Planned lanes:

- Kind KMS v2 smoke coverage,
- v0.1 release-gate coverage.

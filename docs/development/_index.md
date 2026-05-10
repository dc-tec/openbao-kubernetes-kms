---
title: "Development"
description: "Contributor-facing workflow: contributing, code quality, testing, CI, release process, docs style, and the docs site itself."
weight: 70
browse:
  - "/development/contributing"
  - "/development/code-quality"
  - "/development/testing"
  - "/development/e2e-framework"
  - "/development/harvester-kubeadm-lab"
  - "/development/ci-supply-chain"
  - "/development/release-gates"
  - "/development/docs-style-guide"
  - "/development/docs-site"
---

# Development

These pages cover local development, CI, release process, and how the docs site is built. Use them as a contributor or reviewer rather than as an operator.

## Topics

- [Contributing](/development/contributing/) for local environment setup, Go version, and the contribution workflow.
- [Code Quality](/development/code-quality/) for the strict typed Go conventions, ast-grep rules, and Semgrep boundaries.
- [Testing](/development/testing/) for the unit, integration, and end-to-end testing strategy.
- [E2E Framework](/development/e2e-framework/) for the Ginkgo and Gomega suite layout, manifest routing, and reports.
- [Harvester Kubeadm Lab](/development/harvester-kubeadm-lab/) for local-only VM release-gate setup.
- [CI And Supply Chain](/development/ci-supply-chain/) for the CI pipeline, version pinning, and supply-chain controls.
- [Release Gates](/development/release-gates/) for the release process and the gates a release must pass.
- [Docs Style Guide](/development/docs-style-guide/) for the writing rules, IA contracts, and verification commands enforced on the docs.
- [Docs Site](/development/docs-site/) for how this Hugo site is structured, built, and published.

## Use Another Section If

- the question is about installing, wiring, or operating the provider: go to [Start Here](/getting-started/) or [Operations](/operations/).
- the question is about exact CLI, configuration, or contract behavior: go to [Reference](/reference/).
- the question is about the design rationale behind a feature: go to [Architecture](/architecture/).

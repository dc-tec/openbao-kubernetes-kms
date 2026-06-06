# Contributing

`openbao-kubernetes-kms` is control-plane security software. Contributions should preserve explicit contracts, pinned release inputs, and testable operational behavior.

## Ground Rules

- Keep implementation changes aligned with the public contract docs.
- Update documentation in the same change when behavior, config, metrics, deployment, or compatibility changes.
- Do not introduce floating `latest` CI inputs or support claims.
- Do not log plaintext, JWTs, OpenBao tokens, full ciphertext, or raw Transit key material.
- Do not use `map[string]any`, `map[string]interface{}`, or broad `any` in production code.
- Keep dependency, generated, vendored, and release-artifact changes intentional and reviewable.
- Prefer small changes with focused tests over broad refactors.

## Commit And PR Format

- Use Conventional Commits with a useful scope, for example `fix(auth): ...` or `docs(security): ...`.
- Sign commits with the Developer Certificate of Origin using `git commit -s`.
- Explain compatibility, deployment, metric, and security impact in the pull request when those surfaces change.

## Project Choices

- Project/repository: `openbao-kubernetes-kms`
- Go module: `github.com/dc-tec/openbao-kubernetes-kms`
- Binary: `bao-kms-provider`
- Go toolchain: `1.26.4`
- CLI/config framework: Viper
- Local task runner: Makefile
- Version policy: `.ci/versions.yaml`

## Local Checks

Run the core local checks before opening a pull request:

```sh
make ci-core
```

For documentation-only changes, also run:

```sh
make docs-check
make docs-build
```

For focused end-to-end or deployment validation, use the targets documented in [Testing](docs/development/testing.md) and [E2E Framework](docs/development/e2e-framework.md).

See [Code Quality](docs/development/code-quality.md) and [Contributing](docs/development/contributing.md) for the full contributor rules.

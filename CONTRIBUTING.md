# Contributing

This project is in the documentation and M0 foundation stage.

## Ground Rules

- Use workstream IDs from [docs/workstreams/implementation-backlog.md](docs/workstreams/implementation-backlog.md) in issues and pull requests when possible.
- Keep implementation changes aligned with the contract docs.
- Update documentation in the same change when behavior, config, metrics, deployment, or compatibility changes.
- Do not introduce floating `latest` CI inputs or support claims.
- Do not log plaintext, JWTs, OpenBao tokens, full ciphertext, or raw Transit key material.
- Do not use `map[string]any`, `map[string]interface{}`, or broad `any` in production code.

## Foundation Choices

- Project/repository: `openbao-kubernetes-kms`
- Go module: `github.com/dc-tec/openbao-kubernetes-kms`
- Binary: `bao-kms-provider`
- Go toolchain: `1.26.3`
- CLI/config framework: Viper
- Local task runner: Makefile
- Version policy: `.ci/versions.yaml`

## Local Checks

During M0, use:

```sh
make ci-core
```

Once Go implementation exists, `make ci-core` must include formatting, vetting, unit tests, and the fast contract checks described in the backlog.

See [Code quality](docs/code-quality.md) for the typed-Go rules.

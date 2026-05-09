# Test Layout

This directory holds non-unit test harnesses once the implementation reaches the relevant workstreams.

| Directory | Purpose |
|---|---|
| `e2e` | Ginkgo/Gomega end-to-end suite, manifest, ephemeral OpenBao CI lane, provider full-stack OpenBao/KMS v2 socket coverage, and future Kind/control-plane scenarios. |
| `fakes` | Reusable test doubles for fake Transit, fake Status cache, and fake OpenBao auth/token lifecycle tests. |
| `integration` | OpenBao-backed integration tests. |
| `kmsconformance` | Kubernetes KMS v2 protocol conformance tests. |
| `testdata` | Golden fixtures and shared test inputs. |

# Test Layout

This directory holds non-unit test harnesses and shared fixtures.

| Directory | Purpose |
|---|---|
| `e2e` | Ginkgo/Gomega end-to-end suite, suite manifest, OpenBao CI lanes, full-stack provider and OpenBao KMS v2 socket coverage, and Kind control-plane scenarios. |
| `fakes` | Reusable test doubles for fake Transit, fake Status cache, and fake OpenBao auth/token lifecycle tests. |
| `integration` | OpenBao-backed integration tests. |
| `kmsconformance` | Kubernetes KMS v2 protocol conformance tests. |
| `testdata` | Golden fixtures and shared test inputs. |

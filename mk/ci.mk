##@ CI

.PHONY: ci
ci: ci-core ## Run the standard local CI gate.

.PHONY: ci-core
ci-core: verify-tidy verify-vendor lint lint-tagged security-ci test test-race test-certauth fuzz build release-artifacts ## Run the local core quality gate.

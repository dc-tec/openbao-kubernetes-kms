##@ Development

.PHONY: fmt
fmt: ## Format Go sources.
	@if find cmd internal test hack/tools -name '*.go' 2>/dev/null | grep -q .; then \
		gofmt -w $$(find cmd internal test hack/tools -name '*.go'); \
		if command -v "$(GOFUMPT)" >/dev/null 2>&1; then "$(GOFUMPT)" -w cmd internal test hack/tools; fi; \
	else \
		printf '%s\n' 'No Go files yet; skipping Go formatting.'; \
	fi

.PHONY: verify-fmt
verify-fmt: ## Verify Go formatting without modifying files.
	@if find cmd internal test hack/tools -name '*.go' 2>/dev/null | grep -q .; then \
		unformatted="$$(gofmt -l $$(find cmd internal test hack/tools -name '*.go'))"; \
		if [ -n "$$unformatted" ]; then printf '%s\n' "$$unformatted"; exit 1; fi; \
		if command -v "$(GOFUMPT)" >/dev/null 2>&1; then \
			unformatted="$$("$(GOFUMPT)" -l cmd internal test hack/tools)"; \
			if [ -n "$$unformatted" ]; then printf '%s\n' "$$unformatted"; exit 1; fi; \
		else \
			printf '%s\n' 'gofumpt not installed; skipping gofumpt verification.'; \
		fi; \
	else \
		printf '%s\n' 'No Go files yet; skipping Go formatting verification.'; \
	fi

.PHONY: lint
lint: docs-check versions-check verify-e2e-manifest verify-fmt test-ast lint-ast semgrep-ci ## Run lightweight lint checks.
	@"$(GO)" vet ./...
	@if command -v "$(STATICCHECK)" >/dev/null 2>&1; then "$(STATICCHECK)" ./...; else printf '%s\n' 'staticcheck not installed; skipping staticcheck.'; fi
	@if command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1; then "$(GOLANGCI_LINT)" run; else printf '%s\n' 'golangci-lint not installed; skipping golangci-lint.'; fi

.PHONY: verify-devenv
verify-devenv: ## Verify the pinned devenv toolchain contract.
	@bash hack/dev/verify-devenv.sh

.PHONY: lint-ci
lint-ci: lint vulncheck ## Run lint plus vulnerability checks.

.PHONY: test
test: ## Run Go tests.
	@"$(GO)" test ./...

.PHONY: test-race
test-race: ## Run race-enabled Go tests.
	@"$(GO)" test -race ./...

.PHONY: tidy
tidy: ## Run go mod tidy.
	@GOFLAGS="-mod=mod" "$(GO)" mod tidy

.PHONY: vendor
vendor: ## Refresh vendor/.
	@GOFLAGS="-mod=mod" "$(GO)" mod vendor

.PHONY: verify-tidy
verify-tidy: ## Verify go.mod and go.sum are tidy.
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp go.mod "$$tmp/go.mod"; \
	cp go.sum "$$tmp/go.sum"; \
	GOFLAGS="-mod=mod" "$(GO)" mod tidy; \
	cmp -s go.mod "$$tmp/go.mod" && cmp -s go.sum "$$tmp/go.sum"

.PHONY: verify-vendor
verify-vendor: ## Verify vendor/ is synchronized with go.mod and go.sum.
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp go.mod "$$tmp/go.mod"; \
	cp go.sum "$$tmp/go.sum"; \
	if [ -d vendor ]; then cp -R vendor "$$tmp/vendor"; else mkdir "$$tmp/vendor"; fi; \
	GOFLAGS="-mod=mod" "$(GO)" mod vendor; \
	cmp -s go.mod "$$tmp/go.mod" && cmp -s go.sum "$$tmp/go.sum" && diff -qr "$$tmp/vendor" vendor >/dev/null

.PHONY: fuzz
fuzz: ## Run curated fuzz smoke targets.
	@"$(GO)" test ./internal/aad -run '^$$' -fuzz '^FuzzParseAnnotations$$' -fuzztime="$(FUZZTIME)"
	@"$(GO)" test ./internal/aad -run '^$$' -fuzz '^FuzzPrepareDecrypt$$' -fuzztime="$(FUZZTIME)"
	@"$(GO)" test ./internal/keyregistry -run '^$$' -fuzz '^FuzzParseKeyID$$' -fuzztime="$(FUZZTIME)"
	@"$(GO)" test ./internal/keyregistry -run '^$$' -fuzz '^FuzzStateFileDecode$$' -fuzztime="$(FUZZTIME)"
	@"$(GO)" test ./internal/keyregistry -run '^$$' -fuzz '^FuzzStateCheckpointDecode$$' -fuzztime="$(FUZZTIME)"

.PHONY: versions-check
versions-check: ## Check central version policy exists and contains no floating latest.
	@test -f .ci/versions.yaml
	@! grep -R -n 'latest' .ci/versions.yaml

.PHONY: verify-e2e-manifest
verify-e2e-manifest: ## Validate the E2E suite manifest and version-pin policy.
	@"$(GO)" test ./test/e2e -run '^Test(E2EManifest|KubernetesPreviewMatrixPolicy|OpenBaoVersionPolicy|ReleaseWorkflowUsesManifestGate|ReleaseGateDefersUnsupportedSPIRELane|CIWorkflowReusesPrebuiltE2EProviderImages|ReleaseWorkflowRunsE2EAgainstBuiltImage|ReleaseGateMakeTargetsExist)$$' -count=1

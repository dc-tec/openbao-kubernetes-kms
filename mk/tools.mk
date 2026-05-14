##@ Tooling

.PHONY: bootstrap
bootstrap: ## Prepare local development prerequisites.
	@printf 'Go toolchain: %s\n' '$(GO_VERSION)'
	@if command -v npm >/dev/null 2>&1; then \
		npm ci --prefix .github/tools; \
	else \
		printf '%s\n' 'npm not found; skipping ast-grep tool install.'; \
	fi
	@$(MAKE) install-go-tools

.PHONY: install-go-tools
install-go-tools: ## Install pinned optional Go quality and E2E tools into bin/.
	@mkdir -p "$(GOBIN)"
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install github.com/google/go-licenses/v2@$(GO_LICENSES_VERSION)

.PHONY: install-release-tools
install-release-tools: ## Install pinned release packaging tools into bin/.
	@mkdir -p "$(GOBIN)"
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION)

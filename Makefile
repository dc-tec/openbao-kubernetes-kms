SHELL := /bin/sh

.PHONY: help bootstrap build fmt verify-fmt lint lint-ci test test-race tidy verify-tidy ci-core docs-check versions-check lint-ast test-ast semgrep-rules-test semgrep-scan semgrep-ci install-go-tools vulncheck

GO_VERSION := $(shell cat .go-version)
GO ?= go
GOBIN ?= $(CURDIR)/bin
AST_GREP ?= .github/tools/node_modules/.bin/ast-grep
SEMGREP ?= semgrep
GOFUMPT ?= gofumpt
STATICCHECK ?= staticcheck
GOVULNCHECK ?= govulncheck
GOLANGCI_LINT ?= golangci-lint
SEMGREP_CONFIG_FLAGS ?= --config .semgrep/rules
SEMGREP_TARGETS ?= cmd internal
SEMGREP_ARTIFACT_DIR ?= dist/semgrep
SEMGREP_OUTPUT_JSON ?= $(SEMGREP_ARTIFACT_DIR)/semgrep.json
BIN ?= bin/bao-kms-provider
VERSION ?= 0.0.0-dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf '%s' unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIRTY ?= $(shell if [ -n "$$(git status --porcelain 2>/dev/null)" ]; then printf '%s' true; else printf '%s' false; fi)
VERSION_PKG := github.com/dc-tec/openbao-kubernetes-kms/internal/version
LDFLAGS := -s -w -X $(VERSION_PKG).version=$(VERSION) -X $(VERSION_PKG).commit=$(COMMIT) -X $(VERSION_PKG).buildDate=$(BUILD_DATE) -X $(VERSION_PKG).dirty=$(DIRTY)
GOFUMPT_VERSION ?= v0.9.2
STATICCHECK_VERSION ?= v0.7.0
GOVULNCHECK_VERSION ?= v1.2.0
GOLANGCI_LINT_VERSION ?= v2.11.4

help:
	@printf '%s\n' 'Targets:'
	@printf '%s\n' '  bootstrap       Prepare local development prerequisites once implementation exists'
	@printf '%s\n' '  build           Build bao-kms-provider with version metadata'
	@printf '%s\n' '  fmt             Format Go sources when go.mod exists'
	@printf '%s\n' '  lint            Run lightweight lint checks'
	@printf '%s\n' '  lint-ast        Run ast-grep rules when ast-grep and Go code are present'
	@printf '%s\n' '  test            Run Go tests when go.mod exists'
	@printf '%s\n' '  test-race       Run race-enabled Go tests'
	@printf '%s\n' '  ci-core         Run the local core quality gate'
	@printf '%s\n' '  docs-check      Check docs for known formatting artifacts'
	@printf '%s\n' '  install-go-tools Install pinned optional Go quality tools into bin/'
	@printf '%s\n' '  semgrep-ci      Run Semgrep rule tests and blocking scan when semgrep is available'
	@printf '%s\n' '  versions-check  Check central version policy exists'

bootstrap:
	@printf 'Go toolchain: %s\n' '$(GO_VERSION)'
	@if command -v npm >/dev/null 2>&1; then \
		npm ci --prefix .github/tools; \
	else \
		printf '%s\n' 'npm not found; skipping ast-grep tool install.'; \
	fi
	@$(MAKE) install-go-tools

install-go-tools:
	@mkdir -p "$(GOBIN)"
	@GOBIN="$(GOBIN)" "$(GO)" install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	@GOBIN="$(GOBIN)" "$(GO)" install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	@GOBIN="$(GOBIN)" "$(GO)" install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@GOBIN="$(GOBIN)" "$(GO)" install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

build:
	@mkdir -p "$$(dirname "$(BIN)")"
	@"$(GO)" build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN)" ./cmd/bao-kms-provider

fmt:
	@if find cmd internal -name '*.go' 2>/dev/null | grep -q .; then \
		gofmt -w $$(find cmd internal -name '*.go'); \
		if command -v "$(GOFUMPT)" >/dev/null 2>&1; then "$(GOFUMPT)" -w cmd internal; fi; \
	else \
		printf '%s\n' 'No Go files yet; skipping Go formatting.'; \
	fi

verify-fmt:
	@if find cmd internal -name '*.go' 2>/dev/null | grep -q .; then \
		unformatted="$$(gofmt -l $$(find cmd internal -name '*.go'))"; \
		if [ -n "$$unformatted" ]; then printf '%s\n' "$$unformatted"; exit 1; fi; \
		if command -v "$(GOFUMPT)" >/dev/null 2>&1; then \
			unformatted="$$("$(GOFUMPT)" -l cmd internal)"; \
			if [ -n "$$unformatted" ]; then printf '%s\n' "$$unformatted"; exit 1; fi; \
		else \
			printf '%s\n' 'gofumpt not installed; skipping gofumpt verification.'; \
		fi; \
	else \
		printf '%s\n' 'No Go files yet; skipping Go formatting verification.'; \
	fi

lint: docs-check versions-check verify-fmt test-ast lint-ast semgrep-ci
	@"$(GO)" vet ./...
	@if command -v "$(STATICCHECK)" >/dev/null 2>&1; then "$(STATICCHECK)" ./...; else printf '%s\n' 'staticcheck not installed; skipping staticcheck.'; fi
	@if command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1; then "$(GOLANGCI_LINT)" run; else printf '%s\n' 'golangci-lint not installed; skipping golangci-lint.'; fi

lint-ci: lint vulncheck

test:
	@"$(GO)" test ./...

test-race:
	@"$(GO)" test -race ./...

tidy:
	@"$(GO)" mod tidy

verify-tidy:
	@"$(GO)" mod tidy
	@git diff --exit-code -- go.mod go.sum

vulncheck:
	@if command -v "$(GOVULNCHECK)" >/dev/null 2>&1; then "$(GOVULNCHECK)" ./...; else printf '%s\n' 'govulncheck not installed; skipping govulncheck.'; fi

ci-core: verify-tidy lint vulncheck test test-race build

docs-check:
	@! grep -R -n $$(printf '\357\277\274') README.md docs
	@! grep -R -n '⸻' README.md docs
	@! grep -R -n 'openbao-kms-provider' README.md docs

test-ast:
	@if command -v "$(AST_GREP)" >/dev/null 2>&1; then \
		"$(AST_GREP)" test -c .ast-grep/sgconfig.yml; \
	else \
		printf '%s\n' 'ast-grep not installed; skipping ast-grep rule tests.'; \
	fi

lint-ast:
	@if ! find cmd internal -name '*.go' 2>/dev/null | grep -q .; then \
		printf '%s\n' 'No Go files yet; skipping ast-grep scan.'; \
	elif command -v "$(AST_GREP)" >/dev/null 2>&1; then \
		"$(AST_GREP)" scan -c .ast-grep/sgconfig.yml --report-style=medium --error .; \
	else \
		printf '%s\n' 'ast-grep not installed; cannot scan Go code.'; \
		exit 1; \
	fi

semgrep-rules-test:
	@if command -v "$(SEMGREP)" >/dev/null 2>&1; then \
		"$(SEMGREP)" scan --test --config .semgrep/rules .semgrep/tests; \
	else \
		printf '%s\n' 'semgrep not installed; skipping Semgrep rule tests.'; \
	fi

semgrep-scan:
	@targets=""; for d in $(SEMGREP_TARGETS); do [ -e "$$d" ] && targets="$$targets $$d"; done; \
	if [ -z "$$targets" ] || ! find $$targets -name '*.go' 2>/dev/null | grep -q .; then \
		printf '%s\n' 'No Semgrep targets yet; skipping Semgrep scan.'; \
	elif command -v "$(SEMGREP)" >/dev/null 2>&1; then \
		"$(SEMGREP)" scan --metrics=off --no-git-ignore $(SEMGREP_CONFIG_FLAGS) $$targets; \
	else \
		printf '%s\n' 'semgrep not installed; cannot scan targets.'; \
		exit 1; \
	fi

semgrep-ci: semgrep-rules-test
	@targets=""; for d in $(SEMGREP_TARGETS); do [ -e "$$d" ] && targets="$$targets $$d"; done; \
	if [ -z "$$targets" ] || ! find $$targets -name '*.go' 2>/dev/null | grep -q .; then \
		printf '%s\n' 'No Semgrep targets yet; skipping Semgrep CI scan.'; \
	elif command -v "$(SEMGREP)" >/dev/null 2>&1; then \
		mkdir -p "$(SEMGREP_ARTIFACT_DIR)"; \
		"$(SEMGREP)" scan --metrics=off --no-git-ignore --error --json --output "$(SEMGREP_OUTPUT_JSON)" $(SEMGREP_CONFIG_FLAGS) $$targets; \
	else \
		printf '%s\n' 'semgrep not installed; cannot scan targets.'; \
		exit 1; \
	fi

versions-check:
	@test -f .ci/versions.yaml
	@! grep -R -n 'latest' .ci/versions.yaml

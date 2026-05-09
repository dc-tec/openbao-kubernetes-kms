SHELL := /bin/sh

.PHONY: help bootstrap build build-linux image image-smoke deployment-samples-check release-artifacts checksums clean-dist fmt verify-fmt lint lint-ci test test-race test-e2e test-e2e-openbao test-e2e-openbao-ci tidy verify-tidy ci-core docs-check versions-check verify-e2e-manifest lint-ast test-ast semgrep-rules-test semgrep-scan semgrep-ci install-go-tools vulncheck

GO_VERSION := $(shell cat .go-version)
GO ?= go
GOBIN ?= $(CURDIR)/bin
AST_GREP ?= .github/tools/node_modules/.bin/ast-grep
SEMGREP ?= semgrep
GOFUMPT ?= gofumpt
STATICCHECK ?= staticcheck
GOVULNCHECK ?= govulncheck
GOLANGCI_LINT ?= golangci-lint
GINKGO ?= $(if $(wildcard $(GOBIN)/ginkgo),$(GOBIN)/ginkgo,ginkgo)
SEMGREP_CONFIG_FLAGS ?= --config .semgrep/rules
SEMGREP_TARGETS ?= cmd internal
SEMGREP_ARTIFACT_DIR ?= dist/semgrep
SEMGREP_OUTPUT_JSON ?= $(SEMGREP_ARTIFACT_DIR)/semgrep.json
E2E_PACKAGE ?= ./test/e2e
E2E_LABEL_FILTER ?=
E2E_TIMEOUT ?= 30m
E2E_ARTIFACT_DIR ?= artifacts/e2e
E2E_JUNIT_REPORT ?= $(E2E_ARTIFACT_DIR)/junit.xml
E2E_JSON_REPORT ?= $(E2E_ARTIFACT_DIR)/ginkgo.json
E2E_PARALLEL_NODES ?= 1
E2E_GINKGO_EXTRA_ARGS ?=
E2E_OPENBAO_IMAGE ?= $(shell awk '/^[[:space:]]*image:[[:space:]]*/ { gsub("\"", "", $$2); print $$2; exit }' .ci/versions.yaml)
BINARY_NAME ?= bao-kms-provider
BIN ?= bin/$(BINARY_NAME)
DOCKER ?= docker
IMAGE_REPOSITORY ?= ghcr.io/dc-tec/bao-kms-provider
IMAGE_TAG ?= $(VERSION)
IMAGE ?= $(IMAGE_REPOSITORY):$(IMAGE_TAG)
IMAGE_PLATFORM ?= linux/$(shell "$(GO)" env GOARCH)
DIST_DIR ?= dist/release
CHECKSUM_FILE ?= $(DIST_DIR)/checksums.txt
CHECKSUM ?= shasum -a 256
RELEASE_TARGETS ?= linux/amd64 linux/arm64
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
GINKGO_VERSION ?= v2.28.3

help:
	@printf '%s\n' 'Targets:'
	@printf '%s\n' '  bootstrap       Prepare local development prerequisites once implementation exists'
	@printf '%s\n' '  build           Build bao-kms-provider with version metadata'
	@printf '%s\n' '  build-linux     Cross-compile Linux release artifacts'
	@printf '%s\n' '  image           Build local distroless non-root container image'
	@printf '%s\n' '  image-smoke     Build and smoke-test local container image'
	@printf '%s\n' '  deployment-samples-check Validate deployment sample manifests and scripts'
	@printf '%s\n' '  checksums       Generate release artifact checksums'
	@printf '%s\n' '  fmt             Format Go sources when go.mod exists'
	@printf '%s\n' '  lint            Run lightweight lint checks'
	@printf '%s\n' '  lint-ast        Run ast-grep rules when ast-grep and Go code are present'
	@printf '%s\n' '  test            Run Go tests when go.mod exists'
	@printf '%s\n' '  test-race       Run race-enabled Go tests'
	@printf '%s\n' '  test-e2e        Run Ginkgo/Gomega E2E tests'
	@printf '%s\n' '  test-e2e-openbao Run ephemeral OpenBao CI E2E tests'
	@printf '%s\n' '  ci-core         Run the local core quality gate'
	@printf '%s\n' '  docs-check      Check docs for known formatting artifacts'
	@printf '%s\n' '  install-go-tools Install pinned optional Go quality tools into bin/'
	@printf '%s\n' '  semgrep-ci      Run Semgrep rule tests and blocking scan when semgrep is available'
	@printf '%s\n' '  versions-check  Check central version policy exists'
	@printf '%s\n' '  verify-e2e-manifest Validate the E2E suite manifest'

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
	@GOBIN="$(GOBIN)" "$(GO)" install github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)

build:
	@mkdir -p "$$(dirname "$(BIN)")"
	@"$(GO)" build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN)" ./cmd/bao-kms-provider

image:
	@DOCKER_BUILDKIT=1 "$(DOCKER)" build \
		--platform "$(IMAGE_PLATFORM)" \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--build-arg DIRTY="$(DIRTY)" \
		-t "$(IMAGE)" .

image-smoke: image
	@user="$$("$(DOCKER)" image inspect "$(IMAGE)" --format '{{.Config.User}}')"; \
	if [ "$$user" != "65532:65532" ]; then \
		printf 'image user must be 65532:65532, got %s\n' "$$user"; \
		exit 1; \
	fi
	@"$(DOCKER)" run --rm "$(IMAGE)" version | grep -q '^version: '

build-linux: release-artifacts

release-artifacts: clean-dist
	@set -eu; \
	mkdir -p "$(DIST_DIR)"; \
	for target in $(RELEASE_TARGETS); do \
		goos="$${target%/*}"; \
		goarch="$${target#*/}"; \
		artifact="$(DIST_DIR)/$(BINARY_NAME)_$(VERSION)_$${goos}_$${goarch}"; \
		printf 'building %s\n' "$$artifact"; \
		CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" "$(GO)" build -trimpath -ldflags "$(LDFLAGS)" -o "$$artifact" ./cmd/bao-kms-provider; \
	done
	@$(MAKE) checksums

checksums:
	@set -eu; \
	artifacts="$$(find "$(DIST_DIR)" -maxdepth 1 -type f -name '$(BINARY_NAME)_$(VERSION)_*' -exec basename {} \; | sort)"; \
	if [ -z "$$artifacts" ]; then \
		printf '%s\n' 'No release artifacts found for checksum generation.'; \
		exit 1; \
	fi; \
	cd "$(DIST_DIR)" && $(CHECKSUM) $$artifacts > "$$(basename "$(CHECKSUM_FILE)")"

clean-dist:
	@rm -rf "$(DIST_DIR)"

fmt:
	@if find cmd internal test -name '*.go' 2>/dev/null | grep -q .; then \
		gofmt -w $$(find cmd internal test -name '*.go'); \
		if command -v "$(GOFUMPT)" >/dev/null 2>&1; then "$(GOFUMPT)" -w cmd internal test; fi; \
	else \
		printf '%s\n' 'No Go files yet; skipping Go formatting.'; \
	fi

verify-fmt:
	@if find cmd internal test -name '*.go' 2>/dev/null | grep -q .; then \
		unformatted="$$(gofmt -l $$(find cmd internal test -name '*.go'))"; \
		if [ -n "$$unformatted" ]; then printf '%s\n' "$$unformatted"; exit 1; fi; \
		if command -v "$(GOFUMPT)" >/dev/null 2>&1; then \
			unformatted="$$("$(GOFUMPT)" -l cmd internal test)"; \
			if [ -n "$$unformatted" ]; then printf '%s\n' "$$unformatted"; exit 1; fi; \
		else \
			printf '%s\n' 'gofumpt not installed; skipping gofumpt verification.'; \
		fi; \
	else \
		printf '%s\n' 'No Go files yet; skipping Go formatting verification.'; \
	fi

lint: docs-check versions-check verify-e2e-manifest verify-fmt test-ast lint-ast semgrep-ci
	@"$(GO)" vet ./...
	@if command -v "$(STATICCHECK)" >/dev/null 2>&1; then "$(STATICCHECK)" ./...; else printf '%s\n' 'staticcheck not installed; skipping staticcheck.'; fi
	@if command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1; then "$(GOLANGCI_LINT)" run; else printf '%s\n' 'golangci-lint not installed; skipping golangci-lint.'; fi

lint-ci: lint vulncheck

test:
	@"$(GO)" test ./...

test-race:
	@"$(GO)" test -race ./...

test-e2e: verify-e2e-manifest
	@mkdir -p "$(E2E_ARTIFACT_DIR)"
	@if command -v "$(GINKGO)" >/dev/null 2>&1; then \
		set -- --tags=e2e --timeout="$(E2E_TIMEOUT)" --junit-report="$(E2E_JUNIT_REPORT)" --json-report="$(E2E_JSON_REPORT)"; \
		if [ "$(E2E_PARALLEL_NODES)" != "1" ]; then set -- "$$@" --procs="$(E2E_PARALLEL_NODES)"; fi; \
		if [ -n "$(E2E_LABEL_FILTER)" ]; then set -- "$$@" --label-filter="$(E2E_LABEL_FILTER)"; fi; \
		set -- "$$@" $(E2E_GINKGO_EXTRA_ARGS) "$(E2E_PACKAGE)"; \
		"$(GINKGO)" "$$@"; \
	else \
		if [ -n "$(E2E_LABEL_FILTER)" ]; then \
			printf '%s\n' 'ginkgo is required when E2E_LABEL_FILTER is set; run make install-go-tools or clear E2E_LABEL_FILTER.'; \
			exit 1; \
		fi; \
		"$(GO)" test -tags=e2e "$(E2E_PACKAGE)" -run '^TestE2E$$' -count=1; \
	fi

test-e2e-openbao: test-e2e-openbao-ci

test-e2e-openbao-ci: verify-e2e-manifest
	@if command -v "$(GINKGO)" >/dev/null 2>&1; then \
		E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" "$(MAKE)" test-e2e E2E_LABEL_FILTER='openbao && transit && ci' E2E_TIMEOUT=2m; \
	else \
		E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestE2E$$' -count=1; \
	fi

tidy:
	@"$(GO)" mod tidy

verify-tidy:
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp go.mod "$$tmp/go.mod"; \
	cp go.sum "$$tmp/go.sum"; \
	"$(GO)" mod tidy; \
	cmp -s go.mod "$$tmp/go.mod" && cmp -s go.sum "$$tmp/go.sum"

vulncheck:
	@if command -v "$(GOVULNCHECK)" >/dev/null 2>&1; then "$(GOVULNCHECK)" ./...; else printf '%s\n' 'govulncheck not installed; skipping govulncheck.'; fi

deployment-samples-check:
	@"$(GO)" test ./test/deployment
	@for script in hack/kubeadm/*.sh; do sh -n "$$script"; done
	@if command -v systemd-analyze >/dev/null 2>&1; then \
		systemd-analyze verify deploy/systemd/bao-kms-provider.service; \
	else \
		printf '%s\n' 'systemd-analyze not installed; skipping systemd unit verification.'; \
	fi

ci-core: verify-tidy lint vulncheck test test-race build release-artifacts

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

verify-e2e-manifest:
	@"$(GO)" test ./test/e2e -run '^TestE2EManifest$$' -count=1

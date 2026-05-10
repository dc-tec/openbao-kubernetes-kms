SHELL := /bin/sh

.PHONY: help bootstrap build build-linux image image-smoke deployment-samples-check release-artifacts release-packages release-bundles release-distribution checksums clean-dist fmt verify-fmt lint lint-ci test test-race test-e2e test-e2e-openbao test-e2e-openbao-ci test-e2e-provider-openbao-ci test-e2e-provider-failure-openbao-ci test-e2e-provider-ha-openbao-ci test-e2e-provider-decrypt-storm-openbao-ci test-e2e-provider-load-soak-openbao-ci test-e2e-provider-restore-openbao-ci test-e2e-provider-rotation-openbao-ci test-e2e-provider-upgrade-rollback-openbao-ci test-e2e-kind-smoke test-e2e-kind-convergence test-e2e-kind-upgrade-rollback test-e2e-kind-dr-runbook tidy vendor verify-tidy verify-vendor ci-core docs-check docs-deps docs-build docs-serve versions-check verify-e2e-manifest lint-ast test-ast semgrep-rules-test semgrep-scan semgrep-ci install-go-tools install-release-tools vulncheck go-licenses license-check license-report security-ci security-scan-fs security-scan-image security-scan-built-image fuzz harvester-lab-values harvester-lab-lint harvester-lab-render harvester-lab-dry-run harvester-lab-create harvester-lab-status harvester-lab-wait harvester-lab-ssh-config harvester-lab-wait-ssh harvester-lab-bootstrap-openbao harvester-lab-bootstrap-kubeadm harvester-lab-bootstrap-mcp harvester-lab-bootstrap-guests harvester-lab-verify-guests harvester-lab-wire-provider harvester-lab-wire-systemd harvester-lab-wire-static harvester-lab-wire-mcp harvester-lab-verify-kms harvester-lab-verify-recovery harvester-lab-verify-openbao-outage harvester-lab-verify-load harvester-lab-verify-upgrade-rollback harvester-lab-verify-paired-restore harvester-lab-verify-mcp-recovery harvester-lab-production-gate harvester-lab-e2e harvester-lab-destroy

GO_VERSION := $(shell cat .go-version)
GO ?= go
GOBIN ?= $(CURDIR)/bin
AST_GREP ?= .github/tools/node_modules/.bin/ast-grep
SEMGREP ?= semgrep
GOFUMPT ?= $(if $(wildcard $(GOBIN)/gofumpt),$(GOBIN)/gofumpt,gofumpt)
STATICCHECK ?= $(if $(wildcard $(GOBIN)/staticcheck),$(GOBIN)/staticcheck,staticcheck)
GOVULNCHECK ?= $(if $(wildcard $(GOBIN)/govulncheck),$(GOBIN)/govulncheck,govulncheck)
GOLANGCI_LINT ?= $(if $(wildcard $(GOBIN)/golangci-lint),$(GOBIN)/golangci-lint,golangci-lint)
GO_LICENSES ?= $(if $(wildcard $(GOBIN)/go-licenses),$(GOBIN)/go-licenses,go-licenses)
NFPM ?= $(if $(wildcard $(GOBIN)/nfpm),$(GOBIN)/nfpm,nfpm)
TRIVY ?= trivy
GINKGO ?= $(if $(wildcard $(GOBIN)/ginkgo),$(GOBIN)/ginkgo,ginkgo)
SEMGREP_CONFIG_FLAGS ?= --config .semgrep/rules
SEMGREP_TARGETS ?= cmd internal .github
SEMGREP_ARTIFACT_DIR ?= dist/semgrep
SEMGREP_OUTPUT_JSON ?= $(SEMGREP_ARTIFACT_DIR)/semgrep.json
FUZZTIME ?= 10s
E2E_PACKAGE ?= ./test/e2e
E2E_LABEL_FILTER ?=
E2E_TIMEOUT ?= 30m
E2E_ARTIFACT_DIR ?= artifacts/e2e
E2E_JUNIT_REPORT ?= $(E2E_ARTIFACT_DIR)/junit.xml
E2E_JSON_REPORT ?= $(E2E_ARTIFACT_DIR)/ginkgo.json
E2E_PARALLEL_NODES ?= 1
E2E_GINKGO_EXTRA_ARGS ?=
E2E_OPENBAO_IMAGE ?= $(shell awk '/^[[:space:]]*image:[[:space:]]*/ { gsub("\"", "", $$2); print $$2; exit }' .ci/versions.yaml)
E2E_PROVIDER_IMAGE ?= $(IMAGE_REPOSITORY):e2e-$(COMMIT)
E2E_PROVIDER_OLD_IMAGE ?= $(IMAGE_REPOSITORY):e2e-upgrade-old-$(COMMIT)
E2E_PROVIDER_NEW_IMAGE ?= $(IMAGE_REPOSITORY):e2e-upgrade-new-$(COMMIT)
E2E_PROVIDER_BUILD ?= true
E2E_KIND_NODE_IMAGE ?= $(shell awk '/^[[:space:]]*kindNodeImage:[[:space:]]*/ { print $$2; exit }' .ci/versions.yaml)
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
PACKAGE_FORMATS ?= deb rpm
PACKAGE_RELEASE ?= 1
NFPM_CONFIG ?= deploy/package/linux/nfpm.yaml
VERSION ?= 0.0.0-dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf '%s' unknown)
BUILD_DATE ?= $(shell if [ -n "$${SOURCE_DATE_EPOCH:-}" ]; then date -u -r "$${SOURCE_DATE_EPOCH}" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$${SOURCE_DATE_EPOCH}" +%Y-%m-%dT%H:%M:%SZ; else date -u +%Y-%m-%dT%H:%M:%SZ; fi)
DIRTY ?= $(shell if [ -n "$$(git status --porcelain 2>/dev/null)" ]; then printf '%s' true; else printf '%s' false; fi)
VERSION_PKG := github.com/dc-tec/openbao-kubernetes-kms/internal/version
GO_BUILD_FLAGS ?= -trimpath -buildvcs=false
LDFLAGS := -s -w -X $(VERSION_PKG).version=$(VERSION) -X $(VERSION_PKG).commit=$(COMMIT) -X $(VERSION_PKG).buildDate=$(BUILD_DATE) -X $(VERSION_PKG).dirty=$(DIRTY)
GOFUMPT_VERSION ?= v0.9.2
STATICCHECK_VERSION ?= v0.7.0
GOVULNCHECK_VERSION ?= v1.2.0
GOLANGCI_LINT_VERSION ?= v2.11.4
GINKGO_VERSION ?= v2.28.3
GO_LICENSES_VERSION ?= v2.0.1
NFPM_VERSION ?= v2.46.3
GO_LICENSES_ALLOWED ?= Apache-2.0 BSD-2-Clause BSD-3-Clause ISC MIT MPL-2.0 Unicode-DFS-2016
GO_LICENSES_IGNORE ?= github.com/dc-tec/openbao-kubernetes-kms
GO_LICENSES_PACKAGE_TARGETS ?= ./cmd/bao-kms-provider
LICENSE_REPORT_DIR ?= dist/licenses
go_licenses_empty :=
go_licenses_space := $(go_licenses_empty) $(go_licenses_empty)
go_licenses_comma := ,
GO_LICENSES_ALLOWED_CSV := $(subst $(go_licenses_space),$(go_licenses_comma),$(strip $(GO_LICENSES_ALLOWED)))
HUGO_VERSION ?= v0.159.1
HUGO_RUN := GOFLAGS="-mod=mod" "$(GO)" run github.com/gohugoio/hugo@$(HUGO_VERSION)
DOCS_BASE_URL ?= https://dc-tec.github.io/openbao-kubernetes-kms/
DOCS_OUT ?= public

help:
	@printf '%s\n' 'Targets:'
	@printf '%s\n' '  bootstrap       Prepare local development prerequisites once implementation exists'
	@printf '%s\n' '  build           Build bao-kms-provider with version metadata'
	@printf '%s\n' '  build-linux     Cross-compile Linux release artifacts'
	@printf '%s\n' '  image           Build local distroless non-root container image'
	@printf '%s\n' '  image-smoke     Build and smoke-test local container image'
	@printf '%s\n' '  deployment-samples-check Validate deployment sample manifests and scripts'
	@printf '%s\n' '  release-packages Build native systemd .deb/.rpm packages from release binaries'
	@printf '%s\n' '  release-bundles  Build deterministic systemd and static-pod tarball bundles'
	@printf '%s\n' '  release-distribution Build release packages and bundles, then refresh checksums'
	@printf '%s\n' '  checksums       Generate release artifact checksums'
	@printf '%s\n' '  fmt             Format Go sources when go.mod exists'
	@printf '%s\n' '  lint            Run lightweight lint checks'
	@printf '%s\n' '  lint-ast        Run ast-grep rules when ast-grep and Go code are present'
	@printf '%s\n' '  test            Run Go tests when go.mod exists'
	@printf '%s\n' '  test-race       Run race-enabled Go tests'
	@printf '%s\n' '  test-e2e        Run Ginkgo/Gomega E2E tests'
	@printf '%s\n' '  test-e2e-openbao Run ephemeral OpenBao CI E2E tests'
	@printf '%s\n' '  test-e2e-provider-openbao-ci Run containerized provider/OpenBao KMS v2 E2E tests'
	@printf '%s\n' '  test-e2e-provider-failure-openbao-ci Run provider/OpenBao failure-mode E2E tests'
	@printf '%s\n' '  test-e2e-provider-ha-openbao-ci Run provider/OpenBao HA failover E2E tests'
	@printf '%s\n' '  test-e2e-provider-decrypt-storm-openbao-ci Run provider/OpenBao decrypt storm smoke E2E tests'
	@printf '%s\n' '  test-e2e-provider-load-soak-openbao-ci Run provider/OpenBao load-soak E2E tests'
	@printf '%s\n' '  test-e2e-provider-restore-openbao-ci Run provider/OpenBao backend replacement and restore E2E tests'
	@printf '%s\n' '  test-e2e-provider-rotation-openbao-ci Run provider/OpenBao Transit rotation E2E tests'
	@printf '%s\n' '  test-e2e-provider-upgrade-rollback-openbao-ci Run provider binary upgrade/rollback E2E tests'
	@printf '%s\n' '  test-e2e-kind-smoke Run pinned Kind Kubernetes KMS v2 smoke tests'
	@printf '%s\n' '  test-e2e-kind-convergence Run pinned Kind multi-control-plane convergence tests'
	@printf '%s\n' '  test-e2e-kind-upgrade-rollback Run pinned Kind static-pod upgrade/rollback tests'
	@printf '%s\n' '  test-e2e-kind-dr-runbook Run pinned Kind DR restore runbook tests'
	@printf '%s\n' '  ci-core         Run the local core quality gate'
	@printf '%s\n' '  docs-check      Check docs for known formatting artifacts'
	@printf '%s\n' '  docs-deps       Install the pinned Hugo binary locally'
	@printf '%s\n' '  docs-build      Build the Hugo docs site into public/'
	@printf '%s\n' '  docs-serve      Serve the docs site locally on http://localhost:1313/'
	@printf '%s\n' '  harvester-lab-values Generate ignored local Harvester lab values'
	@printf '%s\n' '  harvester-lab-render Render local-only Harvester VM manifests'
	@printf '%s\n' '  harvester-lab-create Create local-only Harvester VM lab'
	@printf '%s\n' '  harvester-lab-bootstrap-guests Bootstrap OpenBao and kubeadm inside lab VMs'
	@printf '%s\n' '  harvester-lab-bootstrap-mcp Bootstrap optional multi-control-plane kubeadm VMs'
	@printf '%s\n' '  harvester-lab-verify-guests Verify OpenBao and kubeadm guest bootstrap'
	@printf '%s\n' '  harvester-lab-wire-provider Wire provider into both kubeadm VMs'
	@printf '%s\n' '  harvester-lab-wire-mcp Wire provider into optional multi-control-plane kubeadm VMs'
	@printf '%s\n' '  harvester-lab-verify-kms Verify Kubernetes KMS v2 envelope storage'
	@printf '%s\n' '  harvester-lab-verify-mcp-recovery Verify optional multi-control-plane kubeadm recovery'
	@printf '%s\n' '  harvester-lab-production-gate Run local-only recovery, outage, restore, upgrade, and load checks'
	@printf '%s\n' '  harvester-lab-e2e Run the full local-only Harvester kubeadm lab'
	@printf '%s\n' '  harvester-lab-destroy Destroy local-only Harvester VM lab'
	@printf '%s\n' '  install-go-tools Install pinned optional Go quality tools into bin/'
	@printf '%s\n' '  install-release-tools Install pinned release packaging tools into bin/'
	@printf '%s\n' '  semgrep-ci      Run Semgrep rule tests and blocking scan when semgrep is available'
	@printf '%s\n' '  security-ci     Run vulnerability, license, and filesystem security scans'
	@printf '%s\n' '  verify-vendor   Verify vendor/ is synchronized with go.mod and go.sum'
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
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install github.com/google/go-licenses/v2@$(GO_LICENSES_VERSION)

install-release-tools:
	@mkdir -p "$(GOBIN)"
	@env -u GOFLAGS GOBIN="$(GOBIN)" "$(GO)" install github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION)

build:
	@mkdir -p "$$(dirname "$(BIN)")"
	@"$(GO)" build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(BIN)" ./cmd/bao-kms-provider

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
			CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" "$(GO)" build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$$artifact" ./cmd/bao-kms-provider; \
	done
	@$(MAKE) checksums

release-packages:
	@if ! command -v "$(NFPM)" >/dev/null 2>&1; then \
		printf '%s\n' 'nfpm not installed; run make install-release-tools.'; \
		exit 1; \
	fi
	@set -eu; \
	for target in $(RELEASE_TARGETS); do \
		goos="$${target%/*}"; \
		goarch="$${target#*/}"; \
		binary="$(DIST_DIR)/$(BINARY_NAME)_$(VERSION)_$${goos}_$${goarch}"; \
		if [ ! -f "$$binary" ]; then \
			printf 'release binary not found: %s\n' "$$binary" >&2; \
			exit 1; \
		fi; \
		for format in $(PACKAGE_FORMATS); do \
			artifact="$(DIST_DIR)/$(BINARY_NAME)_$(VERSION)_$${goos}_$${goarch}.$$format"; \
			printf 'packaging %s\n' "$$artifact"; \
			SOURCE_DATE_EPOCH="$${SOURCE_DATE_EPOCH:-0}" \
			VERSION="$(VERSION)" \
			NFPM_ARCH="$$goarch" \
			NFPM_RELEASE="$(PACKAGE_RELEASE)" \
			BUILD_DATE="$(BUILD_DATE)" \
			PACKAGE_BINARY="$$binary" \
				"$(NFPM)" package --config "$(NFPM_CONFIG)" --packager "$$format" --target "$$artifact"; \
		done; \
	done

release-bundles:
	@set -eu; \
	source_date_epoch="$${SOURCE_DATE_EPOCH:-0}"; \
	image_ref="$(IMAGE)"; \
	if [ -n "$${IMAGE_DIGEST:-}" ]; then image_ref="$${image_ref}@$${IMAGE_DIGEST}"; fi; \
	for target in $(RELEASE_TARGETS); do \
		goos="$${target%/*}"; \
		goarch="$${target#*/}"; \
		binary="$(DIST_DIR)/$(BINARY_NAME)_$(VERSION)_$${goos}_$${goarch}"; \
		if [ ! -f "$$binary" ]; then \
			printf 'release binary not found: %s\n' "$$binary" >&2; \
			exit 1; \
		fi; \
		GOFLAGS="-mod=vendor" "$(GO)" run ./hack/tools/release_bundle \
			-kind systemd \
			-output "$(DIST_DIR)/$(BINARY_NAME)_$(VERSION)_systemd_$${goos}_$${goarch}.tar.gz" \
			-prefix "$(BINARY_NAME)_$(VERSION)_systemd_$${goos}_$${goarch}" \
			-binary "$$binary" \
			-source-date-epoch "$$source_date_epoch"; \
	done; \
	GOFLAGS="-mod=vendor" "$(GO)" run ./hack/tools/release_bundle \
		-kind static-pod \
		-output "$(DIST_DIR)/$(BINARY_NAME)_$(VERSION)_static-pod.tar.gz" \
		-prefix "$(BINARY_NAME)_$(VERSION)_static-pod" \
		-image-ref "$$image_ref" \
		-source-date-epoch "$$source_date_epoch"

release-distribution: release-packages release-bundles
	@$(MAKE) checksums

checksums:
	@set -eu; \
	artifacts="$$(find "$(DIST_DIR)" -maxdepth 1 -type f \
		! -name "$$(basename "$(CHECKSUM_FILE)")" \
		! -name '*.bundle' \
		! -name 'provenance-index.json' \
		-exec basename {} \; | sort)"; \
	if [ -z "$$artifacts" ]; then \
		printf '%s\n' 'No release artifacts found for checksum generation.'; \
		exit 1; \
	fi; \
	cd "$(DIST_DIR)" && $(CHECKSUM) $$artifacts > "$$(basename "$(CHECKSUM_FILE)")"

clean-dist:
	@rm -rf "$(DIST_DIR)"

fmt:
	@if find cmd internal test hack/tools -name '*.go' 2>/dev/null | grep -q .; then \
		gofmt -w $$(find cmd internal test hack/tools -name '*.go'); \
		if command -v "$(GOFUMPT)" >/dev/null 2>&1; then "$(GOFUMPT)" -w cmd internal test hack/tools; fi; \
	else \
		printf '%s\n' 'No Go files yet; skipping Go formatting.'; \
	fi

verify-fmt:
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
		E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" "$(MAKE)" test-e2e E2E_LABEL_FILTER='openbao && transit && ci' E2E_TIMEOUT=6m; \
	else \
		E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestE2E$$' -count=1; \
	fi
	@$(MAKE) test-e2e-provider-openbao-ci

test-e2e-provider-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestProviderContainerFullStackE2E$$' -count=1 -timeout=4m

test-e2e-provider-failure-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestProvider(OpenBaoOutageFailsClosed|OpenBaoSealFailsClosed|BadPolicyFailsClosed|ExpiredJWTFailsClosed|JWTFileRotation|JWTSigningKeyRollover|TransitKeyMissingFailsClosed|StatusStalenessFailsClosed|StaleSocketReclaimed)E2E$$' -count=1 -timeout=10m

test-e2e-provider-ha-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestProviderOpenBaoHAFailoverE2E$$' -count=1 -timeout=7m

test-e2e-provider-decrypt-storm-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestProviderDecryptStormSmokeE2E$$' -count=1 -timeout=5m

test-e2e-provider-load-soak-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestProviderLoadSoakE2E$$' -count=1 -timeout=6m

test-e2e-provider-restore-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestProvider(OpenBaoBackendReplacement|ContainerizedDRRestore)E2E$$' -count=1 -timeout=8m

test-e2e-provider-rotation-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestProviderTransitRotationE2E$$' -count=1 -timeout=8m

test-e2e-provider-upgrade-rollback-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_OLD_IMAGE)" VERSION="$(VERSION)-e2e-old" COMMIT="$(COMMIT)-old"; \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_NEW_IMAGE)" VERSION="$(VERSION)-e2e-new" COMMIT="$(COMMIT)-new"; \
	fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_OLD_IMAGE="$(E2E_PROVIDER_OLD_IMAGE)" E2E_PROVIDER_NEW_IMAGE="$(E2E_PROVIDER_NEW_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestProviderBinaryUpgradeRollbackE2E$$' -count=1 -timeout=8m

test-e2e-kind-smoke: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_KIND_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" E2E_KIND_NODE_IMAGE="$(E2E_KIND_NODE_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestKindKMSV2SmokeE2E$$' -count=1 -timeout=30m

test-e2e-kind-convergence: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_KIND_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" E2E_KIND_NODE_IMAGE="$(E2E_KIND_NODE_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestKindMultiControlPlaneConvergenceE2E$$' -count=1 -timeout=45m

test-e2e-kind-upgrade-rollback: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_KIND_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" E2E_KIND_NODE_IMAGE="$(E2E_KIND_NODE_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestKindStaticPodUpgradeRollbackE2E$$' -count=1 -timeout=30m

test-e2e-kind-dr-runbook: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_KIND_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_IMAGE)" E2E_KIND_NODE_IMAGE="$(E2E_KIND_NODE_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestKindDRRestoreRunbookE2E$$' -count=1 -timeout=35m

tidy:
	@GOFLAGS="-mod=mod" "$(GO)" mod tidy

vendor:
	@GOFLAGS="-mod=mod" "$(GO)" mod vendor

verify-tidy:
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp go.mod "$$tmp/go.mod"; \
	cp go.sum "$$tmp/go.sum"; \
	GOFLAGS="-mod=mod" "$(GO)" mod tidy; \
	cmp -s go.mod "$$tmp/go.mod" && cmp -s go.sum "$$tmp/go.sum"

verify-vendor:
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp go.mod "$$tmp/go.mod"; \
	cp go.sum "$$tmp/go.sum"; \
	if [ -d vendor ]; then cp -R vendor "$$tmp/vendor"; else mkdir "$$tmp/vendor"; fi; \
	GOFLAGS="-mod=mod" "$(GO)" mod vendor; \
	cmp -s go.mod "$$tmp/go.mod" && cmp -s go.sum "$$tmp/go.sum" && diff -qr "$$tmp/vendor" vendor >/dev/null

vulncheck:
	@if command -v "$(GOVULNCHECK)" >/dev/null 2>&1; then "$(GOVULNCHECK)" ./...; else printf '%s\n' 'govulncheck not installed; skipping govulncheck.'; fi

go-licenses:
	@if ! command -v "$(GO_LICENSES)" >/dev/null 2>&1; then \
		printf '%s\n' 'go-licenses not installed; run make install-go-tools.'; \
		exit 1; \
	fi

license-check: verify-vendor go-licenses
	@"$(GO_LICENSES)" check \
		--allowed_licenses="$(GO_LICENSES_ALLOWED_CSV)" \
		--ignore "$(GO_LICENSES_IGNORE)" \
		$(GO_LICENSES_PACKAGE_TARGETS)

license-report: verify-vendor go-licenses
	@mkdir -p "$(LICENSE_REPORT_DIR)"
	@"$(GO_LICENSES)" report \
		--ignore "$(GO_LICENSES_IGNORE)" \
		$(GO_LICENSES_PACKAGE_TARGETS) \
		> "$(LICENSE_REPORT_DIR)/go-licenses-report.csv" \
		2> "$(LICENSE_REPORT_DIR)/go-licenses-report.stderr.log"
	@printf 'License report written to %s\n' "$(LICENSE_REPORT_DIR)/go-licenses-report.csv"

security-ci: vulncheck license-check security-scan-fs

security-scan-fs:
	@if command -v "$(TRIVY)" >/dev/null 2>&1; then \
		"$(TRIVY)" fs \
			--scanners vuln,misconfig \
			--severity HIGH,CRITICAL \
			--ignore-unfixed \
			--exit-code 1 \
			--ignorefile .trivyignore \
			--skip-version-check \
			--skip-dirs .github/tools/node_modules \
			--skip-dirs artifacts \
			--skip-dirs bin \
			--skip-dirs dist \
			--skip-dirs public \
			--skip-dirs vendor \
			.; \
	else \
		printf '%s\n' 'trivy not installed; skipping filesystem security scan.'; \
	fi

security-scan-image:
	@if command -v "$(TRIVY)" >/dev/null 2>&1; then \
		"$(TRIVY)" image \
			--severity HIGH,CRITICAL \
			--ignore-unfixed \
			--exit-code 1 \
			--skip-version-check \
			"$(IMAGE)"; \
	else \
		printf '%s\n' 'trivy not installed; skipping image security scan.'; \
	fi

security-scan-built-image: image security-scan-image

fuzz:
	@"$(GO)" test ./internal/aad -run '^$$' -fuzz '^FuzzParseAnnotations$$' -fuzztime="$(FUZZTIME)"
	@"$(GO)" test ./internal/keyregistry -run '^$$' -fuzz '^FuzzParseKeyID$$' -fuzztime="$(FUZZTIME)"

deployment-samples-check:
	@"$(GO)" test ./test/deployment
	@for script in hack/kubeadm/*.sh; do sh -n "$$script"; done
	@for script in hack/harvester/*.sh hack/harvester/remote/*.sh; do sh -n "$$script"; done
	@for script in deploy/package/linux/scripts/*.sh; do sh -n "$$script"; done
	@if command -v systemd-analyze >/dev/null 2>&1; then \
		systemd-analyze verify deploy/systemd/bao-kms-provider.service; \
	else \
		printf '%s\n' 'systemd-analyze not installed; skipping systemd unit verification.'; \
	fi

ci-core: verify-tidy verify-vendor lint security-ci test test-race fuzz build release-artifacts

docs-check:
	@! grep -R -n --exclude-dir=_archive $$(printf '\357\277\274') README.md docs
	@! grep -R -n --exclude-dir=_archive '⸻' README.md docs
	@! grep -R -n --exclude-dir=_archive 'openbao-kms-provider' README.md docs
	@! grep -R -n --exclude-dir=_archive '—' README.md docs

docs-deps:
	@GOFLAGS="-mod=mod" "$(GO)" install github.com/gohugoio/hugo@$(HUGO_VERSION)

docs-build:
	@$(HUGO_RUN) --source . --baseURL "$(DOCS_BASE_URL)" --destination "$(DOCS_OUT)" --gc --minify

docs-serve:
	@$(HUGO_RUN) server --source . --baseURL http://localhost:1313/

harvester-lab-values:
	@hack/harvester/lab.sh values

harvester-lab-lint:
	@hack/harvester/lab.sh lint

harvester-lab-render:
	@hack/harvester/lab.sh render

harvester-lab-dry-run:
	@hack/harvester/lab.sh dry-run

harvester-lab-create:
	@hack/harvester/lab.sh create

harvester-lab-status:
	@hack/harvester/lab.sh status

harvester-lab-wait:
	@hack/harvester/lab.sh wait

harvester-lab-ssh-config:
	@hack/harvester/lab.sh ssh-config

harvester-lab-wait-ssh:
	@hack/harvester/lab.sh wait-ssh

harvester-lab-bootstrap-openbao:
	@hack/harvester/lab.sh bootstrap-openbao

harvester-lab-bootstrap-kubeadm:
	@hack/harvester/lab.sh bootstrap-kubeadm

harvester-lab-bootstrap-mcp:
	@hack/harvester/lab.sh bootstrap-mcp

harvester-lab-bootstrap-guests:
	@hack/harvester/lab.sh bootstrap-guests

harvester-lab-verify-guests:
	@hack/harvester/lab.sh verify-guests

harvester-lab-wire-provider:
	@hack/harvester/lab.sh wire-provider

harvester-lab-wire-systemd:
	@hack/harvester/lab.sh wire-systemd

harvester-lab-wire-static:
	@hack/harvester/lab.sh wire-static

harvester-lab-wire-mcp:
	@hack/harvester/lab.sh wire-mcp

harvester-lab-verify-kms:
	@hack/harvester/lab.sh verify-kms

harvester-lab-verify-recovery:
	@hack/harvester/lab.sh verify-recovery

harvester-lab-verify-openbao-outage:
	@hack/harvester/lab.sh verify-openbao-outage

harvester-lab-verify-load:
	@hack/harvester/lab.sh verify-load

harvester-lab-verify-upgrade-rollback:
	@hack/harvester/lab.sh verify-upgrade-rollback

harvester-lab-verify-paired-restore:
	@hack/harvester/lab.sh verify-paired-restore

harvester-lab-verify-mcp-recovery:
	@hack/harvester/lab.sh verify-mcp-recovery

harvester-lab-production-gate:
	@hack/harvester/lab.sh production-gate

harvester-lab-e2e:
	@hack/harvester/lab.sh e2e

harvester-lab-destroy:
	@hack/harvester/lab.sh destroy

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

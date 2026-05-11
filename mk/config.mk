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

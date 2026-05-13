##@ Build And Release

.PHONY: build
build: ## Build bao-kms-provider with version metadata.
	@mkdir -p "$$(dirname "$(BIN)")"
	@"$(GO)" build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(BIN)" ./cmd/bao-kms-provider

.PHONY: build-certauth-spiffe
build-certauth-spiffe: ## Build host binary with SPIFFE certificate auth enabled.
	@mkdir -p "$$(dirname "$(CERTAUTH_SPIFFE_BIN)")"
	@CGO_ENABLED=0 "$(GO)" build $(GO_BUILD_FLAGS) -tags "$(CERTAUTH_SPIFFE_BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o "$(CERTAUTH_SPIFFE_BIN)" ./cmd/bao-kms-provider

.PHONY: build-certauth-pkcs11
build-certauth-pkcs11: ## Build host binary with PKCS#11 certificate auth enabled.
	@mkdir -p "$$(dirname "$(CERTAUTH_PKCS11_BIN)")"
	@CGO_ENABLED=1 "$(GO)" build $(GO_BUILD_FLAGS) -tags "$(CERTAUTH_PKCS11_BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o "$(CERTAUTH_PKCS11_BIN)" ./cmd/bao-kms-provider

.PHONY: build-certauth-combined
build-certauth-combined: ## Build host binary with PKCS#11 and SPIFFE certificate auth enabled.
	@mkdir -p "$$(dirname "$(CERTAUTH_COMBINED_BIN)")"
	@CGO_ENABLED=1 "$(GO)" build $(GO_BUILD_FLAGS) -tags "$(CERTAUTH_COMBINED_BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o "$(CERTAUTH_COMBINED_BIN)" ./cmd/bao-kms-provider

.PHONY: image
image: ## Build local distroless non-root container image.
	@DOCKER_BUILDKIT=1 "$(DOCKER)" build \
		--platform "$(IMAGE_PLATFORM)" \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--build-arg DIRTY="$(DIRTY)" \
		-t "$(IMAGE)" .

.PHONY: image-smoke
image-smoke: image ## Build and smoke-test local container image.
	@user="$$("$(DOCKER)" image inspect "$(IMAGE)" --format '{{.Config.User}}')"; \
	if [ "$$user" != "65532:65532" ]; then \
		printf 'image user must be 65532:65532, got %s\n' "$$user"; \
		exit 1; \
	fi
	@"$(DOCKER)" run --rm "$(IMAGE)" version | grep -q '^version: '

.PHONY: build-linux
build-linux: release-artifacts ## Cross-compile Linux release artifacts.

.PHONY: release-artifacts
release-artifacts: clean-dist ## Build Linux release binaries and checksums.
	@$(MAKE) release-artifacts-default
	@$(MAKE) checksums

.PHONY: release-artifacts-default
release-artifacts-default:
	@set -eu; \
	mkdir -p "$(DIST_DIR)"; \
	for target in $(RELEASE_TARGETS); do \
		goos="$${target%/*}"; \
		goarch="$${target#*/}"; \
		artifact="$(DIST_DIR)/$(BINARY_NAME)_$(VERSION)_$${goos}_$${goarch}"; \
		printf 'building %s\n' "$$artifact"; \
				CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" "$(GO)" build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$$artifact" ./cmd/bao-kms-provider; \
	done

.PHONY: release-artifacts-certauth-spiffe
release-artifacts-certauth-spiffe: clean-dist ## Build Linux SPIFFE cert-auth release binaries and checksums.
	@$(MAKE) release-artifacts-certauth-spiffe-binaries
	@$(MAKE) checksums

.PHONY: release-artifacts-with-certauth-spiffe
release-artifacts-with-certauth-spiffe: clean-dist ## Build default and SPIFFE cert-auth Linux release binaries.
	@$(MAKE) release-artifacts-default
	@$(MAKE) release-artifacts-certauth-spiffe-binaries
	@$(MAKE) checksums

.PHONY: release-artifacts-certauth-spiffe-binaries
release-artifacts-certauth-spiffe-binaries:
	@set -eu; \
	mkdir -p "$(DIST_DIR)"; \
	for target in $(RELEASE_TARGETS); do \
		goos="$${target%/*}"; \
		goarch="$${target#*/}"; \
		artifact="$(DIST_DIR)/$(CERTAUTH_SPIFFE_ARTIFACT_NAME)_$(VERSION)_$${goos}_$${goarch}"; \
		printf 'building %s\n' "$$artifact"; \
			CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" "$(GO)" build $(GO_BUILD_FLAGS) -tags "$(CERTAUTH_SPIFFE_BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o "$$artifact" ./cmd/bao-kms-provider; \
	done

.PHONY: release-artifact-certauth-pkcs11-host
release-artifact-certauth-pkcs11-host: ## Build host PKCS#11 cert-auth artifact and checksums.
	@set -eu; \
	mkdir -p "$(DIST_DIR)"; \
	goos="$$("$(GO)" env GOOS)"; \
	goarch="$$("$(GO)" env GOARCH)"; \
	artifact="$(DIST_DIR)/$(CERTAUTH_PKCS11_ARTIFACT_NAME)_$(VERSION)_$${goos}_$${goarch}"; \
	printf 'building %s\n' "$$artifact"; \
	CGO_ENABLED=1 GOOS="$$goos" GOARCH="$$goarch" "$(GO)" build $(GO_BUILD_FLAGS) -tags "$(CERTAUTH_PKCS11_BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o "$$artifact" ./cmd/bao-kms-provider
	@$(MAKE) checksums

.PHONY: release-artifact-certauth-combined-host
release-artifact-certauth-combined-host: ## Build host combined cert-auth artifact and checksums.
	@set -eu; \
	mkdir -p "$(DIST_DIR)"; \
	goos="$$("$(GO)" env GOOS)"; \
	goarch="$$("$(GO)" env GOARCH)"; \
	artifact="$(DIST_DIR)/$(CERTAUTH_COMBINED_ARTIFACT_NAME)_$(VERSION)_$${goos}_$${goarch}"; \
	printf 'building %s\n' "$$artifact"; \
	CGO_ENABLED=1 GOOS="$$goos" GOARCH="$$goarch" "$(GO)" build $(GO_BUILD_FLAGS) -tags "$(CERTAUTH_COMBINED_BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o "$$artifact" ./cmd/bao-kms-provider
	@$(MAKE) checksums

.PHONY: release-packages
release-packages: ## Build native systemd .deb/.rpm packages from release binaries.
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

.PHONY: release-bundles
release-bundles: ## Build deterministic systemd and static-pod tarball bundles.
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

.PHONY: release-distribution
release-distribution: release-artifacts ## Build release packages and bundles, then refresh checksums.
	@$(MAKE) release-packages
	@$(MAKE) release-bundles
	@$(MAKE) checksums

.PHONY: checksums
checksums: ## Generate release artifact checksums.
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

.PHONY: clean-dist
clean-dist: ## Remove release artifacts.
	@rm -rf "$(DIST_DIR)"

##@ E2E

.PHONY: test-e2e
test-e2e: verify-e2e-manifest ## Run Ginkgo/Gomega E2E tests; filter with E2E_LABEL_FILTER.
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

.PHONY: test-e2e-release-preview-openbao
test-e2e-release-preview-openbao: verify-e2e-manifest test-e2e-release-preview-openbao-images ## Run manifest-defined OpenBao preview release E2E lanes.
	@E2E_PROVIDER_BUILD=false "$(GO)" run ./hack/tools/e2e_release_gate -group openbao -make "$(MAKE)"

.PHONY: test-e2e-release-preview-kind
test-e2e-release-preview-kind: verify-e2e-manifest test-e2e-release-preview-kind-images ## Run manifest-defined Kind preview release E2E lanes.
	@E2E_PROVIDER_BUILD=false "$(GO)" run ./hack/tools/e2e_release_gate -group kind -make "$(MAKE)"

.PHONY: test-e2e-release-preview-openbao-images
test-e2e-release-preview-openbao-images: verify-e2e-manifest ## Build provider images needed by the OpenBao preview release gate.
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_OLD_IMAGE)" VERSION="$(VERSION)-e2e-old" COMMIT="$(COMMIT)-old"; \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_NEW_IMAGE)" VERSION="$(VERSION)-e2e-new" COMMIT="$(COMMIT)-new"; \
		$(MAKE) image-certauth-pkcs11-e2e; \
	fi

.PHONY: test-e2e-release-preview-kind-images
test-e2e-release-preview-kind-images: verify-e2e-manifest ## Build provider image needed by the Kind preview release gate.
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_IMAGE)"; \
	fi

.PHONY: test-e2e-openbao
test-e2e-openbao: test-e2e-openbao-ci

.PHONY: test-e2e-openbao-ci
test-e2e-openbao-ci: verify-e2e-manifest ## Run the default OpenBao CI E2E lane.
	@if command -v "$(GINKGO)" >/dev/null 2>&1; then \
		E2E_OPENBAO_CI=true \
		E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" \
		E2E_PROVIDER_IMAGE= \
		E2E_PROVIDER_OLD_IMAGE= \
		E2E_PROVIDER_NEW_IMAGE= \
		E2E_PROVIDER_CERTAUTH_PKCS11_IMAGE= \
		E2E_PROVIDER_CERTAUTH_SPIFFE_IMAGE= \
		"$(MAKE)" test-e2e E2E_LABEL_FILTER='openbao && transit && ci' E2E_TIMEOUT=6m; \
	else \
		E2E_OPENBAO_CI=true \
		E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" \
		E2E_PROVIDER_IMAGE= \
		E2E_PROVIDER_OLD_IMAGE= \
		E2E_PROVIDER_NEW_IMAGE= \
		E2E_PROVIDER_CERTAUTH_PKCS11_IMAGE= \
		E2E_PROVIDER_CERTAUTH_SPIFFE_IMAGE= \
		"$(GO)" test -v -tags=e2e ./test/e2e -run '^TestE2E$$' -count=1; \
	fi

define provider-e2e-target
.PHONY: $(1)
$(1): verify-e2e-manifest
	@if [ "$$(E2E_PROVIDER_BUILD)" != "false" ]; then $$(MAKE) image IMAGE="$$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$$(E2E_PROVIDER_IMAGE)" "$$(GO)" test -v -tags=e2e ./test/e2e -run '$(2)' -count=1 -timeout=$(3)
endef

$(eval $(call provider-e2e-target,test-e2e-provider-openbao-ci,^TestProviderContainerFullStackE2E$$$$,4m))
$(eval $(call provider-e2e-target,test-e2e-provider-cli-openbao-ci,^TestProviderCLI(HappyPath|JWTClaimDriftRedacted|UnsupportedTransitKeyTypeFails|RotationMissingStateFailsClosed)E2E$$$$,12m))
$(eval $(call provider-e2e-target,test-e2e-provider-failure-openbao-ci,^TestProvider(OpenBaoOutageFailsClosed|OpenBaoSealFailsClosed|BadPolicyFailsClosed|ExpiredJWTFailsClosed|JWTExpectedClaimDriftFailsClosed|JWTFileRotation|JWTSigningKeyRollover|TransitKeyMissingFailsClosed|StatusStalenessFailsClosed|StaleSocketReclaimed)E2E$$$$,12m))
$(eval $(call provider-e2e-target,test-e2e-provider-ha-openbao-ci,^TestProviderOpenBaoHAFailoverE2E$$$$,7m))
$(eval $(call provider-e2e-target,test-e2e-provider-decrypt-storm-openbao-ci,^TestProviderDecryptStormSmokeE2E$$$$,5m))
$(eval $(call provider-e2e-target,test-e2e-provider-decrypt-soak-openbao-ci,^TestProviderDecryptSoakE2E$$$$,7m))
$(eval $(call provider-e2e-target,test-e2e-provider-load-soak-openbao-ci,^TestProviderLoadSoakE2E$$$$,6m))
$(eval $(call provider-e2e-target,test-e2e-provider-restore-openbao-ci,^TestProvider(OpenBaoBackendReplacement|ContainerizedDRRestore)E2E$$$$,8m))
$(eval $(call provider-e2e-target,test-e2e-provider-rotation-openbao-ci,^TestProvider(TransitRotation|TransitMinDecryptionVersionBlocksHistorical|MissingStateAfterRotationFailsClosed)E2E$$$$,18m))

.PHONY: test-e2e-cert-auth-openbao-ci
test-e2e-cert-auth-openbao-ci: verify-e2e-manifest ## Run the OpenBao TLS certificate auth E2E lane.
	@if command -v "$(GINKGO)" >/dev/null 2>&1; then \
		E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" "$(MAKE)" test-e2e E2E_LABEL_FILTER='openbao && certauth && ci' E2E_TIMEOUT=6m; \
	else \
		E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" "$(GO)" test -v -tags=e2e ./test/e2e -run '^TestE2E$$' -count=1 -timeout=6m -ginkgo.label-filter='openbao && certauth && ci'; \
	fi

.PHONY: test-e2e-provider-certauth-spiffe-openbao-ci
test-e2e-provider-certauth-spiffe-openbao-ci: verify-e2e-manifest ## Run provider E2E with real SPIRE Workload API cert source.
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image-certauth-spiffe; fi
	@E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_CERTAUTH_SPIFFE_IMAGE)" E2E_SPIRE_SERVER_IMAGE="$(E2E_SPIRE_SERVER_IMAGE)" E2E_SPIRE_AGENT_IMAGE="$(E2E_SPIRE_AGENT_IMAGE)" "$(GO)" test -v -tags=e2e ./test/e2e -run '^TestProviderCertAuthSPIREWorkloadAPISourceE2E$$' -count=1 -timeout=7m

.PHONY: test-e2e-provider-certauth-spiffe-openbao-local
test-e2e-provider-certauth-spiffe-openbao-local: verify-e2e-manifest ## Run provider E2E with real SPIRE Workload API and OpenBao cert login.
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image IMAGE="$(E2E_PROVIDER_CERTAUTH_SPIFFE_IMAGE)" IMAGE_CGO_ENABLED=0 IMAGE_GO_BUILD_TAGS="$(CERTAUTH_SPIFFE_LOCAL_BUILD_TAGS)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_CERT_AUTH_URI_SAN_ALIAS=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_CERTAUTH_SPIFFE_IMAGE)" E2E_SPIRE_SERVER_IMAGE="$(E2E_SPIRE_SERVER_IMAGE)" E2E_SPIRE_AGENT_IMAGE="$(E2E_SPIRE_AGENT_IMAGE)" "$(GO)" test -v -tags=e2e ./test/e2e -run '^TestProviderCertAuthSPIREOpenBaoE2E$$' -count=1 -timeout=7m

.PHONY: test-e2e-provider-certauth-pkcs11-openbao-ci
test-e2e-provider-certauth-pkcs11-openbao-ci: verify-e2e-manifest ## Run provider E2E with real PKCS#11 SoftHSM cert source.
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then $(MAKE) image-certauth-pkcs11-e2e; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$(E2E_PROVIDER_CERTAUTH_PKCS11_IMAGE)" "$(GO)" test -v -tags=e2e ./test/e2e -run '^TestProviderCertAuthPKCS11SoftHSME2E$$' -count=1 -timeout=7m

.PHONY: test-e2e-provider-certauth-sources-openbao-ci
test-e2e-provider-certauth-sources-openbao-ci: verify-e2e-manifest ## Run supported provider cert-auth source E2E lanes.
	@$(MAKE) test-e2e-provider-certauth-pkcs11-openbao-ci

.PHONY: test-e2e-provider-upgrade-rollback-openbao-ci
test-e2e-provider-upgrade-rollback-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_OLD_IMAGE)" VERSION="$(VERSION)-e2e-old" COMMIT="$(COMMIT)-old"; \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_NEW_IMAGE)" VERSION="$(VERSION)-e2e-new" COMMIT="$(COMMIT)-new"; \
	fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_OLD_IMAGE="$(E2E_PROVIDER_OLD_IMAGE)" E2E_PROVIDER_NEW_IMAGE="$(E2E_PROVIDER_NEW_IMAGE)" "$(GO)" test -v -tags=e2e ./test/e2e -run '^TestProviderBinaryUpgradeRollbackE2E$$' -count=1 -timeout=8m

define kind-e2e-target
.PHONY: $(1)
$(1): verify-e2e-manifest
	@if [ "$$(E2E_PROVIDER_BUILD)" != "false" ]; then $$(MAKE) image IMAGE="$$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_KIND_CI=true E2E_OPENBAO_IMAGE="$$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$$(E2E_PROVIDER_IMAGE)" E2E_KIND_NODE_IMAGE="$$(E2E_KIND_NODE_IMAGE)" "$$(GO)" test -v -tags=e2e ./test/e2e -run '$(2)' -count=1 -timeout=$(3)
endef

$(eval $(call kind-e2e-target,test-e2e-kind-smoke,^TestKindKMSV2SmokeE2E$$$$,30m))
$(eval $(call kind-e2e-target,test-e2e-kind-convergence,^TestKindMultiControlPlaneConvergenceE2E$$$$,45m))
$(eval $(call kind-e2e-target,test-e2e-kind-upgrade-rollback,^TestKindStaticPodUpgradeRollbackE2E$$$$,30m))
$(eval $(call kind-e2e-target,test-e2e-kind-dr-runbook,^TestKindDRRestoreRunbookE2E$$$$,35m))

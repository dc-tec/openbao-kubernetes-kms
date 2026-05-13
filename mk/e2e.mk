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

.PHONY: test-e2e-openbao
test-e2e-openbao: test-e2e-openbao-ci

.PHONY: test-e2e-openbao-ci
test-e2e-openbao-ci: verify-e2e-manifest ## Run the default OpenBao CI E2E lane.
	@if command -v "$(GINKGO)" >/dev/null 2>&1; then \
		E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" "$(MAKE)" test-e2e E2E_LABEL_FILTER='openbao && transit && ci' E2E_TIMEOUT=6m; \
	else \
		E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestE2E$$' -count=1; \
	fi
	@$(MAKE) test-e2e-provider-openbao-ci

define provider-e2e-target
.PHONY: $(1)
$(1): verify-e2e-manifest
	@if [ "$$(E2E_PROVIDER_BUILD)" != "false" ]; then $$(MAKE) image IMAGE="$$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$$(E2E_PROVIDER_IMAGE)" "$$(GO)" test -tags=e2e ./test/e2e -run '$(2)' -count=1 -timeout=$(3)
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
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestE2E$$' -count=1 -timeout=6m -ginkgo.label-filter='openbao && certauth && ci'

.PHONY: test-e2e-provider-upgrade-rollback-openbao-ci
test-e2e-provider-upgrade-rollback-openbao-ci: verify-e2e-manifest
	@if [ "$(E2E_PROVIDER_BUILD)" != "false" ]; then \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_OLD_IMAGE)" VERSION="$(VERSION)-e2e-old" COMMIT="$(COMMIT)-old"; \
		$(MAKE) image IMAGE="$(E2E_PROVIDER_NEW_IMAGE)" VERSION="$(VERSION)-e2e-new" COMMIT="$(COMMIT)-new"; \
	fi
	@E2E_OPENBAO_CI=true E2E_OPENBAO_IMAGE="$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_OLD_IMAGE="$(E2E_PROVIDER_OLD_IMAGE)" E2E_PROVIDER_NEW_IMAGE="$(E2E_PROVIDER_NEW_IMAGE)" "$(GO)" test -tags=e2e ./test/e2e -run '^TestProviderBinaryUpgradeRollbackE2E$$' -count=1 -timeout=8m

define kind-e2e-target
.PHONY: $(1)
$(1): verify-e2e-manifest
	@if [ "$$(E2E_PROVIDER_BUILD)" != "false" ]; then $$(MAKE) image IMAGE="$$(E2E_PROVIDER_IMAGE)"; fi
	@E2E_KIND_CI=true E2E_OPENBAO_IMAGE="$$(E2E_OPENBAO_IMAGE)" E2E_PROVIDER_IMAGE="$$(E2E_PROVIDER_IMAGE)" E2E_KIND_NODE_IMAGE="$$(E2E_KIND_NODE_IMAGE)" "$$(GO)" test -tags=e2e ./test/e2e -run '$(2)' -count=1 -timeout=$(3)
endef

$(eval $(call kind-e2e-target,test-e2e-kind-smoke,^TestKindKMSV2SmokeE2E$$$$,30m))
$(eval $(call kind-e2e-target,test-e2e-kind-convergence,^TestKindMultiControlPlaneConvergenceE2E$$$$,45m))
$(eval $(call kind-e2e-target,test-e2e-kind-upgrade-rollback,^TestKindStaticPodUpgradeRollbackE2E$$$$,30m))
$(eval $(call kind-e2e-target,test-e2e-kind-dr-runbook,^TestKindDRRestoreRunbookE2E$$$$,35m))

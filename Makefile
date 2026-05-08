SHELL := /bin/sh

.PHONY: help bootstrap fmt lint test ci-core docs-check versions-check lint-ast test-ast semgrep-rules-test semgrep-scan semgrep-ci

GO_VERSION := $(shell cat .go-version)
AST_GREP ?= .github/tools/node_modules/.bin/ast-grep
SEMGREP ?= semgrep
SEMGREP_CONFIG_FLAGS ?= --config .semgrep/rules
SEMGREP_TARGETS ?= cmd internal
SEMGREP_ARTIFACT_DIR ?= dist/semgrep
SEMGREP_OUTPUT_JSON ?= $(SEMGREP_ARTIFACT_DIR)/semgrep.json

help:
	@printf '%s\n' 'Targets:'
	@printf '%s\n' '  bootstrap       Prepare local development prerequisites once implementation exists'
	@printf '%s\n' '  fmt             Format Go sources when go.mod exists'
	@printf '%s\n' '  lint            Run lightweight lint checks'
	@printf '%s\n' '  lint-ast        Run ast-grep rules when ast-grep and Go code are present'
	@printf '%s\n' '  test            Run Go tests when go.mod exists'
	@printf '%s\n' '  ci-core         Run the local core quality gate'
	@printf '%s\n' '  docs-check      Check docs for known formatting artifacts'
	@printf '%s\n' '  semgrep-ci      Run Semgrep rule tests and blocking scan when semgrep is available'
	@printf '%s\n' '  versions-check  Check central version policy exists'

bootstrap:
	@printf 'Go toolchain: %s\n' '$(GO_VERSION)'
	@if command -v npm >/dev/null 2>&1; then \
		npm ci --prefix .github/tools; \
	else \
		printf '%s\n' 'npm not found; skipping ast-grep tool install.'; \
	fi
	@if [ ! -f go.mod ]; then \
		printf '%s\n' 'go.mod does not exist yet; run M0 module initialization first.'; \
	fi

fmt:
	@if [ -f go.mod ]; then gofmt -w $$(find . -name '*.go' -not -path './vendor/*'); else printf '%s\n' 'No go.mod yet; skipping Go formatting.'; fi

lint: docs-check versions-check test-ast lint-ast semgrep-ci
	@if [ -f go.mod ]; then go vet ./...; else printf '%s\n' 'No go.mod yet; skipping Go lint.'; fi

test:
	@if [ -f go.mod ]; then go test ./...; else printf '%s\n' 'No go.mod yet; skipping Go tests.'; fi

ci-core: lint test

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
	if [ -z "$$targets" ] || ! find $$targets \( -name '*.go' -o -name '*.yml' -o -name '*.yaml' \) 2>/dev/null | grep -q .; then \
		printf '%s\n' 'No Semgrep targets yet; skipping Semgrep scan.'; \
	elif command -v "$(SEMGREP)" >/dev/null 2>&1; then \
		"$(SEMGREP)" scan --metrics=off --no-git-ignore $(SEMGREP_CONFIG_FLAGS) $$targets; \
	else \
		printf '%s\n' 'semgrep not installed; cannot scan targets.'; \
		exit 1; \
	fi

semgrep-ci: semgrep-rules-test
	@targets=""; for d in $(SEMGREP_TARGETS); do [ -e "$$d" ] && targets="$$targets $$d"; done; \
	if [ -z "$$targets" ] || ! find $$targets \( -name '*.go' -o -name '*.yml' -o -name '*.yaml' \) 2>/dev/null | grep -q .; then \
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

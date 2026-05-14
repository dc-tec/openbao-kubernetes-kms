##@ Documentation

.PHONY: docs-check
docs-check: ## Check docs for known formatting artifacts.
	@! grep -R -n --exclude-dir=_archive $$(printf '\357\277\274') README.md docs
	@! grep -R -n --exclude-dir=_archive '⸻' README.md docs
	@! grep -R -n --exclude-dir=_archive 'openbao-kms-provider' README.md docs
	@! grep -R -n --exclude-dir=_archive '—' README.md docs

.PHONY: docs-deps
docs-deps: ## Install the pinned Hugo binary locally.
	@GOFLAGS="-mod=mod" "$(GO)" install github.com/gohugoio/hugo@$(HUGO_VERSION)

.PHONY: docs-build
docs-build: ## Build the Hugo docs site into public/.
	@$(HUGO_RUN) --source . --baseURL "$(DOCS_BASE_URL)" --destination "$(DOCS_OUT)" --cleanDestinationDir --gc --minify

.PHONY: docs-serve
docs-serve: ## Serve the docs site locally on http://localhost:1313/.
	@$(HUGO_RUN) server --source . --baseURL http://localhost:1313/

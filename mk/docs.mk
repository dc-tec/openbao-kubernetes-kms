##@ Documentation

DOCS_PROSE_PATHS := \
	README.md \
	SECURITY.md \
	CONTRIBUTING.md \
	':(glob)docs/**/*.md' \
	':(glob)deploy/**/*.md' \
	':(glob)test/**/*.md' \
	hack/harvester/README.md \
	':(glob)website/content/**/*.md' \
	website/layouts/index.html \
	website/layouts/_default/list.html \
	website/layouts/search/single.html \
	':(glob).github/ISSUE_TEMPLATE/*.md' \
	.github/PULL_REQUEST_TEMPLATE.md

.PHONY: docs-check
docs-check: ## Check docs for known formatting artifacts.
	@status=0; git grep -n -I -e "$$(printf '\357\277\274')" -- $(DOCS_PROSE_PATHS) || status=$$?; test "$$status" -eq 1
	@status=0; git grep -n -I -e '⸻' -- $(DOCS_PROSE_PATHS) || status=$$?; test "$$status" -eq 1
	@status=0; git grep -n -I -e 'openbao-kms-provider' -- $(DOCS_PROSE_PATHS) || status=$$?; test "$$status" -eq 1
	@status=0; git grep -n -I -e '—' -- $(DOCS_PROSE_PATHS) || status=$$?; test "$$status" -eq 1

.PHONY: docs-deps
docs-deps: ## Install the pinned Hugo binary locally.
	@GOFLAGS="-mod=mod" "$(GO)" install github.com/gohugoio/hugo@$(HUGO_VERSION)

.PHONY: docs-build
docs-build: ## Build the Hugo docs site into public/.
	@$(HUGO_RUN) --source . --baseURL "$(DOCS_BASE_URL)" --destination "$(DOCS_OUT)" --cleanDestinationDir --gc --minify

.PHONY: docs-serve
docs-serve: ## Serve the docs site locally on http://localhost:1313/.
	@$(HUGO_RUN) server --source . --baseURL http://localhost:1313/

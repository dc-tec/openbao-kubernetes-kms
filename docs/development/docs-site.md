---
title: "Docs Site"
description: "How the Hugo documentation site is structured, mounted, built, and published."
weight: 80
---

# Docs Site

This page documents how the published Hugo site is structured and built. For the writing rules and IA contracts that apply to documentation changes see [Docs Style Guide](/development/docs-style-guide/).

## Source Layout

The repository keeps documentation source under `docs/` so it stays close to the code it describes. The Hugo site assembles those sources through mounts declared in `hugo.toml`:

```text
docs/
  getting-started/    operator first-success path
  deployment/         systemd vs static-pod, identity model
  operations/         day-2 runbooks
  reference/          exhaustive lookup
  security/           threat model, hardening, auth, AAD
  architecture/       maintainer-facing rationale
  development/        contributor-facing
website/
  content/            homepage, error pages, search
  layouts/            Hugo templates
  assets/             CSS and JS sources
  static/             brand assets, fonts, vendored mermaid
hugo.toml             site config and module mounts
```

Historical planning material is not carried in the live documentation tree. Use repository history when older design notes or implementation planning context are needed.

## Mount Configuration

`hugo.toml` mounts each section directory under `content/<section>` so Hugo treats them as native sections without symlinking:

```toml
[[module.mounts]]
  source = "website/content"
  target = "content"

[[module.mounts]]
  source = "docs/getting-started"
  target = "content/getting-started"
```

Each section has its own mount entry. Adding a new section requires:

1. creating `docs/<section>/_index.md` with `title`, `description`, `weight`, and `browse`,
2. adding a mount entry in `hugo.toml`,
3. linking the new section from `website/layouts/index.html` if it should appear on the homepage navigation.

## Building Locally

Hugo runs through `go run` so a separate Hugo install is not required. The Makefile encapsulates the version pin and the standard targets:

```sh
make docs-deps    # install pinned Hugo into GOBIN once
make docs-build   # build into public/ with --cleanDestinationDir --gc --minify
make docs-serve   # serve locally on http://localhost:1313/
```

The Hugo version is pinned in the Makefile via `HUGO_VERSION` and matches the version used by CI. Bump it in lockstep with any layout or shortcode changes that depend on Hugo behavior.

## Verification

Two Make targets gate every documentation change:

```sh
make docs-build
make docs-check
```

`make docs-build` runs the full Hugo build and must complete without warnings. `make docs-check` enforces forbidden-string and em-dash gates against `docs/`. The current rules:

- no object replacement character (octal `\357\277\274`) in `docs/` or `README.md`,
- no three-em dash (`U+2E3B`) in `docs/` or `README.md`,
- no older long-form name variants in `docs/` or `README.md` (the binary is `bao-kms-provider`; see [Docs Style Guide](/development/docs-style-guide/)),
- no em-dash (`U+2014`) in `docs/` or `README.md`.

For the writing rules these gates enforce see [Docs Style Guide: Voice And Prose Rules](/development/docs-style-guide/#voice-and-prose-rules).

## Layout Templates

`website/layouts/` holds the Hugo templates:

```text
website/layouts/
  index.html              homepage with hero, workflow, section cards
  _default/baseof.html    base template for all pages
  _default/list.html      section landing template
  _default/single.html    leaf page template
  _default/error.html     error page template
  _markup/render-link.html        custom link rendering
  _markup/render-codeblock-mermaid.html  mermaid block rendering
  partials/site_head.html
  partials/site_header.html
  partials/site_nav.html
  partials/site_nav_tree.html
  partials/site_toc.html
  partials/site_footer.html
  partials/page_title.html
  partials/page_description.html
  partials/mermaid_script.html
  search/single.html
```

The homepage template hardcodes the workflow ladder (Check Fit, Set Up OpenBao, Install And Wire, Verify End-To-End, Operate Safely). When the operator workflow changes, update `website/layouts/index.html` along with the corresponding section pages.

## Mermaid

Mermaid diagrams render through a custom `_markup/render-codeblock-mermaid.html` shortcode plus a small initialization script in `website/assets/js/mermaid-init.js`. Author diagrams as fenced code blocks tagged `mermaid` in markdown; no shortcode invocation is needed.

The mermaid library is vendored at `website/static/vendor/mermaid/mermaid.min.js` so the published site does not depend on a CDN.

## Publishing

The site is published to GitHub Pages. The deploy pipeline uses the same `make docs-build` target with the `DOCS_BASE_URL` variable set to the public URL. The deploy step is intentionally separate from the build step so a faulty build never replaces the live site.

A rendered build of the site lives in `public/` after `make docs-build`. The directory is gitignored.

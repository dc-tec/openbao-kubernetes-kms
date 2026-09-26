---
title: Docs style guide
description: "The writing, structure, verification, and presentation standard for the published documentation."
eyebrow: Contribute
weight: 70
---

The documentation describes supported behavior for people who deploy, secure,
and operate `bao-kms-provider`. Write for the reader's task, and verify every
product claim against the code, samples, or release it describes.

This standard follows the OpenBao Operator documentation standard and Google's
guidance for [clear technical writing](https://developers.google.com/tech-writing/one),
[procedures](https://developers.google.com/style/procedures),
[headings](https://developers.google.com/style/headings),
[voice](https://developers.google.com/style/voice),
[person](https://developers.google.com/style/person), and
[prescriptive language](https://developers.google.com/style/prescriptive-documentation).

## Organize documentation by task

Each section owns one kind of work:

| Section | Owns | Primary reader |
|---|---|---|
| `get-started/` | The path from choosing a deployment model to verified encryption. | First-time operator |
| `configure/` | Variants of the default setup: auth sources, policy, monitoring. | Operator adapting the default path |
| `operate/` | Rotation, upgrade, recovery, and troubleshooting of a running provider. | Operator maintaining a deployment |
| `security/` | Trust boundaries, controls, host identity, and their limits. | Security reviewer or platform owner |
| `reference/` | Exact CLI, configuration, protocol, observability, and lifecycle contracts. | Operator or maintainer looking up behavior |
| `architecture/` | Durable design rationale and tradeoffs. | Maintainer or reviewer |
| `contribute/` | Local workflow, CI, tests, release process, and docs maintenance. Published at `/contribute/`, outside the operator task flow. | Contributor or reviewer |

Add a page only when it gives a task or contract a clear home. Extend an
existing page when a new page would repeat prerequisites, warnings, or
explanations that page already owns. When a page starts serving two readers,
move each part to the section where that reader would look.

## Write directly

- State the outcome before background or rationale.
- Use active voice, second person, present tense, and direct verbs.
- Use sentence case for titles and headings. Keep product names, acronyms, and
  identifiers such as `key_id` as written.
- Start task headings with a base-form verb, such as "Rotate the Transit key".
  Use noun phrases for concept and reference headings, such as "Failure modes".
- Keep one idea in each sentence and one topic in each paragraph.
- Put prerequisites before the procedure.
- Present one recommended path first. Put genuine alternatives in a separate
  section.
- Use `must` for requirements, `can` for permission or capability, and `might`
  for possibility.
- Use OpenBao terminology. Mention Vault only for related work or compatibility
  context.
- Use the binary name `bao-kms-provider` in prose.
- Prefer "tested matrix", "preview release", and "verified artifact" in
  operator-facing pages.

Avoid:

- slogans, marketing language, and commentary about the page itself,
- filler openers such as "It is important to note",
- words that understate operational cost, such as "simply", "just", or
  "obviously",
- formulaic contrast such as "not X, but Y" when two plain sentences are
  clearer,
- internal shorthand such as "release gate", "support claim", or "evidence
  bundle" in operator-facing pages,
- implementation history that does not affect the current contract.

The docs check rejects em dash characters in tracked first-party prose. Use a
comma, period, parentheses, or rewrite the sentence.

## Say it once

- State each caveat, version matrix, or list of values on the page that owns
  it, and link to that page elsewhere. Support caveats belong to
  [Compatibility](/docs/reference/compatibility/), identity-bearing values to
  [Plan identity values](/docs/get-started/plan-values/), and incident rules to
  [Disaster recovery](/docs/operate/disaster-recovery/#during-an-incident).
- Do not open a page by restating its description. The template already shows
  it as the lede.
- Use at most one "see also" sentence at the top of a page.
- Do not end pages with "Read next" or "Use another section if" lists. The
  sidebar, section pages, and Previous and Next links cover navigation.
- Drop plans for future releases unless they change the current contract.
- Aim for about 800 words on task and runbook pages and under 150 words on
  section landing pages. Reference pages can be as long as the contract
  requires, without narrative.

## Write complete procedures

Use numbered steps for ordered work. Start each step with an imperative and
keep one primary action in each step. Introduce each command with the action
and its expected effect, explain placeholders before the reader copies the
command, and state the observable result afterward.

Examples must be complete for the task they claim to perform. Label partial
configuration, policy, or manifest examples as fragments. Do not present a
passing configuration parse, a single health endpoint, or one metric as proof
that authentication, encryption, or recovery works end to end.

Use explicit placeholders such as `<cluster-id>` and `<key-name>`. Keep shell
examples safe to paste after substitution. Avoid commands that overwrite live
configuration, expose credentials, or imply that a destructive operation is
reversible.

## Preserve operational boundaries

Keep requirements and warnings that protect security, data, availability, or
access. A shorter page must not hide:

- trust roots, credentials, or auth material custody,
- destructive or irreversible effects, such as Transit key settings OpenBao
  cannot revert,
- identity-bearing values that must not change after encryption begins,
- compatibility and version constraints,
- required network paths and external dependencies, such as a JWT issuer that
  runs independently of the protected API server,
- backup, restore, and rollback prerequisites,
- the difference between a started provider, a passing `doctor` run, and
  verified encryption in etcd.

Use a warning callout only when ignoring it can cause material harm. Use a
note for scope, ownership, limitations, and other context. Do not use callouts
as decoration.

## Verify product claims

Behavioral pages list the narrowest authoritative repository paths in the
`verifiedBy` front matter field. Choose evidence in this order:

1. Configuration fields and defaults: `internal/config` types, validation, and
   the configuration schema.
2. Commands and flags: `cmd/bao-kms-provider`.
3. Runtime behavior: internal packages, unit tests, integration tests, and
   end-to-end tests under `test/`.
4. Deployment examples: `deploy/` samples, packaging inputs, and release
   artifacts.
5. Compatibility and release claims: `.ci/versions.yaml`, release tags, and
   release automation.

Do not copy a claim because an older page contained it. When code and
documentation disagree, document the supported runtime behavior or resolve the
product contract before publishing. Hugo does not render `verifiedBy`; it
exists for review and maintenance.

Keep tested snippets tested. `test/deployment/systemd-install.sh` extracts and
runs the fenced `sh` block after each marker comment, such as
`<!-- systemd-bundle-install -->`, in the install guide. Keep those blocks as
plain Markdown fences and change the matching bundle README in the same commit.

## Write front matter

```yaml
---
title: Rotate the Transit key
description: State the result and the important boundary in one sentence.
eyebrow: Operate · Key lifecycle
weight: 10
verifiedBy:
  - internal/keyregistry
  - test/e2e/provider_rotation_test.go
---
```

- `title` matches the page's entry in `website/data/navigation.yaml`.
- `description` renders as the page lede and in search results.
- `eyebrow` names the section and, where useful, a topic or the Get started
  step, such as `Get started · Step 4`.
- `weight` orders the page on its section landing page.

Do not repeat the title as a `#` heading in the body. The page template renders
`title` as the only H1, so the body starts with the first paragraph or `##`
section.

## Use the smallest useful presentation

Prefer Markdown, short tables, and ordinary links. Use shortcodes only when
their meaning improves scanning:

- `callout` with `type` set to `warning`, `note`, or `tip` for a bounded
  warning or note,
- `checklist` for an exit or readiness checklist,
- `command` for a titled command sequence.

Plain fenced code blocks already render as copyable blocks. Give every fence a
language, such as `sh`, `yaml`, or `text`.

Use Mermaid when a sequence, dependency, boundary, or decision tree is clearer
as a diagram than as prose. Keep diagrams small enough to read on the
published site.

## Link and navigate

Link to the page that owns a topic instead of repeating its detail.

- Use absolute site paths, such as `/docs/operate/rotation/` or
  `/contribute/testing/`. Do not link to `.md` files from published pages.
- Use the target page's title as link text, optionally prefixed with its
  section, such as "Operate: Rotation".
- When you change a heading that other pages link to, update the fragment links
  in the same change.

Add every new page to `website/data/navigation.yaml`. Items with a `step`
field form the Previous and Next path of their group; items that share a step
are alternatives. When a page moves or is removed, add its old route to
`website/data/redirects.yaml`. See [Docs site](/contribute/docs-site/) for
both mechanisms.

## Definition of done

A documentation change is complete when:

- the page has one clear task or contract,
- behavioral claims cite current `verifiedBy` evidence,
- examples are complete, safe, and match the configuration schema and CLI,
- security, data, availability, access, and compatibility boundaries remain
  explicit,
- titles, navigation, links, anchors, and search work in the rendered site,
- `make docs-check`, `make docs-build`, and the checks for any code or samples
  the page describes pass.

`make docs-check` scans tracked prose for configured text and typography
issues. `make docs-build` fails on any Hugo warning, then checks the rendered
site for broken links, missing fragments, and navigation gaps.

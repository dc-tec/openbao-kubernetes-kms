---
title: "Docs Style Guide"
description: "Writing, structure, linking, and verification guidance for the published documentation."
weight: 70
---

# Docs Style Guide

The documentation should help operators make safe decisions without having to
read the source code first. Contributor and architecture pages can go deeper,
but public workflow pages should stay practical and easy to follow.

## Audience

Write each page for the reader who is most likely to use it:

| Section | Primary reader | Page style |
|---|---|---|
| `getting-started/` | First-time operator | Sequential tutorial with one recommended path. |
| `deployment/` | Operator choosing or applying a runtime model | Model comparison, then concrete setup. |
| `operations/` | Operator maintaining a deployed provider | Task runbooks with checks and recovery notes. |
| `reference/` | Operator or maintainer looking up exact behavior | Precise lookup material. |
| `security/` | Security reviewer or platform owner | Trust boundaries, controls, and limitations. |
| `architecture/` | Maintainer or reviewer | Design rationale and tradeoffs. |
| `development/` | Contributor or reviewer | Local workflow, CI, tests, release process, and docs maintenance. |

If a page starts serving two different readers, split it or move part of the
content to the section where that reader would naturally look.

## Voice

Prefer direct, concrete prose:

- Say what the provider does, what the operator does, and what success looks
  like.
- Use short paragraphs and explicit headings.
- Keep warnings tied to a real operational consequence.
- Prefer "tested matrix", "preview release", and "verified artifact" in
  user-facing docs.
- Keep detailed CI and release mechanics in `development/` unless operators
  need them for installation or verification.
- Use OpenBao terminology. Mention Vault only for related work or compatibility
  context.
- Use the binary name `bao-kms-provider` in prose.

Avoid:

- marketing language,
- filler openers such as "It is important to note",
- words that understate operational cost, such as "simply", "just", or
  "obviously",
- formulaic contrast such as "not X, but Y" when two plain sentences would be
  clearer,
- internal shorthand such as "release gate", "support claim", or "evidence
  bundle" in operator-facing pages.

The docs check rejects em dash characters in tracked first-party prose. Use a
comma, period, parentheses, or rewrite the sentence.

## Page Structure

Use the shape that matches the section:

- Getting-started pages should have a clear beginning, ordered steps, and a
  visible end state.
- Deployment pages should separate model selection from model-specific setup.
- Operations pages can branch by symptom or condition, but should keep recovery
  steps ordered.
- Reference pages should be stable lookup material, not narrative.
- Security pages should describe scope, trust, controls, and limits.
- Architecture pages can explain why the system works the way it does.
- Development pages can include contributor-only details and CI mechanics.

## Links

Link to the canonical page for a topic instead of repeating the same detail in
several places.

- Use absolute site paths such as `/operations/rotation/`.
- Avoid `.md` links from published docs.
- Section landing pages should help readers move to the right section if they
  arrived in the wrong place.
- When changing a heading that other pages link to, update the fragment links in
  the same change.

## Front Matter

Every page needs:

```yaml
title: "Page Title"
description: "Short description used by search and previews."
weight: 10
```

Section landing pages also use `browse` to define the intended order:

```yaml
browse:
  - "/getting-started/overview"
  - "/getting-started/openbao-setup"
```

## Diagrams

Use Mermaid when a sequence, dependency, boundary, or decision tree is clearer
as a diagram than as prose. Keep diagrams small enough to read on the published
site.

## Verification

Before merging a docs change, run:

```sh
make docs-check
make docs-build
```

`make docs-check` catches configured text and typography checks. `make
docs-build` runs the Hugo build and should complete without warnings.

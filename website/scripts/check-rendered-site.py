#!/usr/bin/env python3
"""Validate the generated Hugo site without browser or npm dependencies."""

from __future__ import annotations

import json
import re
import sys
from collections import Counter
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urljoin, urlparse


# Set from the site base URL in main().
BASE_PATH = ""
PUBLIC_HOST = ""
MOJIBAKE_MARKERS = ("Ã", "â€", "â€™", "â€œ", "â€˜", "ï¿½", "�")


class Document(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.ids: list[str] = []
        self.hrefs: list[str] = []
        self.text: list[str] = []
        self.content_text: list[str] = []
        self._content_div_depth = 0
        self.command_blocks: list[str] = []
        self.canonical: str | None = None
        self.is_redirect = False
        self.search_indexes: list[str] = []
        self.sidebar_hrefs: list[str] = []
        self.navigation_current_hrefs: list[str] = []
        self._navigation_depth = 0
        self._in_sidebar_navigation = False
        self._command_div_depth = 0
        self._command_code: list[str] | None = None

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        values = dict(attrs)
        if values.get("id"):
            self.ids.append(values["id"] or "")
        if values.get("href"):
            self.hrefs.append(values["href"] or "")

        classes = (values.get("class") or "").split()
        if values.get("data-search-index"):
            self.search_indexes.append(values["data-search-index"] or "")
        if tag == "link" and values.get("rel") == "canonical":
            self.canonical = values.get("href")
        if tag == "meta" and (values.get("http-equiv") or "").lower() == "refresh":
            self.is_redirect = True
        if tag == "nav":
            if self._navigation_depth:
                self._navigation_depth += 1
            elif values.get("aria-label") == "Documentation" or "section-nav" in classes:
                self._navigation_depth = 1
                self._in_sidebar_navigation = values.get("aria-label") == "Documentation"
        if tag == "a" and values.get("href") and self._navigation_depth:
            if self._in_sidebar_navigation:
                self.sidebar_hrefs.append(values["href"] or "")
            if values.get("aria-current") == "page":
                self.navigation_current_hrefs.append(values["href"] or "")
        if tag == "div":
            if self._content_div_depth > 0:
                self._content_div_depth += 1
            elif "article-content" in classes:
                self._content_div_depth = 1
            if self._command_div_depth > 0:
                self._command_div_depth += 1
            elif "command-block" in classes:
                self._command_div_depth = 1
        elif tag == "code" and self._command_div_depth > 0:
            self._command_code = []

    def handle_data(self, data: str) -> None:
        self.text.append(data)
        if self._content_div_depth:
            self.content_text.append(data)
        if self._command_code is not None:
            self._command_code.append(data)

    def handle_endtag(self, tag: str) -> None:
        if tag == "div" and self._content_div_depth:
            self._content_div_depth -= 1
        if tag == "nav" and self._navigation_depth:
            self._navigation_depth -= 1
            if not self._navigation_depth:
                self._in_sidebar_navigation = False
        if tag == "code" and self._command_code is not None:
            self.command_blocks.append("".join(self._command_code))
            self._command_code = None
        elif tag == "div" and self._command_div_depth > 0:
            self._command_div_depth -= 1


def yaml_line_opens_block(stripped: str) -> bool:
    if stripped.startswith("- "):
        return True
    without_comment = stripped.split(" #", 1)[0].rstrip()
    return re.search(r":\s*(?:[|>][-+0-9]*)?$", without_comment) is not None


def validate_kubernetes_yaml(code: str) -> str | None:
    """Return a rendering or indentation error for a Kubernetes YAML block."""
    documents: list[list[str]] = [[]]
    for line in code.splitlines():
        if line.strip() == "---":
            documents.append([])
        else:
            documents[-1].append(line.rstrip())

    for document in documents:
        significant = [line for line in document if line.strip() and not line.lstrip().startswith("#")]
        if not significant or not significant[0].startswith("apiVersion:"):
            continue

        top_level = {line.split(":", 1)[0] for line in significant if line == line.lstrip() and ":" in line}
        # Config kinds such as EncryptionConfiguration carry no metadata.
        missing = {"apiVersion", "kind"} - top_level
        if missing:
            return f"top-level fields are indented or missing: {', '.join(sorted(missing))}"

        previous_indent = 0
        previous_stripped = significant[0].strip()
        for line_number, line in enumerate(significant[1:], start=2):
            indent = len(line) - len(line.lstrip(" "))
            if indent > previous_indent and not yaml_line_opens_block(previous_stripped):
                return f"line {line_number} is nested below a scalar value: {line.strip()!r}"
            previous_indent = indent
            previous_stripped = line.strip()

    return None


def route_for(root: Path, html_file: Path) -> str:
    relative = html_file.relative_to(root).as_posix()
    if relative == "index.html":
        return "/"
    if relative.endswith("/index.html"):
        return f"/{relative.removesuffix('index.html')}"
    if relative.endswith(".html"):
        return f"/{relative.removesuffix('.html')}/"
    raise ValueError(f"unexpected HTML path: {html_file}")


def load_document(path: Path, cache: dict[Path, Document]) -> Document:
    if path not in cache:
        document = Document()
        document.feed(path.read_text(encoding="utf-8"))
        cache[path] = document
    return cache[path]


def normalize_internal_path(current_route: str, href: str) -> tuple[str, str] | None:
    parsed = urlparse(href)
    if parsed.scheme in {"mailto", "tel", "javascript"}:
        return None
    if parsed.netloc and parsed.netloc != PUBLIC_HOST:
        return None

    path = parsed.path
    if parsed.netloc == PUBLIC_HOST:
        if path != BASE_PATH and not path.startswith(f"{BASE_PATH}/"):
            return None

    if path == BASE_PATH or path.startswith(f"{BASE_PATH}/"):
        path = path.removeprefix(BASE_PATH)

    if not path:
        route = current_route
    elif path.startswith("/"):
        route = path
    else:
        route = urlparse(urljoin(f"https://local{current_route}", path)).path

    if not route.endswith("/") and not Path(route).suffix:
        route += "/"

    return route, unquote(parsed.fragment)


def validate_navigation(routes: dict[str, Path], documents: dict[Path, Document]) -> list[str]:
    """Require canonical guides to be reachable from their navigation."""
    errors: list[str] = []
    sidebar_targets: set[str] = set()
    global_guides: list[str] = []
    for route, html_file in routes.items():
        document = load_document(html_file, documents)
        if document.is_redirect or not document.canonical:
            continue
        if normalize_internal_path(route, document.canonical) != (route, ""):
            continue

        is_docs = "/docs/" in route
        is_global_guide = route.startswith(("/project/", "/contribute/")) and route.count("/") > 2
        if is_global_guide:
            global_guides.append(route)
        current_targets = {
            normalize_internal_path(route, href) for href in document.navigation_current_hrefs
        }
        if (is_docs and not route.endswith("/docs/")) or is_global_guide:
            if (route, "") not in current_targets:
                errors.append(f"missing current page in navigation: {route}")

        docs_prefix = route.split("/docs/", 1)[0] + "/docs/"
        for href in document.sidebar_hrefs:
            target = normalize_internal_path(route, href)
            if target is None:
                continue
            target_route, _ = target
            sidebar_targets.add(target_route)
            if is_docs and "/docs/" in target_route and not target_route.startswith(docs_prefix):
                errors.append(f"sidebar crosses documentation versions: {route}: {href}")

    for route in global_guides:
        if route not in sidebar_targets:
            errors.append(f"missing global guide in sidebar: {route}")
    return errors


def validate_version_contract(
    root: Path, routes: dict[str, Path], documents: dict[Path, Document]
) -> list[str]:
    """Keep published-release instructions separate from unreleased behavior."""
    errors: list[str] = []
    for route, html_file in routes.items():
        document = load_document(html_file, documents)
        if document.is_redirect or "/docs/" not in route:
            continue
        is_next = route.startswith("/next/")
        text = "".join(document.text)
        expected_notice = "Next · Unreleased" if is_next else "Documentation for 0.1.0-preview.3"
        if expected_notice not in text:
            errors.append(f"missing version notice: {route}")
        if not is_next:
            for marker in ("private draft", "private snapshot", "tagged artifacts pending", "artifact acceptance remains pending"):
                if marker in text.casefold():
                    errors.append(f"unpublished release marker: {route}: {marker}")
        expected_index = "/next/index.json" if is_next else "/index.json"
        if document.search_indexes != [BASE_PATH + expected_index]:
            errors.append(f"wrong version search index: {route}")

    for route, has_init in (("/docs/reference/cli/", True), ("/next/docs/reference/cli/", True)):
        if route not in routes:
            errors.append(f"missing version CLI: {route}")
            continue
        document = load_document(routes[route], documents)
        if ("init" in document.ids) != has_init:
            errors.append(f"incorrect init availability: {route}")

    canonical_guides = (
        "docs/configure/oauth2/",
        "docs/configure/keycloak/",
        "docs/get-started/",
        "docs/get-started/before-you-begin/",
        "docs/get-started/download/",
        "docs/get-started/plan-values/",
        "docs/security/verify-release-artifacts/",
        "docs/reference/cli/",
        "docs/reference/configuration/",
        "docs/reference/kms-v2-contract/",
    )
    for prefix in ("/", "/next/"):
        for guide in canonical_guides:
            route = prefix + guide
            if route not in routes:
                errors.append(f"missing canonical version guide: {route}")
                continue
            document = load_document(routes[route], documents)
            if document.is_redirect or normalize_internal_path(route, document.canonical or "") != (route, ""):
                errors.append(f"version guide redirects instead of rendering: {route}")

    release_contracts = {
        "/docs/reference/compatibility/": (
            "fresh disposable installation", "new Transit key", "provider identity",
            "In-place upgrades from earlier previews are unsupported",
            "without rewriting its state or checkpoint", "no automatic adoption or migration command",
            "Preview.3 qualification scope", "Published preview.3 matrix", "Historical preview.2 matrix",
            "20 October 2026", "passed all 13 jobs", "passed all 15 jobs",
            "1.34.11", "1.35.8", "1.36.4", "1.37.0", "2.6.3", "2.7.0",
            "do not repeat every cross-pair", "three API servers",
            "CLI artifacts", "distribution targets", "not native execution qualification",
            "Linux arm64 checks use QEMU", "provider service runs only on Linux",
            "does not qualify an upgrade to preview.3 or a downgrade of preview.3 state",
        ),
        "/docs/get-started/": ("fresh disposable installation", "new Transit key", "provider identity"),
        "/docs/get-started/before-you-begin/": (
            "fresh disposable installation", "new Transit key", "provider identity",
            "protected Kubernetes API is down", "Linux control-plane nodes",
        ),
        "/docs/get-started/download/": (
            "fresh disposable installation", "new Transit key", "provider identity",
            "darwin", "macOS binary runs workstation commands only", "refuses to start outside Linux",
            "0.1.0-preview.3",
        ),
        "/docs/reference/configuration/": (
            "An omitted source selects", "only when", "jwtFile", "no", "oauth2",
            "Explicit empty values are invalid", "forbidden for",
        ),
        "/docs/configure/oauth2/": (
            "new access token before each OpenBao JWT login", "no automatic method fallback",
            "while the protected Kubernetes API is unavailable", "independent process",
            "existing OpenBao token usable", "New logins require the issuer",
            "cannot fall back to a JWT file or another authentication method",
            "does not revoke OpenBao tokens already issued", "client_secret_basic", "client_secret_post",
        ),
        "/docs/security/auth-model/": (
            "OpenBao token renewal does not acquire another OAuth token",
            "memory only", "TokenReview",
        ),
        "/docs/operate/upgrade/": (
            "requires a fresh disposable installation", "Do not use this procedure to upgrade an earlier preview",
            "Keep the old state, checkpoint, and key material intact",
        ),
        "/docs/operate/rotation/": (
            "retire-versions", "restore-versions", "--expected-state-hash", "Stop the provider",
            "every backup that needs the old versions has expired", "never restore one file",
        ),
        "/docs/operate/disaster-recovery/": (
            "registry state file and its adjacent checkpoint", "back up the two files together",
            "a Transit key recreated with the same name is a new key, not a recovery",
        ),
        "/docs/reference/release-lifecycle/": (
            "best effort", "production", "darwin", "PKCS#11",
        ),
        "/docs/errata/": (
            "Historical preview.2 corrections", "apply only to", "0.1.0-preview.2",
            "do not describe preview.3 installation behavior", "fresh disposable installation",
        ),
    }
    for route, required_phrases in release_contracts.items():
        if route not in routes:
            errors.append(f"missing release contract page: {route}")
            continue
        text = " ".join(" ".join(load_document(routes[route], documents).content_text).split()).casefold()
        for phrase in required_phrases:
            if phrase.casefold() not in text:
                errors.append(f"missing release contract: {route}: {phrase}")

    errata_route = "/docs/errata/"
    if errata_route in routes:
        historical_anchors = {
            "available-commands-and-authentication", "systemd-directory-permissions",
            "systemd-tarball-layout", "static-pod-diagnostic-binary",
            "fresh-evaluation-clusters", "diagnostic-scope",
        }
        actual = set(load_document(routes[errata_route], documents).ids)
        for anchor in sorted(historical_anchors - actual):
            errors.append(f"missing historical correction anchor: {errata_route}#{anchor}")

    for prefix in ("", "next/"):
        index_file = root / prefix / "index.json"
        if not index_file.is_file():
            errors.append(f"missing search index: {index_file}")
            continue
        for entry in json.loads(index_file.read_text(encoding="utf-8")):
            target = normalize_internal_path("/", entry["url"])
            if target is None:
                errors.append(f"external search entry: {entry['url']}")
                continue
            route, _ = target
            if route.startswith("/next/") != bool(prefix):
                errors.append(f"search crosses documentation versions: {prefix}: {route}")
            if route not in routes or load_document(routes[route], documents).is_redirect:
                errors.append(f"noncanonical search entry: {route}")
    return errors


def validate_redirects(routes: dict[str, Path], documents: dict[Path, Document]) -> list[str]:
    """Require internal redirects to resolve directly to canonical content."""
    errors: list[str] = []
    for route, html_file in routes.items():
        document = load_document(html_file, documents)
        if not document.is_redirect:
            continue
        target = normalize_internal_path(route, document.canonical or "")
        if target is None or target[0] not in routes:
            errors.append(f"redirect has no internal canonical target: {route}")
            continue
        target_route = target[0]
        seen = {route}
        while target_route in routes:
            if target_route in seen:
                errors.append(f"redirect cycle: {route} -> {target_route}")
                break
            seen.add(target_route)
            target_document = load_document(routes[target_route], documents)
            if not target_document.is_redirect:
                break
            errors.append(f"redirect chain: {route} -> {target_route}")
            next_target = normalize_internal_path(target_route, target_document.canonical or "")
            if next_target is None:
                break
            target_route = next_target[0]
    return errors


def main() -> int:
    global BASE_PATH, PUBLIC_HOST
    if len(sys.argv) != 3:
        print(f"usage: {Path(sys.argv[0]).name} PUBLIC_DIR BASE_URL", file=sys.stderr)
        return 2

    base_url = urlparse(sys.argv[2])
    BASE_PATH = base_url.path.rstrip("/")
    PUBLIC_HOST = base_url.netloc

    root = Path(sys.argv[1]).resolve()
    if not root.is_dir():
        print(f"rendered site not found: {root}", file=sys.stderr)
        return 1

    html_files = sorted(root.rglob("*.html"))
    routes = {route_for(root, path): path for path in html_files}
    documents: dict[Path, Document] = {}
    errors: list[str] = []
    for route, count in Counter(route_for(root, path) for path in html_files).items():
        if count > 1:
            errors.append(f"duplicate rendered route: {route} ({count})")

    required_routes = ("/", "/docs/", "/next/docs/", "/docs/errata/", "/next/docs/configure/oauth2/", "/404/")
    for route in required_routes:
        if route not in routes:
            errors.append(f"missing required route: {route}")

    for route, html_file in routes.items():
        document = load_document(html_file, documents)
        for element_id, count in Counter(document.ids).items():
            if count > 1:
                errors.append(f"duplicate id: {route}#{element_id} ({count})")

        text = "".join(document.text)
        for marker in MOJIBAKE_MARKERS:
            if marker in text:
                errors.append(f"encoding marker {marker!r}: {route}")

        for index, code in enumerate(document.command_blocks, start=1):
            yaml_error = validate_kubernetes_yaml(code)
            if yaml_error:
                errors.append(f"invalid rendered Kubernetes YAML: {route}: block {index}: {yaml_error}")

        for href in document.hrefs:
            try:
                target = normalize_internal_path(route, href)
            except ValueError as error:
                errors.append(f"invalid href: {route}: {href}: {error}")
                continue
            if target is None:
                continue

            target_route, fragment = target
            target_file = routes.get(target_route)
            if target_file is None:
                static_file = root / target_route.removeprefix("/")
                if not static_file.is_file():
                    errors.append(f"missing target: {route}: {href} -> {target_route}")
                continue

            if fragment:
                target_document = load_document(target_file, documents)
                if fragment not in target_document.ids:
                    errors.append(f"missing fragment: {route}: {href}")

    errors.extend(validate_redirects(routes, documents))
    errors.extend(validate_navigation(routes, documents))
    errors.extend(validate_version_contract(root, routes, documents))

    print(
        f"Rendered HTML: {len(html_files)}; "
        f"duplicate IDs: {sum(error.startswith('duplicate id:') for error in errors)}; "
        f"content, target, fragment, encoding, and navigation errors: "
        f"{sum(not error.startswith('duplicate id:') for error in errors)}"
    )
    if errors:
        print("\n".join(errors[:100]), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

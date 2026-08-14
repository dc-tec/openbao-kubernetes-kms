#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

fail() {
  printf 'ERR  %s\n' "$1" >&2
  exit 1
}

ok() {
  printf 'OK   %s\n' "$1"
}

manifest_version() {
  local key="$1"
  awk -v key="${key}" '$1 == key ":" { gsub(/"/, "", $2); print $2; exit }' .ci/versions.yaml
}

require_nix_command() {
  local name="$1"
  local command_path=""
  local resolved_path=""

  command_path="$(command -v "${name}" 2>/dev/null || true)"
  if [[ -z "${command_path}" ]]; then
    fail "${name} is missing from the devenv environment"
  fi

  resolved_path="$(realpath "${command_path}")"
  if [[ "${resolved_path}" != /nix/store/* ]]; then
    fail "${name} resolved outside the Nix store: ${resolved_path}"
  fi

  ok "${name}: ${resolved_path}"
}

if [[ -z "${DEVENV_ROOT:-}" ]]; then
  fail "DEVENV_ROOT is not set; run this check through 'devenv test' or 'devenv shell'"
fi

devenv_root="$(cd "${DEVENV_ROOT}" && pwd)"
if [[ "${devenv_root}" != "${ROOT_DIR}" ]]; then
  fail "DEVENV_ROOT points to ${devenv_root}, expected ${ROOT_DIR}"
fi

if [[ -z "${DEVENV_PROFILE:-}" ]]; then
  fail "DEVENV_PROFILE is not set inside devenv"
fi
if [[ "${PATH%%:*}" != "${DEVENV_PROFILE}/bin" ]]; then
  fail "the active devenv profile must take precedence in PATH"
fi
ok "active devenv profile takes precedence in PATH"

if [[ "${GOTOOLCHAIN:-}" != "local" ]]; then
  fail "GOTOOLCHAIN must be local inside devenv, found ${GOTOOLCHAIN:-unset}"
fi

if [[ -z "${GOPATH:-}" ]]; then
  fail "GOPATH is not set inside devenv"
fi
if [[ "${GOPATH}" == "${ROOT_DIR}" || "${GOPATH}" == "${ROOT_DIR}/"* ]]; then
  fail "GOPATH must stay outside the repository: ${GOPATH}"
fi
ok "GOPATH is outside the repository (${GOPATH})"

expected_go="$(tr -d '[:space:]' < .go-version)"
module_go="$(sed -n 's/^go //p' go.mod | head -n 1)"
manifest_go="$(manifest_version go)"
if [[ "${module_go}" != "${expected_go}" || "${manifest_go}" != "${expected_go}" ]]; then
  fail "Go version sources disagree: .go-version=${expected_go}, go.mod=${module_go}, manifest=${manifest_go}"
fi
current_go="$(go env GOVERSION 2>/dev/null || true)"
if [[ "${current_go}" != "go${expected_go}" ]]; then
  fail "Go toolchain mismatch: expected go${expected_go}, found ${current_go:-unknown}"
fi
ok "Go ${expected_go} matches all version sources"

expected_node="$(tr -d '[:space:]' < .node-version)"
manifest_node="$(manifest_version node)"
current_node="$(node --version 2>/dev/null || true)"
if [[ "${manifest_node}" != "${expected_node}" ]]; then
  fail "Node.js version sources disagree: .node-version=${expected_node}, manifest=${manifest_node}"
fi
if [[ "${current_node}" != "v${expected_node}" ]]; then
  fail "Node.js mismatch: expected v${expected_node}, found ${current_node:-unknown}"
fi
ok "Node.js ${expected_node} matches all version sources"

expected_hugo="$(tr -d '[:space:]' < .hugo-version)"
manifest_hugo="$(manifest_version hugo)"
current_hugo="$(hugo version 2>/dev/null || true)"
if [[ "${manifest_hugo}" != "${expected_hugo}" ]]; then
  fail "Hugo version sources disagree: .hugo-version=${expected_hugo}, manifest=${manifest_hugo}"
fi
if [[ "${current_hugo}" != *"v${expected_hugo}"* ]]; then
  fail "Hugo mismatch: expected ${expected_hugo}, found ${current_hugo:-unknown}"
fi
ok "Hugo ${expected_hugo} matches all version sources"

expected_ast_grep="$(manifest_version astGrep)"
package_ast_grep="$(jq -r '.devDependencies["@ast-grep/cli"] // empty' .github/tools/package.json)"
if [[ "${package_ast_grep}" != "${expected_ast_grep}" ]]; then
  fail "ast-grep version sources disagree: package=${package_ast_grep:-unset}, manifest=${expected_ast_grep}"
fi
ok "ast-grep ${expected_ast_grep} matches all version sources"

current_semgrep="$(semgrep --version 2>/dev/null | head -n 1)"
expected_semgrep="$(manifest_version semgrep)"
if [[ "${current_semgrep}" != "${expected_semgrep}" ]]; then
  fail "Semgrep mismatch: expected ${expected_semgrep}, found ${current_semgrep:-unknown}"
fi
ok "Semgrep ${expected_semgrep} matches .ci/versions.yaml"

current_trivy="$(trivy version 2>/dev/null | sed -n 's/^Version: //p' | head -n 1)"
expected_trivy="$(manifest_version trivy)"
if [[ "${current_trivy}" != "${expected_trivy}" ]]; then
  fail "Trivy mismatch: expected ${expected_trivy}, found ${current_trivy:-unknown}"
fi
ok "Trivy ${expected_trivy} matches .ci/versions.yaml"

current_kind="$(kind version 2>/dev/null || true)"
expected_kind="$(manifest_version kindCli)"
if [[ "${current_kind}" != *"v${expected_kind}"* ]]; then
  fail "Kind mismatch: expected ${expected_kind}, found ${current_kind:-unknown}"
fi
ok "Kind ${expected_kind} matches .ci/versions.yaml"

current_kubectl="$(kubectl version --client --output=json 2>/dev/null | jq -r '.clientVersion.gitVersion // empty')"
expected_kubectl="$(manifest_version kubectlCli)"
if [[ "${current_kubectl}" != "v${expected_kubectl}" ]]; then
  fail "kubectl mismatch: expected v${expected_kubectl}, found ${current_kubectl:-unknown}"
fi
ok "kubectl ${expected_kubectl} matches .ci/versions.yaml"

current_helm="$(helm version --short 2>/dev/null || true)"
expected_helm="$(manifest_version helmCli)"
if [[ "${current_helm}" != "v${expected_helm}" && "${current_helm}" != "v${expected_helm}"+* ]]; then
  fail "Helm mismatch: expected v${expected_helm}, found ${current_helm:-unknown}"
fi
ok "Helm ${expected_helm} matches .ci/versions.yaml"

for command_name in \
  bash curl docker git go helm hugo jq kind kubectl make node npm python3 semgrep tofu trivy; do
  require_nix_command "${command_name}"
done

printf '\nPinned devenv toolchain contract verified.\n'

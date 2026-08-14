{
  config,
  inputs,
  lib,
  pkgs,
  ...
}:

let
  readVersionFile = path: lib.removeSuffix "\n" (builtins.readFile path);
  manifestLines = lib.splitString "\n" (builtins.readFile ./.ci/versions.yaml);
  manifestVersion = key:
    let
      line = lib.findFirst (
        candidate: builtins.match "[[:space:]]+${key}: \"[^\"]+\"" candidate != null
      ) (throw ".ci/versions.yaml does not declare ${key}") manifestLines;
      match = builtins.match "[[:space:]]+${key}: \"([^\"]+)\"" line;
    in
    builtins.elemAt match 0;

  goVersion = readVersionFile ./.go-version;
  goPackageName = "go_" + builtins.replaceStrings [ "." ] [ "_" ] goVersion;
  goPackage = builtins.getAttr goPackageName inputs.go-overlay.packages.${pkgs.stdenv.hostPlatform.system};
  nodeVersion = readVersionFile ./.node-version;
  hugoVersion = readVersionFile ./.hugo-version;

  exactPackage =
    name: expected: package:
    if lib.getVersion package == expected then
      package
    else
      throw "${name} version mismatch: expected ${expected}, nixpkgs provides ${lib.getVersion package}";
in
{
  name = "openbao-kubernetes-kms";

  packages = [
    pkgs.bash
    pkgs.coreutils
    pkgs.curl
    pkgs.docker-client
    pkgs.findutils
    pkgs.gawk
    pkgs.git
    pkgs.gnugrep
    pkgs.gnused
    goPackage
    pkgs.gnumake
    pkgs.jq
    (exactPackage "Helm" (manifestVersion "helmCli") pkgs.kubernetes-helm)
    (exactPackage "Hugo" hugoVersion pkgs.hugo)
    (exactPackage "Kind" (manifestVersion "kindCli") pkgs.kind)
    (exactPackage "kubectl" (manifestVersion "kubectlCli") pkgs.kubectl)
    (exactPackage "Node.js" nodeVersion pkgs.nodejs_22)
    pkgs.opentofu
    pkgs.python3
    (exactPackage "Semgrep" (manifestVersion "semgrep") pkgs.semgrep)
    (exactPackage "Trivy" (manifestVersion "trivy") pkgs.trivy)
  ];

  env = {
    GOFLAGS = "-mod=vendor";
    GOROOT = "${goPackage}/share/go/";
    GOTOOLCHAIN = "local";
  };

  profiles.editor.module = {
    languages.go = {
      enable = true;
      package = goPackage;
      delve.enable = true;
      lsp.enable = true;
    };
  };

  enterShell = ''
    export GOPATH="''${XDG_CACHE_HOME:-$HOME/.cache}/openbao-kubernetes-kms/go"
    export PATH="$DEVENV_PROFILE/bin:$DEVENV_ROOT/bin:$DEVENV_ROOT/.github/tools/node_modules/.bin:$GOPATH/bin:$PATH"
  '';

  tasks = {
    "kms:verify-toolchain" = {
      description = "Verify the pinned service-independent toolchain contract";
      exec = "make verify-devenv";
      cwd = config.git.root;
      before = [ "devenv:enterTest" ];
    };

    "kms:bootstrap" = {
      description = "Install repository-managed tools required by the core contributor workflow";
      exec = "make bootstrap";
      cwd = config.git.root;
    };

    "kms:ci-core" = {
      description = "Run the cluster-independent pull-request-equivalent gate";
      exec = "make ci-core";
      cwd = config.git.root;
    };

    "kms:docs" = {
      description = "Check and build the documentation site";
      exec = "make HUGO_RUN=hugo docs-check docs-build";
      cwd = config.git.root;
    };
  };
}

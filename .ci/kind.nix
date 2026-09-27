{ pkgs }:
let
  version = "0.33.0";
  releases = {
    x86_64-linux = { platform = "linux-amd64"; sha256 = "aee6151561422756b764a4ae28e7f44cda5af5a9eead3cc9985112b1de8d8e0d"; };
    aarch64-linux = { platform = "linux-arm64"; sha256 = "20022bee6cfcd5086cb7234d218e3454e6090022f2a8f55d1fa7fcf42c3867a2"; };
    x86_64-darwin = { platform = "darwin-amd64"; sha256 = "5a99f26f57246dc9319dd294803313197a0f34d33c525b3ea8b655db5916ece0"; };
    aarch64-darwin = { platform = "darwin-arm64"; sha256 = "0c8c7dbe5e23594a198b786c4bc13dacc101fa6196b0cb0b23a1ca44e61f4b4f"; };
  };
  release = releases.${pkgs.stdenv.hostPlatform.system};
in
pkgs.runCommand "kind-${version}" {
  inherit version;
  src = pkgs.fetchurl {
    url = "https://github.com/kubernetes-sigs/kind/releases/download/v${version}/kind-${release.platform}";
    inherit (release) sha256;
  };
} ''
  install -Dm755 "$src" "$out/bin/kind"
''

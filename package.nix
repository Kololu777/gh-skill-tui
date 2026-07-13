{ buildGoModule }:

buildGoModule rec {
  pname = "gh-skill-tui";
  version = "0.3.0";

  src = ./.;

  vendorHash = "sha256-qrX55UC7IMOZS8yDB+JIf5fAatfsRaMl38T1rDKHSAg=";

  ldflags = [ "-X main.version=${version}" ];

  postInstall = ''
    ln -s $out/bin/gh-skill-tui $out/bin/gh-skill-check
  '';
}

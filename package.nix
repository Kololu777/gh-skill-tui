{ buildGoModule }:

buildGoModule rec {
  pname = "gh-skill-tui";
  version = "0.2.0";

  src = ./.;

  vendorHash = "sha256-R6CchW9qEYN87smTucm1BSgMqGMUVfNR7sBX57a45Ek=";

  ldflags = [ "-X main.version=${version}" ];

  postInstall = ''
    ln -s $out/bin/gh-skill-tui $out/bin/gh-skill-check
  '';
}

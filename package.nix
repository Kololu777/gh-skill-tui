{ buildGoModule }:

buildGoModule {
  pname = "gh-skill-tui";
  version = "0.1.0";

  src = ./.;

  vendorHash = "sha256-R6CchW9qEYN87smTucm1BSgMqGMUVfNR7sBX57a45Ek=";

  postInstall = ''
    ln -s $out/bin/gh-skill-tui $out/bin/gst
  '';
}

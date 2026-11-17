{ pkgs, ... }:
{
  languages.go.enable = true;
  languages.go.package = pkgs.go_1_26;
  languages.go.lsp.enable = false;

  packages = [
    pkgs.golangci-lint
  ];

  scripts.check.exec = ''
    go test ./...
    go vet ./...
    golangci-lint run
  '';

  enterShell = ''
    go version
  '';
}

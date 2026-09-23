# Releasing

router-axi releases are source-only. Do not attach prebuilt binaries.

For v0.1.0, merge the release-preparation pull request before starting this
checklist. Run every command from a clean checkout:

- [ ] Update `main` without creating a merge commit, and confirm it is clean.

  ```sh
  git fetch origin --prune
  git switch main
  git merge --ff-only origin/main
  test -z "$(git status --porcelain)"
  ```

- [ ] Run the complete local check.

  ```sh
  nix develop --no-pure-eval --command check
  ```

- [ ] Audit the tracked release contents for router addresses, serial numbers,
      credentials, call data, and captured live responses. Review every match;
      placeholders and security-policy wording are expected.

  ```sh
  git grep -nEi '(password|credential|serial|call.?list|router[_ -]?host|https?://([0-9]{1,3}\.){3}[0-9]{1,3})'
  ```

- [ ] Keep live-router tests local-only. If they are run, use the
      credential-safe command in [README.md](README.md#opt-in-live-router-tests),
      confirm `CI` is empty, and do not retain or publish their output.
- [ ] Create and push the SemVer tag on the checked `main` commit.

  ```sh
  git tag -a v0.1.0 -m 'router-axi v0.1.0'
  git push origin v0.1.0
  ```

- [ ] Create a GitHub source release without binary assets, then verify its tag,
      status, URL, and source archives.

  ```sh
  gh release create v0.1.0 --verify-tag --title 'router-axi v0.1.0' --generate-notes
  gh release view v0.1.0 --json tagName,isDraft,isPrerelease,url,assets
  curl -fsSI https://github.com/Azd325/router-axi/archive/refs/tags/v0.1.0.tar.gz
  curl -fsSI https://github.com/Azd325/router-axi/archive/refs/tags/v0.1.0.zip
  ```

- [ ] Verify installation from the published tag in an isolated directory.

  ```sh
  install_dir=$(mktemp -d)
  GOBIN="$install_dir" go install github.com/Azd325/router-axi/cmd/router-axi@v0.1.0
  "$install_dir/router-axi" --help
  rm -rf "$install_dir"
  ```

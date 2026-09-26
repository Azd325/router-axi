# Releasing

router-axi releases are source-only. Do not attach prebuilt binaries.

Set `VERSION` to the SemVer tag being released, merge the release-preparation
pull request, then run this checklist from a clean checkout:

```sh
VERSION=v0.4.0
```

- [ ] Update `main` without creating a merge commit, and confirm it is clean.

  ```sh
  git fetch origin --prune --tags
  git switch main
  git merge --ff-only origin/main
  test -z "$(git status --porcelain)"
  ```

- [ ] Run the complete local check.

  ```sh
  nix develop --no-pure-eval --command check
  ```

- [ ] Audit tracked release contents for router addresses, serial numbers,
      credentials, call data, and captured live responses. Review every match;
      placeholders and security-policy wording are expected.

  ```sh
  git grep -nEi '(password|credential|serial|call.?list|router[_ -]?host|https?://([0-9]{1,3}\.){3}[0-9]{1,3})'
  ```

- [ ] Keep live-router tests local-only. Run them only with the credential-safe
      command in [README.md](README.md#opt-in-live-router-tests), confirm `CI`
      is empty, and do not retain or publish their output. Record which coverage
      actually ran. Do not run reboot hardware tests: reboot is not idempotent
      and has no automatic recovery verification. Live Wi-Fi mutation coverage
      is separately opt-in and must restore the original state. Live backup
      coverage is separately opt-in, never writes an export file, and requires
      `ROUTER_AXI_BACKUP_PASSWORD`.

- [ ] Create and push the SemVer tag on the checked `main` commit.

  ```sh
  git tag -a "$VERSION" -m "router-axi $VERSION"
  git push origin "$VERSION"
  ```

- [ ] Create a GitHub source release without binary assets, then verify its tag,
      status, URL, and source archives.

  ```sh
  gh release create "$VERSION" --verify-tag --title "router-axi $VERSION" --generate-notes
  gh release view "$VERSION" --json tagName,isDraft,isPrerelease,url,assets
  curl -fsSI "https://github.com/Azd325/router-axi/archive/refs/tags/$VERSION.tar.gz"
  curl -fsSI "https://github.com/Azd325/router-axi/archive/refs/tags/$VERSION.zip"
  ```

- [ ] Verify installation from the published tag in an isolated directory. The
      version fast path must report the released tag rather than `dev`.

  ```sh
  install_dir=$(mktemp -d)
  GOBIN="$install_dir" go install "github.com/Azd325/router-axi/cmd/router-axi@$VERSION"
  test "$("$install_dir/router-axi" --version)" = "$VERSION"
  "$install_dir/router-axi" --help
  rm -rf "$install_dir"
  ```

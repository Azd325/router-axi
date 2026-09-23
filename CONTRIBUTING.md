# Contributing

## Setup

Requirements: Nix with flakes enabled. `direnv` is optional.

```sh
nix develop --no-pure-eval
check
```

Live-router tests are local-only. Follow the credential-safe invocation in [README.md](README.md#opt-in-live-router-tests); do not run them in CI or include their output in issues or pull requests.

## Workflow

1. Create a feature branch from `main`.
2. Make one focused change with tests and documentation where the public contract changes.
3. Run `nix develop --no-pure-eval --command check`.
4. Commit the change.
5. Submit it through the validation gate:

   ```sh
   git push no-mistakes <branch>
   ```

The gate validates the branch, opens the pull request, and records its attestation. Do not open pull requests manually or push feature branches directly to `origin`.

## Conventions

- Preserve read-only defaults, structured errors, exit codes, compact output, and deterministic `--json` output.
- Use sanitized fixtures only. Never commit router addresses, serial numbers, credentials, call data, or live protocol responses.
- Compatibility comes from tested behavior, not router model names.

## Security

Report vulnerabilities privately; see [SECURITY.md](SECURITY.md).

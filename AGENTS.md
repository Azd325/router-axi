# Agent guide

## Scope

`router-axi` is a local, agent-ergonomic router CLI. The supported surface is read-only TR-064 inspection plus one confirmed mutation: `wifi enable|disable` over the documented `WLANConfiguration:GetInfo`/`SetEnable` actions. Mutations follow the VISION "Read before change" contract: they preview their intended effect without confirmation, require an explicit `--confirm` flag, are idempotent with verified state, refuse ambiguous targets, and never print SSIDs or radio data. Do not add other mutations, browser scraping, telemetry, credential persistence, or model-name-based capability guesses without an approved design change.

## Safety

- Read credentials only from `ROUTER_AXI_USERNAME` and `ROUTER_AXI_PASSWORD`.
- Never log, commit, or include in fixtures router addresses, serial numbers, call data, credentials, or captured live responses.
- Keep live-router tests opt-in. They require `ROUTER_AXI_LIVE_TEST=1`, explicit credentials and host, and must refuse CI. Live mutation coverage additionally requires `ROUTER_AXI_LIVE_MUTATION_TEST=1`, defaults to skipped, and must restore the original radio state.
- Preserve structured errors, exit codes, compact default output, and `--json` field ordering.

## Changes

- Read `VISION.md` before changing command behavior.
- Keep TR-064 compatibility fixture-backed; add a regression test for each discovered router variant.
- Run `nix develop --no-pure-eval --command check` before committing.
- Commit focused changes on a feature branch and submit them with `git push no-mistakes <branch>`; do not push feature branches directly to `origin`.

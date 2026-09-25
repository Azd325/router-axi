# Agent guide

## Scope

`router-axi` is a local, agent-ergonomic router CLI. The supported surface is read-only TR-064 inspection plus confirmed `wifi enable|disable` over documented `WLANConfiguration:GetInfo`/`SetEnable` and `reboot` over documented `DeviceConfig:Reboot`, plus `backup` over the documented `DeviceConfig:X_AVM-DE_GetConfigFile` export. Mutations follow the VISION "Read before change" contract: they preview their intended effect without confirmation, require an explicit `--confirm` flag, refuse ambiguous targets, and never print SSIDs or radio data. Wi-Fi changes are idempotent with verified state. Reboot is not idempotent: send it once, report acceptance rather than recovery, never retry or poll after initiation, and report lost responses as uncertain outcomes. Backup is not a mutation of the router: it exports to an explicitly named destination with an atomic owner-only write, never overwrites without `--force`, keeps the export passphrase in `ROUTER_AXI_BACKUP_PASSWORD` only, never reuses the login password, and never prints the passphrase, the one-time download URL, or export contents. Do not add other mutations, browser scraping, telemetry, credential persistence, or model-name-based capability guesses without an approved design change.

## Safety

- Read credentials only from `ROUTER_AXI_USERNAME` and `ROUTER_AXI_PASSWORD`.
- Never log, commit, or include in fixtures router addresses, serial numbers, call data, credentials, or captured live responses. The export passphrase is read only from `ROUTER_AXI_BACKUP_PASSWORD`; never reuse the login password for it and never log or persist either value.
- Keep live-router tests opt-in. They require `ROUTER_AXI_LIVE_TEST=1`, explicit credentials and host, and must refuse CI. Live mutation coverage additionally requires `ROUTER_AXI_LIVE_MUTATION_TEST=1`, defaults to skipped, and must restore the original radio state. Those flags must never enable reboot. No live reboot test is provided; any future reboot test requires a distinct dangerous opt-in and must remain skipped by default. Live backup coverage additionally requires `ROUTER_AXI_LIVE_BACKUP_TEST=1` plus `ROUTER_AXI_BACKUP_PASSWORD`, defaults to skipped, and never writes a backup file.
- Preserve structured errors, exit codes, compact default output, and `--json` field ordering.

## Changes

- Read `VISION.md` before changing command behavior.
- Keep TR-064 compatibility fixture-backed; add a regression test for each discovered router variant.
- Run `nix develop --no-pure-eval --command check` before committing.
- Commit focused changes on a feature branch and submit them with `git push no-mistakes <branch>`; do not push feature branches directly to `origin`.

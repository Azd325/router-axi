---
name: router-axi
description: Inspect and operate a supported home router through the router-axi CLI (FRITZ!Box TR-064). Use when an agent needs to review router status, WAN, traffic, calls, devices, leases, Wi-Fi, guest Wi-Fi, or port forwards, or to make confirmed wifi, reboot, or backup changes from a terminal.
---

# router-axi

`router-axi` is a local, agent-ergonomic CLI over documented FRITZ!Box TR-064
interfaces. It is non-interactive, prints compact AXI text by default with
JSON on explicit `--json`, and never uses prompts. The router defaults to
`http://fritz.box:49000`; override with `--host ADDRESS` or `ROUTER_AXI_HOST`
(an address without a port uses TR-064 port 49000 for HTTP or 49443 for
HTTPS). Credentials are read only from the environment. This skill is static
documentation; run the CLI for live router state.

## Command surface

- `router-axi` — status is the default view (router identity and firmware).
- `doctor` — bounded connectivity and capability diagnosis.
- `overview` — identity, WAN state, and traffic totals in one atomic read.
- `wan` — internet connection state (status, external IP, IP family, uptime).
- `traffic` — byte totals with an `observed_at` timestamp.
- `watch` — bounded read-only WAN/traffic polling; defaults `--interval 5s
  --count 6` (bounds 1s–1m and 1–3600, no unbounded mode, JSONL with `--json`,
  Ctrl-C exits 130).
- `calls` — call history (100 entries default, `--all` for the full list).
- `devices` — connected and known LAN clients (`--all` supported).
- `leases` — observed Hosts-table lease metadata (`--all` supported).
- `wifi` — Wi-Fi inspection; `wifi enable|disable` changes a radio.
- `guest` — documented guest Wi-Fi inspection (public SSID, aggregate state).
- `forwards` — port-forwarding rules (`--all` supported).
- `reboot` — preview router restart; execute once with `--confirm`.
- `backup` — download the documented configuration export to a file.
- `skill install` — explicitly install this static skill; `--path DIRECTORY`
  selects another agent skills parent directory.
- `version` — CLI version.

## Confirmation-gated mutations

- `wifi enable|disable` without `--confirm` only prints a preview with the
  exact confirmed command; with `--confirm` it re-reads state, sends the
  documented `SetEnable` once, and verifies the new state. Idempotent; an
  already-satisfied target reports `changed: false`. Multi-instance routers
  require `--instance N`.
- `reboot` without `--confirm` is a preview plan; `reboot --confirm` sends
  exactly one documented `DeviceConfig:Reboot` and reports acknowledgement,
  not recovery. **Reboot is not idempotent** — never repeat it after an error
  or lost response; check recovery manually with `doctor`.
- `backup --output PATH` writes the export with an atomic owner-only file,
  never overwrites without `--force`, and requires an HTTPS router origin
  (for example `--host https://fritz.box:49443`) with a locally trusted
  certificate. The export passphrase is required to restore the file.

## Output style

Compact AXI text by default (`key:` blocks, or `list[N]{columns}:` tables);
`--json` emits JSON on stdout with stable field order. Empty results state
zero results explicitly. Successful commands suggest the next operation with
`next:`. Errors are structured on stderr in the selected format with `code`,
`message`, and usually `hint`; JSON consumers must also check the exit code.

## Exit codes

`0` success; `1` local output/internal failure; `2` usage or configuration
error; `3` authentication failure; `4` router unreachable (including
`tls_untrusted` on untrusted HTTPS certificates — never skipped); `5`
unsupported router capability; `6` router or protocol error.

## Credentials

- `ROUTER_AXI_HOST` — optional default router address (HTTP or HTTPS origin).
- `ROUTER_AXI_USERNAME` and `ROUTER_AXI_PASSWORD` — router login credentials,
  read only from the environment, never from arguments or prompts.
- `ROUTER_AXI_BACKUP_PASSWORD` — the configuration-export passphrase, used
  only by `backup`, never reused from the login password, never printed.

## Top-level help

The block below is the CLI's own `router-axi help` output; a test fails the
build if it drifts from the committed skill.

<!-- router-axi-help:begin -->
usage: router-axi [--host ADDRESS] [--json] [command]

commands:
  doctor    bounded connectivity and capability diagnosis
  status    router identity and firmware (default)
  overview  identity, WAN state, and traffic totals
  wan       internet connection state
  traffic   byte totals
  watch     bounded WAN state and traffic polling (6 samples, 5s interval)
  calls     call history
  devices   connected and known LAN clients
  leases    observed Hosts table lease metadata
  wifi      Wi-Fi inspection; wifi enable|disable changes a radio with --confirm
  guest     documented guest Wi-Fi inspection
  forwards  port-forwarding rules
  reboot    preview router restart; execute once with --confirm
  backup    download the documented configuration export to a file
  skill     install the router-axi agent skill (explicit opt-in)
  version   CLI version

authentication: ROUTER_AXI_USERNAME and ROUTER_AXI_PASSWORD
backup export passphrase: ROUTER_AXI_BACKUP_PASSWORD

<!-- router-axi-help:end -->

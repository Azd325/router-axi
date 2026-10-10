---
name: router-axi
description: Inspect and operate a supported home router through the router-axi CLI (FRITZ!Box TR-064). Use when an agent needs to review router status, WAN, traffic, calls, devices, leases, DHCP server configuration, DSL link diagnostics, account rights and login posture, Wi-Fi, guest Wi-Fi, port forwards, or service exposure flags, or to make confirmed wifi, WAN reconnect, reboot, Wake-on-LAN, firmware update check, or backup changes from a terminal.
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
- `wan` — compact internet connection state; `wan detail` explicitly reads bounded physical-link properties, optional router-reported rates, totals, and DNS, and connection type, IPv6 status, NAT, and PPP disconnect prevention; `wan reconnect` previews dropping the internet connection and executes once with `--confirm`.
- `traffic` — byte totals with an `observed_at` timestamp.
- `watch` — bounded read-only WAN/traffic polling; defaults `--interval 5s
  --count 6` (bounds 1s–1m and 1–3600, no unbounded mode, JSONL with `--json`,
  Ctrl-C exits 130).
- `calls` — call history (100 entries default, `--all` for the full list).
- `devices` — connected and known LAN clients (100 entries by default,
  `--all` for the full list); `devices detail --ip ADDRESS` reports one
  device's port, speed, guest and VPN flags, WAN access, and update state.
- `leases` — observed Hosts-table lease metadata (100 entries by default,
  `--all` for the full list).
- `dhcp` — read-only DHCP server configuration; never reservation inventory.
- `firmware` — read-only installed firmware, reported update availability, and auto-update state; `firmware check` requests one update check with `--confirm`; never installs updates.
- `dsl` — read-only DSL link diagnostics from documented WANDSLInterfaceConfig:X_AVM-DE_GetDSLInfo: link state, modulation, profile, current/max rates, noise margin, attenuation, and FEC/CRC error counters; `dsl detail` reports the total error counters and the router's line-fault diagnosis.
- `account` — read-only own username and configured rights, anonymous login, default-password posture, and second-factor enabled state; never user enumeration or passwords.
- `wifi` — Wi-Fi inspection; `wifi detail [--instance N]` reports one radio's safe documented properties; `wifi enable|disable` changes a radio.
- `guest` — documented guest Wi-Fi inspection (public SSID, aggregate state, timeout and isolation configuration).
- `forwards` — port-forwarding rules (100 entries by default, `--all` for the
  full list).
- `exposure` — read-only enabled, port, and status flags of remote access, DDNS, MyFRITZ, storage, UPnP, WebDAV, speedtest, and TR-069; never usernames, e-mail addresses, host names, or URLs.
- `reboot` — preview router restart; execute once with `--confirm`.
- `wake MAC` — preview Wake-on-LAN to one supplied MAC; send once with `--confirm`.
- `backup` — download the documented configuration export to a file.
- `setup install|check|uninstall --agent claude|codex|opencode|all` — explicitly manage offline session integrations; nothing is registered automatically.
- `session dashboard` — offline context for the installed session integration; never contacts the router.
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
- `wan reconnect` without `--confirm` is a preview plan; `wan reconnect
  --confirm` sends exactly one documented `ForceTermination` to the active
  WAN connection service and reports acknowledgement, not recovery. The
  internet connection drops and a new external address may be assigned.
  **WAN reconnect is not idempotent** — never repeat it after an error or
  lost response; check the connection manually with `wan`.
- `wake MAC` without `--confirm` previews one validated MAC and the exact
  confirmed command; with `--confirm` it sends the documented Hosts wake
  action once. Acceptance does not mean the device woke. Never look up
  devices, retry, or poll; a lost response is uncertain, so check manually.
- `firmware check` without `--confirm` previews the exact confirmed command;
  with `--confirm` it sends the documented UserInterface update-check action
  once. Acceptance does not mean an update exists; read the result with
  `firmware`. It never installs an update. Never retry or poll; a lost
  response is uncertain.
- `backup --output PATH` writes the export with an atomic owner-only file,
  never overwrites without `--force`, and requires an HTTPS router origin
  (for example `--host https://fritz.box:49443`) with a locally trusted
  certificate. The export passphrase is required to restore the file.

## Output style

Compact AXI text by default (`key:` blocks, or `list[N]{columns}:` tables);
`--json` emits JSON on stdout with stable field order. Empty results state
zero results explicitly as a plain `key: text` line. List commands accept
`--fields a,b` to choose columns; `--json` always reports every field. Text
that looks like a number or boolean is quoted (`software: "8.20"`). Successful
commands suggest the next operation with `next:`, repeating an explicit
`--host`. Structured results and errors use stdout in the selected format;
stderr is reserved for diagnostics, including watch's `output_failed` fallback
when stdout itself fails. Errors have `code`, `message`, and usually `hint`;
JSON consumers must also check the exit code. Watch appends a terminal
structured error record to stdout when polling fails.

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
  wan       internet connection state; wan detail adds bounded link details; wan reconnect drops the connection once with --confirm
  traffic   byte totals
  watch     bounded WAN state and traffic polling (6 samples, 5s interval)
  calls     call history
  event-log bounded router events (telephony excluded by default)
  devices   connected and known LAN clients; devices detail adds one device's link and access state
  leases    observed Hosts table lease metadata
  dhcp      DHCP server configuration (never reservation inventory)
  dsl       DSL link diagnostics; dsl detail adds total error counters and the router's line-fault diagnosis
  firmware  installed firmware, reported update availability, and auto-update state; firmware check requests one update check with --confirm
  account   own rights and login posture
  wifi      Wi-Fi inspection; wifi detail adds per-radio properties; wifi enable|disable changes a radio with --confirm
  guest     documented guest Wi-Fi inspection
  forwards  port-forwarding rules
  exposure  remote-access and local-service exposure flags
  reboot    preview router restart; execute once with --confirm
  wake      preview Wake-on-LAN to one MAC; send once with --confirm
  backup    download the documented configuration export to a file
  skill     install the router-axi agent skill (explicit opt-in)
  setup     manage opt-in Claude Code, Codex, and OpenCode session integrations
  session   print the offline session dashboard
  version   CLI version

authentication: ROUTER_AXI_USERNAME and ROUTER_AXI_PASSWORD
backup export passphrase: ROUTER_AXI_BACKUP_PASSWORD

<!-- router-axi-help:end -->

# router-axi

An agent-ergonomic CLI for inspecting and operating supported home routers.

The current release provides device information, WAN status, traffic statistics, call-list access, connected-device, observed lease metadata, DHCP server configuration, DSL link diagnostics, firmware update status, account rights and login posture, Wi-Fi, documented guest Wi-Fi, and port-forward inspection through documented FRITZ!Box TR-064 interfaces, plus confirmed Wi-Fi changes, WAN reconnect, reboot, and configuration export.

## Design constraints

- Compact AXI output by default, with JSON available explicitly.
- Non-interactive commands with structured data and errors on stdout, plus meaningful exit codes.
- Read-only behavior by default.
- Explicit confirmation for disruptive operations.
- Local operation without telemetry or a hosted account.

## Agent integrations

Session integrations are explicit opt-in and add only a compact **offline** dashboard to agent sessions; they never contact the router, read credentials, or register automatically. Install, inspect, or remove the managed integration for Claude Code, Codex, OpenCode, or all three:

```sh
router-axi setup install --agent all
router-axi setup check --agent claude
router-axi setup uninstall --agent opencode
```

Repeated installs repair the executable path and are idempotent. OpenCode setup adds `@opencode-ai/plugin` when needed; uninstall removes that declaration only when router-axi added it and its value remains unchanged. Pre-existing or modified dependencies and all unrelated package metadata are preserved.

The bundled Agent Skill is a secondary on-demand discovery path for agents that support the agentskills.io format:

```sh
router-axi skill install
```

The default skill destination is `~/.agents/skills/router-axi/SKILL.md`; use `--path DIRECTORY` for another agent skills parent directory. Repeating the same install is a silent no-op.

See [VISION.md](VISION.md) for the acceptance policy. Contributors should read
[CONTRIBUTING.md](CONTRIBUTING.md); security reports belong in
[SECURITY.md](SECURITY.md).

## Installation

Install v0.5.0 with Go:

```sh
go install github.com/Azd325/router-axi/cmd/router-axi@v0.5.0
```

This installs `router-axi` in `GOBIN`, or in `GOPATH/bin` when `GOBIN` is not
set. The command becomes available after that directory is on `PATH`.

From a checked-out repository, use the launcher instead:

```sh
./bin/router-axi --help
```

The launcher builds the current checkout in the Nix development environment
and runs it. It requires Nix with flakes enabled and is intended for development,
not as a versioned installation.

## Usage

```sh
export ROUTER_AXI_USERNAME='router-user'
export ROUTER_AXI_PASSWORD='router-password'

router-axi                 # status is the default view
router-axi version
router-axi --version       # also supports -v and -V
router-axi doctor
router-axi doctor --json
router-axi status
router-axi overview
router-axi wan
router-axi wan detail
router-axi wan detail --json
router-axi traffic
router-axi watch
router-axi watch --interval 2s --count 10 --json
router-axi calls
router-axi devices
router-axi devices --json
router-axi devices detail --ip 192.0.2.20
router-axi devices detail --ip 192.0.2.20 --json
router-axi leases
router-axi leases --json
router-axi leases --all
router-axi dhcp
router-axi dhcp --json
router-axi dsl
router-axi dsl --json
router-axi firmware
router-axi firmware --json
router-axi account
router-axi account --json
router-axi wifi
router-axi wifi --json
router-axi wifi detail
router-axi wifi detail --instance 2 --json
router-axi guest
router-axi guest --json
router-axi skill install
router-axi setup install --agent all
router-axi setup check --agent claude
router-axi wifi enable
router-axi wifi disable
router-axi wifi disable --instance 1 --confirm
router-axi reboot          # preview only; restart requires --confirm
router-axi wan reconnect   # preview only; reconnect requires --confirm
router-axi backup --host https://fritz.box:49443 --output fritz.export
router-axi forwards
router-axi forwards --json
router-axi forwards --all
router-axi wan --json
```

On the FRITZ!Box 6591 with FRITZ!OS 8.25, the advertised WAN service name does
not reliably identify the returned address family. `wan` therefore classifies
`ip_family` from the address itself. `traffic` includes `observed_at`, the UTC
RFC 3339 time at which the CLI completed both counter reads; compact output
shows `unknown` if an observation time is unavailable.
When multiple `WANIPConnection` or `WANPPPConnection` services are advertised,
`wan` and `overview` resolve the active service through the documented
`Layer3Forwarding:GetDefaultConnectionService` action; they do not select the
first advertised service. The identifier matching and fail-closed rules are
the same as [`forwards`](#port-forward-inspection).

`doctor` performs one bounded diagnosis: it fetches the TR-064 device
description once and, when `DeviceInfo` is advertised, invokes only
`DeviceInfo:GetInfo` to verify authentication and obtain model and firmware.
It reports endpoint reachability, TR-064 availability, authentication, and
whether the router advertises the services required by `status`, `overview`,
`wan`, `traffic`, `watch`, `calls`, `devices`, `leases`, `dhcp`, `dsl`, `firmware`, `account`, `wifi`, `forwards`, `reboot`, and
`backup`. `watch` is advertised only when the description includes Layer3Forwarding,
WANCommonInterfaceConfig, and either WANIPConnection or WANPPPConnection; doctor does
not invoke any of their actions. Doctor does not report a separate `guest` capability: WLANConfiguration
advertisement alone cannot prove that every instance implements the documented
`X_AVM-DE_GetWLANExtInfo` discriminator without additional reads, and doctor does
not perform those reads. For reboot, doctor applies the same target selection as the command (exactly
one `DeviceConfig:1` service, plus exactly one `DeviceInfo:1` when credentials
are set), but the result is only a candidate capability: doctor never invokes
Reboot or verifies reboot permission. For backup,
doctor requires exactly one `DeviceConfig:1` and never invokes
`X_AVM-DE_GetConfigFile`; like the other capabilities it is only a candidate
and proves no export permission. For leases,
Hosts advertisement is a candidate capability, not proof of meaningful lease
metadata; doctor does not read the host table. For forwards,
WANIPConnection or WANPPPConnection service advertisement is only a candidate
capability: doctor does not invoke Layer3Forwarding, fetch SCPDs, or verify
enumeration actions. It does
not invoke those commands, retrieve a WAN
address or call list, infer support from a model name, or emit serial numbers,
WAN/phone addresses, call data, or credentials. Unsupported optional
capabilities are a successful diagnosis and include remediation.

Doctor preserves completed checks when diagnosis cannot continue: compact output
writes the partial report followed by its structured error on stdout. With `--json`,
it writes one valid envelope: `{"doctor":<doctor>,"error":<structured error>}`.
Unreachable
routers exit `4`, rejected credentials exit `3`, disabled or missing core
TR-064 support exits `5`, and malformed/router protocol responses exit `6`.
Both compact AXI and JSON field order are deterministic. JSON consumers must
therefore inspect the process exit code as well as stdout.

`devices` reads the documented TR-064 `Hosts:1` table and includes both active
and remembered inactive LAN clients. Each entry contains only `name` when the
router provides one, `ip_address`, `mac_address`, `interface_type`, and
`active`. The MAC address is the stable identifier when the router provides
one; router-assigned table indexes are not exposed because they are not stable.
Entries are sorted by MAC address, then IP address and name. Compact and JSON
output return at most 100 entries by default and accept `--all`; compact output
reports omitted entries, while JSON includes `total` and `omitted`. Empty output
is `devices[0]: no devices found` in compact form and
`{"devices":[],"total":0,"omitted":0}` in JSON.

The `Hosts` service is optional on some TR-064 implementations. A router that
does not advertise it returns `unsupported_capability` and exit `5`; router
faults, malformed or implausibly large host counts, and malformed active states
remain protocol errors with exit `6`. Names, addresses, and interface types can be empty when the router
does not know them. `interface_type` is the service's documented interface
classification, not a physical switch port or inferred connection detail.

### Device detail

Bare `devices` keeps its list output unchanged. `devices detail --ip ADDRESS`
is the explicit read-only detail view of exactly one device. `--ip` takes one
IPv4 address and is required. A missing, repeated, or non-IPv4 `--ip`, a
positional address, and `--all` exit `2` before any request is sent; the
command never reports more than one device.

The command uses only the documented
[FRITZ! Hosts, §2.10](https://fritz.support/resources/TR-064_Hosts.pdf) action
`Hosts:X_AVM-DE_GetSpecificHostEntryByIP`. It requires exactly one advertised
`Hosts` service and confirms the action in that service's SCPD before invoking
it; a missing advertisement exits `5` and no action request is sent. The
logged-in account needs the `App` or `Phone` right; router fault `606` is
reported with that requirement and exit `6`. An address without a host entry
(fault `714`) is `unknown_device` with exit `2`.

Output is the `devices` identification fields plus `port` (Ethernet port number,
beginning with 1), `speed_mbps`, `guest`, `vpn`, `wan_access` (`granted`,
`denied`, `error`, or `unknown`), `update_available`, and `update_successful`
(`succeeded`, `failed`, or `unknown`). Optional fields the router omits, and a
port of `0`, are `unknown` in compact output and JSON `null`; unrecognized
`wan_access` and `update_successful` values are reported as `unknown`. Every
other argument of the response is discarded and never printed.

Compact output is:

```
device_detail:
  name: synthetic-host
  ip_address: 192.0.2.20
  mac_address: 02:00:00:00:00:20
  interface_type: Ethernet
  active: true
  port: 2
  speed_mbps: 1000
  guest: false
  vpn: false
  wan_access: granted
  update_available: true
  update_successful: succeeded
```

JSON fields are deterministic:

```json
{"name":"synthetic-host","ip_address":"192.0.2.20","mac_address":"02:00:00:00:00:20","interface_type":"Ethernet","active":true,"port":2,"speed_mbps":1000,"guest":false,"vpn":false,"wan_access":"granted","update_available":true,"update_successful":"succeeded"}
```

### Bounded WAN detail

Bare `wan` retains its compact connection-state output unchanged. `wan detail` is the
explicit read-only detail view. It uses the documented
`WANCommonInterfaceConfig:GetCommonLinkProperties` action for access type, physical-link
status, and the router's maximum layer-1 download/upload bit rates. The command requires
exactly one advertised WANCommonInterfaceConfig service and confirms
`GetCommonLinkProperties` in that service's SCPD before invoking it.

The command resolves the active WANIPConnection or WANPPPConnection through documented
`Layer3Forwarding:GetDefaultConnectionService`, then checks that active service's SCPD for
`X_GetDNSServers`. The returned comma-separated DNS list accepts IPv4 and IPv6 addresses.
It never looks up or invokes the undocumented connection-service `GetAddonInfos` action.

Each optional WANCommonInterfaceConfig action is checked in the common service's SCPD:

- `X_AVM-DE_GetAddonInfos` supplies sync and tariff download/upload rates in **bits/s**.
  Only some providers send a tariff rate, so a tariff rate of `0` is reported as
  `null`/`unknown`. A sync rate of `0` is preserved and means no sync or not reported.
- `X_AVM-DE_GetActiveProvider` supplies the provider name.
- `GetTotalBytesReceived`, `GetTotalBytesSent`, `GetTotalPacketsReceived`, and
  `GetTotalPacketsSent` supply byte and packet counters. The documented counters cover all
  interfaces over their connection intervals and reset when an interface reconnects. They
  depend on IGD being enabled on the router and may return zero when it is disabled;
  the CLI uses only the TR-064 actions and never falls back to a UPnP IGD endpoint.
- `X_AVM-DE_GetOnlineMonitor(NewSyncGroupIndex)` supplies maximum byte rates and current
  byte-rate series per sync group, including upstream real-time, high, default, and low
  priority classes. Group 0 supplies the count; subsequent groups are read once in index
  order. Reads are bounded to 16 groups and 256 values per series. An excessive or changing
  group count fails atomically. A zero count produces an empty `sync_groups` array.
  An empty or absent series produces an empty array. A malformed non-empty series,
  including one with a trailing comma, fails the command.

An absent optional action is never probed: its numeric/provider JSON fields are `null`,
compact output says `unknown`, and an unavailable monitor is `"sync_groups":null`.
An absent DNS action produces `"dns_servers":[]`, preserving the existing contract.
An advertised action that fails or returns malformed data fails the entire command with a
structured error; unavailable capability is never confused with a failed read. The only
exception is the authorization refusal of the two rights-restricted reads described below.

The [WANCommonInterfaceConfig document](https://fritz.support/resources/TR-064_WAN_Common_Interface_Config.pdf)
(version 22, dated 2026-02-25) documents the prefixed `X_AVM-DE_GetAddonInfos` on the common
service. It returns link rates, **not** the byte rates, byte totals, or DNS assumed by the old
connection-service lookup. DNS comes from the documented
[WANIPConnection](https://fritz.support/resources/TR-064_WAN_IP_Connection.pdf) or
[WANPPPConnection](https://fritz.support/resources/TR-064_WAN_PPP_Connection.pdf) action instead.
The common document explicitly requires App or Phone rights for add-on rates and provider;
link properties allow App, Phone, NAS, or Homeauto rights. It specifies no required-rights
list for monitor or counter actions; the IP/PPP documents likewise specify none for DNS.
An account with only NAS or Homeauto rights still gets the link properties: when the router
refuses `X_AVM-DE_GetAddonInfos` or `X_AVM-DE_GetActiveProvider` with SOAP fault `606`
(action not authorized), the fields that read fills are `null`/`unknown`. Every other error
on these two reads, and an authorization error on any other read, remains an error.

The existing nine JSON fields retain their order. New fields follow them in this order:
`total_download_packets`, `total_upload_packets`, `sync_download_bits_per_second`,
`sync_upload_bits_per_second`, `tariff_download_bits_per_second`,
`tariff_upload_bits_per_second`, `provider`, `sync_groups`.
For example, with all optional actions absent:

```json
{"access_type":"X_AVM-DE_Cable","physical_link_status":"Up","max_download_bits_per_second":1100000000,"max_upload_bits_per_second":55000000,"router_reported_download_bytes_per_second":null,"router_reported_upload_bytes_per_second":null,"total_download_bytes":null,"total_upload_bytes":null,"dns_servers":[],"total_download_packets":null,"total_upload_packets":null,"sync_download_bits_per_second":null,"sync_upload_bits_per_second":null,"tariff_download_bits_per_second":null,"tariff_upload_bits_per_second":null,"provider":null,"sync_groups":null}
```

Monitor fields ending in `bytes_per_second` use **bytes/s**, despite the action's `_bps`
argument names. Each current-rate field is an array in the router's original order; a single
numeric value becomes a one-element array. No sample ordering, elapsed interval, or cross-group
sum is inferred. The document describes `ds_current_bps` as downstream multicast and
`mc_current_bps` as combined home, guest, and multicast downstream traffic; the output
preserves the `ds_current_bytes_per_second` and `mc_current_bytes_per_second` names to avoid
silently swapping them. The existing `router_reported_download_bytes_per_second` and
`router_reported_upload_bytes_per_second` fields keep their position for output stability and
are always `null`/`unknown`; `sync_groups` carries the router-reported byte rates.
These router-reported rates are not independently measured and differ from `watch`'s observed
counter-delta rates.

Access types `DSL`, `Ethernet`, `X_AVM-DE_Fiber`, `X_AVM-DE_UMTS`, `X_AVM-DE_Cable`,
`X_AVM-DE_LTE`, and `unknown` are preserved. Legacy `POTS`, `Cable`, and `Other` remain
accepted for compatibility. Unrecognized non-empty access types and physical-link states
become `unknown`. Numeric values are strict unsigned integers, DNS values must be IP
addresses, and provider names are bounded to 128 bytes without control characters.
Missing required link properties or malformed advertised optional data fail atomically as
protocol errors. Missing or duplicate required services, or action-incomplete required
SCPDs, are explicit unsupported capabilities. SCPD and control URLs must remain on the router
origin. Provider and DNS are allowed output; WAN MAC addresses and PPP usernames are never
included. No credentials, generic SOAP surface, browser endpoint, mutation, or model-name
inference is used. Compatibility is backed by synthetic fixtures for IP/PPP connections,
common-service versions 1 and 2, legacy and documented access types, and optional actions
present or absent; neither original mismatch was verified on a device.

### Bounded WAN and traffic watch

`watch` is a read-only, non-interactive stream, not a daemon. Defaults are **6 samples with a 5s
interval**. `--count` accepts 1–3600 and `--interval` accepts Go durations from
1s through 1m (for example `1500ms`). Zero, negative, malformed, out-of-range,
and duplicate values are usage errors before any router request. These flags
are watch-only. There is no unbounded mode, no file output, and no background
poller.

The first read starts immediately. Subsequent reads wait the requested interval
after the preceding sample is written; reads never overlap or catch up in
bursts. Each complete sample has a 30s deadline, including discovery and
Digest authentication. Slow requests therefore lengthen the observation spacing;
the configured interval is not used as the rate denominator. No wait follows
the final sample. Ctrl-C or SIGTERM cancels the wait or in-flight read, emits a
structured `interrupted` error, and exits `130`; no further polling occurs.

Each sample rediscovers services and resolves the active WAN through documented
`Layer3Forwarding:GetDefaultConnectionService`, using the same identifier matching
as `forwards`. Ambiguous or absent services fail closed. It then reads only
`WANIPConnection`/`WANPPPConnection:GetStatusInfo` and the unique
`WANCommonInterfaceConfig:GetTotalBytesReceived`/`GetTotalBytesSent`. It does not
read router identity, external addresses, hosts, calls, or Wi-Fi data. No
mutation, browser endpoint, or model-based capability guess is used. Control
URLs stay on the router origin and redirects are refused.

`--json` is **JSONL**, one complete object per successful sample, in this field
order (there is no enclosing array or final summary):

```json
{"sample":1,"observed_at":"2026-01-02T03:04:05Z","wan_status":"connected","wan_uptime_seconds":100,"total_download_bytes":100,"total_upload_bytes":40,"download_delta_bytes":null,"upload_delta_bytes":null,"download_bytes_per_second":null,"upload_bytes_per_second":null}
```

Compact output uses one two-line TOON table per sample with a unique key
`sample_1[1]{observed_at,wan_status,...}:`, then `sample_2`, and so on. Columns
follow the JSON field order except that the sequence is in the key. Both formats
use exact integer byte totals and never fabricate a zero: unavailable numeric
values are explicit `null` in JSON and explicit `unknown` in compact output;
WAN state and unavailable observation time use `unknown`. Unknown router state strings are not echoed. `observed_at` is UTC
RFC 3339 with fractional seconds when needed, taken after the final counter
read. Reads are sequential observations, not an atomic router snapshot.

Deltas are nonnegative **observed counter increases**, and rates are their
floating-point bytes/second estimates using each direction's actual time between
completed reads (Go's monotonic clock when available). The first sample has no
derived values. Missing counters/times, nonpositive elapsed time, counter
decreases, source changes, non-connected/unknown WAN state, missing uptime, or
a decrease in WAN uptime suppress affected derived values. A counter decrease
is not guessed to be a wrap: the next sample starts from the new baseline.
These are not instantaneous link speeds or billing measurements. Resets or
multiple wraps that occur entirely between observations and leave a larger
counter cannot be detected; the CLI does not claim to reconstruct that traffic.

A failed first sample emits a terminal sanitized structured error record on stdout.
Any later failure retains earlier complete samples, then appends that final record
and terminates immediately; `--json` remains JSONL, so the terminal record is one
complete `{"error":...}` line. Consumers must check the exit status. There are no
skipped failures, partial samples, or polling retries (the normal Digest
challenge exchange is still allowed). Missing fields are null (`unknown` in compact output); malformed numeric
fields are protocol errors. Standard exits apply: `2` configuration/usage, `3`
auth, `4` network (including `tls_untrusted` and `watch_timeout`), `5` unsupported,
`6` router/protocol; output failure is `output_failed`, exit `1`. Error messages
discard router addresses, raw fault text, credentials, response bodies, and device
data. Existing `wan` and `traffic` output contracts are unchanged.

Tests use synthetic fixtures and controlled clocks. Watch is **not
hardware-validated**; live coverage remains explicitly opt-in and bounded.

### Observed lease metadata

`leases` is included since v0.2.0.
It reports **observations from the Hosts table**, including inactive remembered
hosts, not configured DHCP reservations or a DNS record inventory. It shares
`devices`' table reader without changing that command's behavior.

Only documented `Hosts:GetHostNumberOfEntries` and zero-based
`GetGenericHostEntry(NewIndex)` are called. The entry action returns
`NewHostName`, `NewIPAddress`, `NewMACAddress`, `NewAddressSource`,
`NewLeaseTimeRemaining`, `NewInterfaceType`, and `NewActive`:
[FRITZ! Hosts v31, §§2.1, 2.3, 3](https://fritz.support/resources/TR-064_Hosts.pdf).
[LANHostConfigManagement v9](https://fritz.support/resources/TR-064_LAN_Host_Config_Management.pdf)
provides DHCP server configuration, not a per-client reservation table.
Neither that service nor AVM host-list URLs are needed here. No browser
scraping, undocumented endpoints, DNS lookups, scanning, or mutations occur.

Compact columns are
`leases[N]{name,ip_address,mac_address,address_source,lease_time_remaining,interface_type,active}`.
JSON fields follow the same order, omitting absent names and remaining times:

```json
{"leases":[{"name":"synthetic-client","ip_address":"192.0.2.10","mac_address":"02:00:00:00:00:10","address_source":"DHCP","lease_time_remaining":3600,"interface_type":"Ethernet","active":true}],"total":1,"omitted":0}
```

`address_source` preserves `DHCP` and `Static`; absent or unrecognized sources
become `unknown`. **Static is the host entry's address source, not evidence of a
configured static DHCP reservation.** DHCP does not prove that an assignment
is unreserved. `active` is host connectivity, not lease validity. Names,
addresses, and interface types may be empty; the interface is the protocol's
classification, not a physical port.

`lease_time_remaining` is seconds only when the router supplies a meaningful
positive finite value. The documented type is signed 32-bit (`i4`). The CLI
conservatively exposes only `1–2147483646`; absent/empty values, zero, -1,
and the maximum-value forms `2147483647` and `4294967295` are omitted in JSON
and shown as `unknown` in compact output. This normalization does not assign
vendor-specific meaning to those values: none proves an expired lease,
permanent reservation, or countdown. Other malformed or out-of-range values
fail explicitly. Firmware returning no useful lease metadata still yields
honest host observations rather than fabricated leases.

Entries are sorted by MAC address (case-insensitive), then IP address and name.
Both formats show 100 entries by default, with `--all` for the complete inspected
list. JSON always includes `total` and `omitted`; compact output reports omitted
entries and suggests `leases --all`. Empty output is
`leases[0]: no host observations found` or
`{"leases":[],"total":0,"omitted":0}`. Missing Hosts support is unsupported,
never a successful empty result. The safety limit is 4096 host entries even
with `--all`.

Reads are atomic only at the output boundary: all indexed reads finish before
stdout is emitted, including those beyond the display bound. Any read or
validation failure discards the entire list. There are no partial-success
lists or retries. Reads are sequential, using one initial count; this is not a
router transaction. Concurrent additions, replacements, or reordering may go
undetected, and remembered data may already be stale.

Missing services or invalid-action faults exit `5` with service/firmware
remediation; authentication exits `3`, network failures `4`, malformed values
and other router faults `6`. Redirects are refused. Errors discard router
fault text, codes, URLs, and response bodies. Lease contents are local operational data shown only to
the invoking user, without a reveal flag: never paste real mappings, hostnames,
MACs, private addresses, or live responses into logs, issues, fixtures, or
commits. Tests use synthetic data only. Compatibility is fixture-backed, not
inferred from model names; these actions cannot establish reservation semantics.

### DHCP server configuration

`dhcp` is a bounded read-only view of server-level configuration from the
advertised `LANHostConfigManagement` service. It reports whether the server is
configurable, plus available enabled/relay state, address range, subnet mask,
router list, DNS server list, and domain name. It never enumerates, infers, or
claims configured reservations; `NewReservedAddresses` returned as part of a
documented aggregate response is discarded without being parsed or exposed.

The command requires exactly one advertised service and fetches that service's
same-origin SCPD before any SOAP action. Same-origin control and SCPD URLs must
have no user information, query, or fragment, and redirects are refused. The
SCPD must advertise the documented aggregate `GetInfo`, which is the only DHCP
action the command invokes. No setter, reserved-address action, host-table
action, browser endpoint, scan, DNS lookup, or model-name inference is used.

Compact output has this fixed shape:

```
dhcp:
  server_configurable: true
  server_enabled: true
  relay_enabled: false
  address_range_start: 192.0.2.20
  address_range_end: 192.0.2.200
  subnet_mask: 255.255.255.0
  routers: 192.0.2.1
  dns_servers: 192.0.2.1,192.0.2.53
  domain_name: synthetic.test
```

JSON fields follow the same order:

```json
{"server_configurable":true,"server_enabled":true,"relay_enabled":false,"address_range_start":"192.0.2.20","address_range_end":"192.0.2.200","subnet_mask":"255.255.255.0","routers":["192.0.2.1"],"dns_servers":["192.0.2.1","192.0.2.53"],"domain_name":"synthetic.test"}
```

Aggregate fields omitted by the router are `unknown` in compact output and
`null` or `[]` in JSON; they are never inferred. Returned booleans, IPv4 ranges,
contiguous subnet masks, routers, and DNS servers are validated before any
output, and any failure discards the entire result. Missing or duplicate services,
a missing `GetInfo` action, or an invalid-action fault `401` exit `5`;
authentication exits `3`, network failures `4`, and malformed configuration or
other router faults `6`. Errors discard router addresses, fault text, URLs, and
response bodies. Doctor reports service advertisement only and does not fetch
the SCPD or invoke a DHCP action; the `dsl` capability is likewise advertisement-only,
so doctor never fetches the WANDSLInterfaceConfig SCPD or invokes `X_AVM-DE_GetDSLInfo`.
The `firmware` capability likewise reports UserInterface advertisement only,
without fetching its SCPD or invoking either firmware read.
Compatibility is fixture-backed for aggregate
`GetInfo` and service versions 1 and 2; it is not inferred from router models.

### Firmware update status

`firmware` reports installed firmware and the router's existing update status using
only `UserInterface:GetInfo` and `UserInterface:X_AVM-DE_GetInfo`
([FRITZ! TR-064 UserInterface v25, §§2.1 and 2.8](https://fritz.support/resources/TR-064_User_Interface.pdf)).
Both actions have no input arguments and require no rights in the AVM document;
router authentication policy may still require the usual environment credentials.
The router must advertise exactly one UserInterface service, with both actions
in its SCPD, before either read is sent. Missing or ambiguous services/actions
return `unsupported` (exit 5); malformed responses return a structured protocol
error (exit 6).

```text
firmware:
  current_version: 8.00
  update_available: true
  offered_version: 8.10
  update_state: UpdateAvailable
  build_type: Release
  auto_update_mode: important
  update_time: 2026-01-30T03:00:00+01:00
  last_version: 7.90
  update_successful: succeeded
```

JSON has stable field order:

```json
{"current_version":"8.00","update_available":true,"offered_version":"8.10","update_state":"UpdateAvailable","build_type":"Release","auto_update_mode":"important","update_time":"2026-01-30T03:00:00+01:00","last_version":"7.90","update_successful":"succeeded"}
```

`offered_version` comes from GetInfo's `NewX_AVM-DE_Version`, while
`current_version` comes from X_AVM-DE_GetInfo's `NewX_AVM-DE_CurrentFwVersion`.
Update state, build type, auto-update mode, and update result preserve the router's
reported strings, including legacy `off` mode and future values. Missing/empty
fields and the documented unset update time `0000-00-00T00:00:00` are `unknown`
in text and `null` in JSON. Availability is never inferred from a version or
update state. Update time retains the router's reported timezone when present.
The command omits download/info URLs, password posture, warranty defaults, and
setup-assistant state. It never calls `X_AVM-DE_CheckUpdate`, `X_AVM-DE_DoUpdate`,
or `X_AVM-DE_SetConfig`: availability may reflect an earlier router check,
and running this command neither refreshes that check nor installs an update.

### DSL link diagnostics

`dsl` is a bounded read-only view of the DSL line from the single advertised
`WANDSLInterfaceConfig` service. It invokes only AVM's documented action
`X_AVM-DE_GetDSLInfo`
([FRITZ! TR-064 WANDSLInterfaceConfig v9, §3.4](https://fritz.support/resources/TR-064_WAN_DSL_Interface_Config.pdf));
no other action of that service, no `GetInfo`, no DSL statistics or reset action,
no browser endpoint, and no mutation is used.

The command requires exactly one advertised service and fetches that service's
same-origin SCPD before any SOAP action. Same-origin control and SCPD URLs must
have no user information, query, or fragment, and redirects are refused. The
SCPD must advertise `X_AVM-DE_GetDSLInfo`, which is the only DSL action invoked.

Compact output has this fixed shape:

```
dsl:
  link_status: Up
  modulation_type: VDSL
  current_profile: 17a
  upstream_current_kbps: 42000
  downstream_current_kbps: 250000
  upstream_max_kbps: 50000
  downstream_max_kbps: 300000
  upstream_noise_margin_tenth_db: 70
  downstream_noise_margin_tenth_db: 60
  upstream_attenuation_tenth_db: 120
  downstream_attenuation_tenth_db: 180
  fec_errors: 12
  crc_errors: 3
  atur_vendor: synthetic-vendor
  atur_country: DE
  upstream_power_tenth_dbm: 80
  downstream_power_tenth_dbm: 140
```

JSON fields follow the same order:

```json
{"link_status":"Up","modulation_type":"VDSL","current_profile":"17a","upstream_current_kbps":42000,"downstream_current_kbps":250000,"upstream_max_kbps":50000,"downstream_max_kbps":300000,"upstream_noise_margin_tenth_db":70,"downstream_noise_margin_tenth_db":60,"upstream_attenuation_tenth_db":120,"downstream_attenuation_tenth_db":180,"fec_errors":12,"crc_errors":3,"atur_vendor":"synthetic-vendor","atur_country":"DE","upstream_power_tenth_dbm":80,"downstream_power_tenth_dbm":140}
```

`link_status`, `modulation_type`, `current_profile`, the current and maximum
upstream/downstream rates in Kbps, the upstream/downstream noise margins and
attenuations (in 0.1 dB units), and the FEC and CRC counters are required and
validated before any output. Vendor and country strings and the upstream/downstream
power values (0.1 dBm units) are optional: absent fields are omitted in JSON and
shown as `unknown` in compact output, and a present but malformed value fails the
whole read. Documented link states are preserved; unrecognized non-empty states
become `unknown`. A router without a DSL interface (cable, fiber) advertises the
service on some models and fails the action read; missing or duplicate services,
the missing action, or an invalid-action fault `401` exit `5`, authentication
exits `3`, network failures `4`, and malformed values and other router faults `6`.
Errors discard router addresses, fault text, URLs, and response bodies. Doctor
reports service advertisement only and does not fetch the SCPD or invoke the DSL
action. Compatibility is fixture-backed for service versions 1 and 2, with and
without the optional vendor, country, and power fields; it is not inferred from
router models.

### Account rights and login posture

`account` reports only the current account's username and configured rights,
plus whether anonymous login, a default password, and second-factor
authentication are enabled. The command name follows the existing noun commands
such as `dsl` and `dhcp`. It never enumerates other users or retrieves passwords.

The command requires exactly one advertised `LANConfigSecurity` service and
one `X_AVM-DE_Auth` service. It validates same-origin service URLs and checks
both SCPDs before invoking any action; redirects are refused. It calls only
`LANConfigSecurity:X_AVM-DE_GetCurrentUser`, `GetInfo`,
`X_AVM-DE_GetAnonymousLogin`, and `X_AVM-DE_Auth:GetInfo`, all without input
arguments ([LANConfigSecurity v12, §§2.1–2.3 and 3.1](https://fritz.support/resources/TR-064_LAN_Config_Security.pdf),
[Authentication v5, §3.1](https://fritz.support/resources/TR-064_Authentication.pdf)).
It never calls `X_AVM-DE_GetUserList`, `GetState`, `SetConfig`, or a password action.

`GetCurrentUser` requires App, Dial, Phone, NAS, or Homeauto rights;
LANConfigSecurity `GetInfo` and `GetAnonymousLogin` require no rights, and
Auth `GetInfo` requires any right. Missing services or actions report
`unsupported_capability` (exit 5); HTTP authentication failures report
`authentication_failed` (exit 3). Malformed responses fail with a sanitized
structured protocol error (exit 6), without echoing router response data.
When the router denies `GetCurrentUser` with SOAP fault 606, the command fails
as a whole with `router_protocol_error` (exit 6) and a message that names the
required App, Dial, Phone, NAS, or Homeauto right.

Synthetic compact output:

```text
account:
  username: "synthetic-account"
  anonymous_login_enabled: false
  default_password_active: false
  second_factor_enabled: true
  rights[2]{path,access}:
    BoxAdmin,none
    NAS,readonly
```

The JSON field order is stable:

```json
{"username":"synthetic-account","rights":[{"path":"BoxAdmin","access":"none"},{"path":"NAS","access":"readonly"}],"anonymous_login_enabled":false,"default_password_active":false,"second_factor_enabled":true}
```

Rights list the documented paths `App`, `BoxAdmin`, `Phone`, `Dial`, `NAS`, and
`HomeAuto` first, in that order, with `none`, `readonly`, or `readwrite` access.
The AVM path list is an example, not an enumeration: any other path follows the
documented ones in router order when it starts with an ASCII letter and
continues with ASCII letters, digits, `_`, or `-`, up to 32 characters. The
command reports these paths as returned and does not infer permission for
`reboot`, `backup`, or any other action. A path outside that pattern, a
duplicate path, an unknown access value, or more than 32 rights fails closed
with a protocol error.

The current username can legitimately be empty, particularly with anonymous
login. Older responses may omit the rights list or the default-password flag:
these become `null` in JSON and `unknown` in compact output. An explicitly
empty rights list is `[]` in JSON and `0 configured rights reported` in compact
output. Required anonymous-login and second-factor flags must be valid booleans.
`default_password_active` describes whether at least one account has a default
password; it neither identifies that account nor reports its password.
`second_factor_enabled` is the configuration flag from Auth `GetInfo`, not
an active challenge state or proof that second-factor authentication is granted.
`doctor` reports only advertisement of both required services and performs no
account SCPD or account action reads.

### Wi-Fi radio mutation

`wifi enable|disable` is the first state-changing command. It uses only the documented
FRITZ! TR-064 [WLANConfiguration v48](https://fritz.support/resources/TR-064_WLAN_Configuration.pdf)
actions `GetInfo` (to read `NewEnable`) and `SetEnable` (to change it); no other mutating action,
undocumented endpoint, or browser scraping is used.

The mutation contract:

- Without `--confirm` the command reads the current state, prints a preview with the target
  instance and intended new state, changes nothing, and exits `0` with a `next:` suggestion.
  That suggestion repeats an explicit `--host`, so the confirmed run reaches the previewed router.
  It never relies on an interactive prompt.
- With `--confirm` the command re-reads the current state first, sends `SetEnable` only when the
  state differs, and re-reads `GetInfo` afterwards: success is reported only when the router
  confirms the new state. Already-enabled/already-disabled targets are successful results with
  `changed: false` (idempotent, no SOAP mutation sent).
- The target must be unambiguous. When the router advertises more than one WLANConfiguration
  instance, `--instance N` (the numeric suffix of `service_id`) is required and an
  `ambiguous_instance` usage error exits `2`; an unadvertised instance is `unknown_instance`.
- Result (machine-readable): compact `wifi:` block with `action`, `instance`, `previous`,
  `current`, `changed`, and a `next:` suggestion; JSON is
  `{"wifi":{"instance":...,"action":...,"previous":...,"current":...,"changed":...}}`.
  The preview JSON is
  `{"wifi":{"instance":...,"action":...,"current":...,"intended":...,"preview":true}}`
  and does not include `previous` or `changed`.
- Errors are structured on stdout with the standard exit codes (`2` usage, `3` auth, `4`
  network, `5` unsupported — including a router fault 401 on `SetEnable`, `6` protocol when
  the router did not confirm the state). A network failure after `SetEnable` was sent is
  reported as possibly-applied: the change may have reached the router without a response,
  and re-running the idempotent command is safe. Router fault text, codes, and URLs are discarded.
- A router may accept `SetEnable` without applying it (observed on a FRITZ!Box 6591
  Cable / FRITZ!OS 8.25 guest instance): the confirmation read then still reports the old
  state and the command exits `6` with the unconfirmed-state error. That is an honest
  capability boundary of the firmware, not a CLI success, and re-running is safe.
- SSIDs, BSSIDs, keys, and radio secrets are never requested or printed by the mutation path;
  it reads only the enable state.

`doctor` continues to report WLANConfiguration advertisement only; it does not invoke `SetEnable`
or verify mutation support.

### Router reboot

`reboot` uses only the documented
[FRITZ! DeviceConfig v11, §2.6](https://fritz.support/resources/TR-064_Device_Config.pdf)
`urn:dslforum-org:service:DeviceConfig:1` action `Reboot`, with **no input or
output arguments**, at the control URL from the TR-064 device description.
No factory reset, configuration write, undocumented endpoint, or browser
scraping is involved.

Without `--confirm`, discovery is read-only and the command exits `0` as a
**plan**, not a completed mutation. The compact preview identifies the selected
normalized endpoint, states that the router will restart and temporarily
interrupt all local services, and supplies the exact execute command. The
execute command pins `--host` even when selected through `ROUTER_AXI_HOST` or
the default; JSON previews also preserve `--json`. No interactive prompt occurs.

With `--confirm`, the client refuses ambiguous DeviceConfig targets and unsafe
control URLs, prepares Digest authentication through the documented read-only
`DeviceInfo:GetInfo` action when credentials are configured, and sends **exactly
one Reboot SOAP request**. Authentication challenges, redirects, and network
failures never cause the reboot request to be repeated. A router that does not
supply a reusable Digest challenge through the read may reject the single
request; this fails closed rather than retrying the mutation. Missing services
or invalid-action faults exit `5`, rejected authentication exits `3`, transport
failures exit `4`, and malformed responses or other router faults exit `6`.
Only a valid SOAP `RebootResponse` yields `accepted: true` and exit `0`.
Accepted means the router acknowledged the request, **not** that it restarted
or recovered. No polling or additional requests occur after the reboot POST.

The deterministic JSON distinction is:

```json
{"reboot":{"endpoint":"http://router.test:49000","preview":true,"effect":"the router will restart and temporarily interrupt all local services","execute":"router-axi reboot --host http://router.test:49000 --confirm --json"}}
{"reboot":{"endpoint":"http://router.test:49000","accepted":true,"recovery":"wait for the router to recover, then run router-axi doctor; do not automatically repeat reboot"}}
```

**Disruption and recovery:** reboot temporarily interrupts all local services,
including router access, Wi-Fi, internet connectivity, and telephony. Run it
only when that outage is acceptable and you have a way to regain local access.
Wait for the router to recover, reconnect if needed, then manually run
`router-axi doctor --host ADDRESS` against the selected endpoint. There is no
promised recovery time, automatic recovery check, or rollback.
**Reboot is not idempotent.** Each confirmed invocation can cause another
restart. A lost/malformed response can mean reboot was initiated without an
acknowledgement; the error reports that uncertainty, not success. Do not
automatically repeat the command after an error, timeout, or local output
failure. First check recovery manually and decide whether another reboot is
really needed.

Only normal preview/result output exposes the selected endpoint to the caller.
Errors discard router addresses, identifiers, credentials, fault text and SOAP
bodies. Reboot does not print serials, SSIDs, device data, or the contents of
its authentication read. Tests use synthetic servers only. No live reboot test
is provided or run; existing live-test flags cannot reboot a router. Reboot
acceptance and recovery are **not hardware-validated**.

### WAN reconnect

`wan reconnect` uses only the documented `ForceTermination` action of the
active WAN connection service, with **no input or output arguments**:
[FRITZ! WAN IP Connection v7, §1.7](https://fritz.support/resources/TR-064_WAN_IP_Connection.pdf)
for `urn:dslforum-org:service:WANIPConnection:1` and
[FRITZ! WAN PPP Connection v16, §1.11](https://fritz.support/resources/TR-064_WAN_PPP_Connection.pdf)
for `urn:dslforum-org:service:WANPPPConnection:1`. Neither document requires
`RequestConnection` to restore the connection, and the command never sends it.
Both documents name `Layer3Forwarding:GetDefaultConnectionService` as the way
to determine which of the two services is active.

Without `--confirm`, the command only reads and exits `0` as a **plan**: it
selects the target, then prints the selected normalized endpoint, the effect
(the internet connection will drop and a new external address may be
assigned), and the exact execute command. The execute command pins `--host`;
JSON previews also preserve `--json`. No interactive prompt occurs.

Target selection is the same with and without `--confirm` and fails closed
before any mutation. The router must advertise exactly one
`Layer3Forwarding:1` service whose service description lists
`GetDefaultConnectionService`; that read must name exactly one advertised
`WANIPConnection:1` or `WANPPPConnection:1` service (identifier matching as in
[`forwards`](#port-forward-inspection)); and that service's description must
list `ForceTermination`. Control and service-description URLs must stay on the
router origin without user information, query, or fragment, and redirects are
refused.

With `--confirm`, the client sends **exactly one ForceTermination SOAP
request**. When credentials are configured, the Digest challenge comes from
the `GetDefaultConnectionService` read; a router that does not supply a
reusable challenge may reject the single request, which fails closed rather
than retrying. Authentication challenges, redirects, and network failures
never cause the request to be repeated. Missing or ambiguous services and
invalid-action faults exit `5`, rejected authentication exits `3`, transport
failures exit `4`, and malformed responses or other router faults exit `6`.
Only a valid SOAP `ForceTerminationResponse` of the selected service yields
`accepted: true` and exit `0`. Accepted means the router acknowledged the
request, **not** that the connection dropped or returned. No polling or
additional requests occur after the ForceTermination POST.

```json
{"wan_reconnect":{"endpoint":"http://router.test:49000","preview":true,"effect":"the internet connection will drop and a new external address may be assigned","execute":"router-axi wan reconnect --host http://router.test:49000 --confirm --json"}}
{"wan_reconnect":{"endpoint":"http://router.test:49000","accepted":true,"recovery":"wait for the connection to return, then run router-axi wan; do not automatically repeat wan reconnect"}}
```

**WAN reconnect is not idempotent.** Each confirmed invocation can drop the
connection again. A lost or malformed response can mean the termination was
initiated without an acknowledgement; the error reports that uncertainty, not
success. Do not automatically repeat the command after an error, timeout, or
local output failure. Wait, then run `router-axi wan` manually. There is no
promised recovery time, automatic recovery check, or rollback.

Only normal preview/result output exposes the selected endpoint. Errors
discard router addresses, identifiers, credentials, fault text and SOAP
bodies, and the command never prints the external address. `doctor` does not
report a separate `wan reconnect` capability. Tests use synthetic servers
only. No live WAN reconnect test is provided or run; existing live-test flags
cannot reconnect a router. Acceptance and recovery are **not
hardware-validated**.

### Configuration backup

`backup` uses only the documented
[FRITZ! DeviceConfig v11, §2.8](https://fritz.support/resources/TR-064_Device_Config.pdf)
action `urn:dslforum-org:service:DeviceConfig:1`
`X_AVM-DE_GetConfigFile`, with the input argument `NewX_AVM-DE_Password` and the
output argument `NewX_AVM-DE_ConfigFileUrl`, followed by a download of that
returned URL. Like every other documented read, the action and the download use
the standard TR-064 Digest handshake when credentials are configured. The
action requires configuration rights. No factory reset, no
`X_AVM-DE_SetConfigFile`, no configuration upload, no browser scraping, and no
undocumented endpoint is used; `backup` never modifies the router.

The **export passphrase is read only from `ROUTER_AXI_BACKUP_PASSWORD`**. It is
never accepted as an argument, never read from `ROUTER_AXI_PASSWORD` or any
other variable, and never echoed in output, errors, or JSON. The passphrase is
required to restore the export, so keep it with the file. The router login
credentials keep their existing `ROUTER_AXI_USERNAME` and
`ROUTER_AXI_PASSWORD` variables and are used only for the documented TR-064
authentication.

`backup` **requires an HTTPS router origin**, because the action request
carries the export passphrase in its SOAP body and Digest authentication does
not protect that body. Pass `--host https://fritz.box:49443` (or set
`ROUTER_AXI_HOST` to an `https://` origin) and trust the router certificate on
the local system. A plaintext origin, including the default `fritz.box`, is
refused with `backup_requires_https` (exit `2`) before any request is sent.

The download URL is a one-time router-generated address that is valid for less
than 30 seconds and is never printed or logged. AVM's DeviceConfig reference
requires the URL to be HTTPS secured with the TR-064 certificate, while the
Remote Access reference shows an HTTP example URL; the CLI follows the
stricter document and **refuses plaintext downloads**, redirects at every
stage, and URLs that are not on the router host, carry user information, or
carry a query or fragment. Certificate verification is never skipped: a router
whose certificate is not trusted by the local system fails closed with the
`tls_untrusted` remediation instead of downloading the export in the clear.

The local file contract:

- `--output PATH` is required and names the destination explicitly; the export
  is never written to a default location, `stdout`, or a temporary directory.
- The destination is validated before the router is contacted. A path that
  names a directory, ends in a path separator, or has no existing parent
  directory is a usage error (exit `2`).
- The write is atomic: the export is written to an owner-only (`0600`)
  temporary file in the destination directory, flushed, then linked into
  place. A failed export leaves no file and no temporary file behind.
- An existing file is never overwritten without `--force`. Without `--force`
  the final write is an atomic no-replace operation, so even a file that
  appears between the preflight check and the write survives untouched.
  That operation is a hard link; a filesystem without hard-link support
  (for example FAT/exFAT or some network mounts) fails with
  `backup_link_unsupported`, and `--force` writes with an atomic rename
  instead.
- Output is metadata only: compact `path`, `bytes`, and `sha256`, or
  `{"backup":{"path":"…","bytes":…,"sha256":"…"}}` in JSON. The export
  contents, the passphrase, and the download URL never appear in output,
  errors, or logs.

Missing or duplicate `DeviceConfig` services, an unsafe control URL, and an
invalid-action fault exit `5` with service/firmware remediation; rejected
credentials exit `3`; transport failures `4`; malformed responses, refused
HTTPS URLs, and other router faults `6`; unusable destinations, a plaintext
router origin, and a missing passphrase exit `2`; local write failures,
including `backup_link_unsupported`, exit `1`. An untrusted router
certificate exits `4` with the code `tls_untrusted`.

Backup failures below have stable structured codes and hints in both compact
and JSON output, all with exit `6`:

| Code | Meaning and hint |
| --- | --- |
| `backup_action_rejected` | The router rejected the export action despite advertising DeviceConfig; check supported firmware or use the FRITZ!Box web interface. |
| `backup_invalid_response` | The export response was malformed or unexpected; check supported firmware. |
| `backup_unsafe_download_url` | The returned download address failed HTTPS or same-host safety validation; export was refused. |
| `backup_download_rejected` | The download was rejected with HTTP status 400 or higher, except authentication failures; check supported firmware or use the FRITZ!Box web interface. |
| `backup_export_too_large` | The export exceeded the supported 64 MiB safety limit. |

These diagnostics suppress SOAP fault details, URLs, passphrases, and export
contents. Other router/protocol failures retain `router_protocol_error`.
Compatibility is fixture-backed with synthetic servers only;
the FRITZ!Box export flow is **not hardware-validated**, and `backup` does not
verify that the export can be restored.

### Wi-Fi inspection

`wifi` enumerates every advertised WLANConfiguration service instance, including
logical guest access points; an instance is not necessarily a physical radio.
It returns all instances, sorted by the numeric suffix of their advertised
`service_id`. Identifiers are stable while the router's service description
remains unchanged, not hardware identities. Missing, invalid, or duplicate
identifiers fail explicitly rather than risk misidentifying an instance.

Four documented read actions are invoked per instance, in this order:
`GetInfo` (`NewEnable`, `NewSSID`, `NewStandard`, optional
`NewX_AVM-DE_FrequencyBand`), `GetChannelInfo` (`NewChannel`),
`GetTotalAssociations` (`NewTotalAssociations`), and `GetBeaconType`
(`NewBeaconType`). These actions and response fields are documented in
[FRITZ! TR-064 WLANConfiguration, version 48](https://fritz.support/resources/TR-064_WLAN_Configuration.pdf),
sections 2.2, 2.15, 2.19, 2.13 and 3.

The SSID is public beacon data, broadcast to every nearby device, and is
reported in normal output; there is deliberately no reveal flag because
nothing in the output is a secret. The BSSID that `GetInfo` also returns is
discarded and never emitted, and keys and passphrases are never retrieved:
`GetSecurityKeys`, `X_AVM-DE_GetWLANHybridMode`, associated-device actions,
and every other key- or client-returning action are never called. No browser
or undocumented endpoint is used.

Compact output has the ordered columns
`radios[N]{service_id,ssid,enabled,channel,band,standard,associated_devices,security_mode}`.
JSON uses the same ordered fields:

```json
{"radios":[{"service_id":"urn:WLANConfiguration-com:serviceId:WLANConfiguration1","ssid":"synthetic-ap","enabled":true,"channel":6,"band":"2400","standard":"ax","associated_devices":2,"security_mode":"11i"}],"total":1}
```

`enabled` is the protocol's enable state from `GetInfo` (`0`/`1`, strictly
parsed), `channel` is the protocol's unsigned byte from `GetChannelInfo`
(0 means auto-channel), and `associated_devices` is its unsigned 16-bit
association count, not a client list. `band` is `2400`, `5000`, `6000`, or
`unknown` from the `GetInfo` frequency-band extension; absent extensions and
unrecognized values remain `unknown`, never inferred from channel or model.
`standard` is the highest active mode from `GetInfo` (`b`, `g`, `n`, `ac`,
`ax`, `be`), or `unknown`. `security_mode` preserves documented beacon values
(`None`, `Basic`, `WPA`, `11i`, `WPAand11i`, `WPA3`, `11iandWPA3`, `OWE`,
`OWETrans`); unrecognized values become `unknown`. It is not a security audit
or a passphrase check.

Reads are atomic at the command boundary: any instance or action failure emits a
sanitized structured error on stdout with a nonzero exit code, without result data.
Missing WLANConfiguration support exits `5` with remediation; invalid required
values and router faults exit `6`, authentication failure `3`, and network
failure `4`. No partial list is reported as success. Reads are sequential, not
a simultaneous snapshot. The empty result schema is
`radios[0]: no Wi-Fi services found` or `{"radios":[],"total":0}`; absent
service advertisement is unsupported, **not** a successful empty result.
Doctor reports advertisement only, with explicit unsupported remediation; it
does not probe these actions. Firmware must support all four reads and the
documented service identifiers; routers whose `GetInfo` omits the
frequency-band extension report `band: unknown`. Compatibility is
fixture-backed, not inferred from a model name.

### Per-radio Wi-Fi detail

`wifi detail` is the bounded read-only companion to the `wifi` list: it reports
one identified radio's safe documented properties. It accepts the same
`--instance N` selection as `wifi enable|disable` (required when the router
advertises more than one WLANConfiguration instance) and resolves the target
before any request is sent.

Like `wan detail`, the command validates the target service's SCPD first: the
service description must advertise the documented `GetInfo` and
`GetChannelInfo` actions before either is invoked. Missing advertisement exits
`5` with remediation and no action request is sent.

Only safe fields are read from the two documented responses: enable status
(`NewEnable`, strictly parsed), status (`NewStatus`), standard
(`NewStandard`), max bitrate (`NewMaxBitRate`), channel (`NewChannel`), and
band (`NewX_AVM-DE_FrequencyBand`). Optional fields the router omits are
`unknown` in compact output and JSON `null`; channel and band follow the same
whitelist rules as the `wifi` list, and unrecognized optional values never
fail the command. The BSSID that `GetInfo` also returns is discarded and never
emitted; `GetSecurityKeys`, `X_AVM-DE_GetWLANHybridMode`, and associated-device
actions are never called.

Compact output is:

```
wifi_detail:
  instance: urn:WLANConfiguration-com:serviceId:WLANConfiguration2
  enabled: true
  status: Up
  standard: ax
  max_bit_rate: Auto
  channel: 36
  band: 5000
```

JSON uses the same deterministic field order:

```json
{"service_id":"urn:WLANConfiguration-com:serviceId:WLANConfiguration2","enabled":true,"status":"Up","standard":"ax","max_bit_rate":"Auto","channel":36,"band":"5000"}
```

Errors are the standard structured errors: `ambiguous_instance` and
`unknown_instance` exit `2`, unsupported actions `5`, authentication `3`,
network `4`, and invalid required values `6`. Compatibility is fixture-backed,
including multi-radio routers and guest access-point instances, not inferred
from a model name.

### Guest Wi-Fi inspection

`guest` is a distinct top-level read command because the existing CLI grammar has
single-word inspection commands; `wifi` accepts only the `detail` inspection
action and the established mutation
actions `enable|disable`. It enumerates every advertised WLANConfiguration service
and calls the documented AVM action `X_AVM-DE_GetWLANExtInfo` on each one. An
instance is a guest network only when `NewX_AVM-DE_APType` is exactly `guest`;
`normal` is not selected. Service-instance numbers and SSIDs never determine the
role. The contract is documented in
[FRITZ! WLANConfiguration v48, pp. 12 and 16](https://fritz.support/resources/TR-064_WLAN_Configuration.pdf),
which defines `X_AVM-DE_APType` values `normal` and `guest`.

Classification of all advertised WLAN instances completes before guest details are
read. Each explicit guest is then read with the same documented actions as `wifi`:
`GetInfo`, `GetChannelInfo`, `GetTotalAssociations`, and `GetBeaconType`. Compact
output is
`guests[N]{service_id,ssid,enabled,channel,band,standard,associated_clients,security_mode}`;
JSON uses the same ordered fields under `guests` followed by `total`. The count in
the compact header and JSON `total` makes zero, one, and multiple explicit. AVM's
current mapping documents zero or one logical guest service, whose position may be
service 2, 3, or 4; the CLI does not assume that limit and reports every instance
that explicitly identifies itself as `guest`, in advertised numeric service-ID
order. An empty, completely classified result is
`guests[0]: no guest Wi-Fi networks found` or `{"guests":[],"total":0}`.

`enabled` and the public broadcast `ssid` come from `GetInfo`; channel, band,
standard, per-instance associated-client count, and security mode have the same
normalization as `wifi`. Standard is only the highest active mode documented by
AVM, not the full supported-mode set. Channel `0` means automatic selection;
missing or unrecognized documented extensions become `unknown` only where the
`wifi` contract already permits that (`band` and `standard`). Associated clients is
a count, never a client list.

The guest discriminator is AVM-specific; generic TR-064 defines multiple SSID
instances but no guest role. If any WLAN instance lacks the discriminator, omits or
returns an unknown AP type, or a selected guest lacks any required status action,
the command fails atomically instead of returning an incomplete list. Invalid-action
faults are `unsupported_capability` (exit `5`); authentication, network, and
protocol errors retain the standard exits. There is no positional, SSID, browser,
model-name, or undocumented fallback. Doctor cannot prove this complete action set
from the device description alone, so it intentionally has no separate guest
capability.

SSID is the only network identity returned. `X_AVM-DE_GetWLANExtInfo` may return
other fields, but the client retains only AP type. It never retrieves or outputs
passphrases, security keys, BSSID, client MAC addresses, associated-device records,
or client device/IP details. Tests use synthetic fixtures; hardware guest
inspection is not validated unless the bounded opt-in live test is run locally.
No guest enable/disable command or other mutation is provided.

### Port-forward inspection

`forwards` reads all mappings, including disabled ones, from the router’s active WAN
service, resolved through the documented
`Layer3Forwarding:GetDefaultConnectionService` action, which returns the
default connection’s service identifier. FRITZ! routers return that identifier
either as the advertised `serviceId`, the advertised service `type`, or a
UPnP-style identifier (`urn:upnp-org:serviceId:WANIPConnection1`,
`uuid:…:WANIPConnection.1`, or the dot-separated `1.WANIPConnection.1`
that FRITZ!OS returns); `forwards` accepts any of these shapes and
matches the one advertised WAN service of the same family and instance.
An empty identifier, or one that names no advertised WAN service or more than
one (for example a bare service type shared by two `WANIPConnection`
instances), is unsupported; inactive instances are never contacted.
That instance, `WANIPConnection` or
`WANPPPConnection`, must advertise both `GetPortMappingNumberOfEntries` and
`GetGenericPortMappingEntry` in its SCPD.
The first returns `NewPortMappingNumberOfEntries` (unsigned 16-bit count);
the second accepts the zero-based `NewPortMappingIndex` and returns
`NewEnabled`, `NewProtocol`, `NewExternalPort`, `NewInternalClient`,
`NewInternalPort`, `NewPortMappingDescription`, `NewRemoteHost`, and
`NewLeaseDuration`. The documented contracts are in the FRITZ!
[WAN IP Connection v6, §§1.11–1.12](https://fritz.support/resources/TR-064_WAN_IP_Connection.pdf)
and [WAN PPP Connection v15, §§1.15–1.16](https://fritz.support/resources/TR-064_WAN_PPP_Connection.pdf)
references. Zero-based indexing and fault `713` are specified in
[Broadband Forum TR-064, §2.4.14](https://www.broadband-forum.org/pdfs/tr-064-1-0-1.pdf);
`GetDefaultConnectionService` is specified in the FRITZ!
[TR-064 Layer3Forwarding](https://fritz.support/resources/TR-064_Layer_3_Forwarding.pdf)
reference. Only these documented read actions are called; `AddPortMapping`,
`DeletePortMapping`, and every other mutating action are never invoked.
There is no browser scraping, undocumented endpoint, or IGD fallback.

Compact columns and JSON fields are ordered as below; the internal target is
returned verbatim, without DNS resolution or network scanning:

```json
{"forwards":[{"enabled":true,"protocol":"TCP","external_port":8443,"internal_client":"192.0.2.10","internal_port":443,"description":"synthetic-service","remote_host":"","lease_duration":0}],"total":1,"omitted":0}
```

Protocols are `TCP` or `UDP`; ports are in `1–65535`. An empty
`remote_host` means no remote-host restriction. Lease duration is seconds
(unsigned 32-bit); zero means permanent. If the router omits the lease field,
JSON omits `lease_duration` and compact output uses `unknown`, not zero.
Enabled states accept `0`/`1` or `false`/`true`. A missing internal target,
missing remote-host field, or invalid numeric, enabled, or protocol value fails
explicitly.
Descriptions and targets are local operational data, not secrets: they are
shown to the invoking user, with no reveal flag. Do not paste real output into
logs, issues, fixtures, or commits. The client does not log mapping contents,
and errors discard router fault text, fault codes, URLs, and response bodies.
SCPD and control URLs must stay on the router origin; redirects are refused.

Results sort by protocol, numeric external port, remote host, internal target,
numeric internal port, enabled state (false first), description, then lease
(absent before present). Identical records are retained; service IDs and
transient table indexes are not exposed. Output is limited to
100 entries by default. `--all` returns the complete inspected list; compact
output reports `omitted` and suggests `forwards --all`, while JSON always
includes `total` and `omitted`. Empty output is
`forwards[0]: no port forwards found` or
`{"forwards":[],"total":0,"omitted":0}`. No advertised service or a missing
required action is **unsupported**, never a successful empty list.

Enumeration is atomic only at the output boundary: all indexes are read before
any stdout is emitted, including entries beyond the default display limit.
An indexed fault (including a vanished index), any other read failure, or a
changed count on the final count read discards the whole result. Reads are
sequential, with no management lock or router transaction: equal counts cannot detect replacements
or reordering during the read, and changes after the final count read are not
detected. There are no retries or partial-success lists. The safety limit is
4096 total entries, even with `--all`.

Missing services/SCPDs/actions or an invalid-action SOAP fault exit `5` with
`unsupported_capability` and firmware/service remediation. Authentication exits
`3`, network failures `4`, malformed responses and other router faults `6`.
Doctor reports service advertisement only, not confirmed action support.
Compatibility is fixture-backed for active IP and PPP services, selection by
service type and identifier, empty tables, and absent lease fields; it is not
inferred from model names.
Only the active WAN service is enumerated. Routers commonly advertise both
`WANIPConnection` and `WANPPPConnection` while only one is active, and an
inactive instance can reject these documented actions, so advertisement of
both services does not imply both are readable. If `GetDefaultConnectionService`
is unavailable or names a service without both actions, `forwards` exits `5`
with remediation; there is no fallback to another instance.
This table is not a firewall audit: IPv6 pinholes, exposed-host settings, or
rules unavailable through these documented actions are outside its scope.

`overview` reads router identity, active WAN state, and traffic totals in that fixed
order. It is atomic: if any read fails, the command emits the failed operation as
a structured error on stdout with its normal non-zero
exit code. It never presents a partial overview as successful. JSON output has
stable `router`, `wan`, and `traffic` objects in that order.

The router defaults to `http://fritz.box:49000`. Override it with `--host ADDRESS` or
`ROUTER_AXI_HOST`; an address without a port uses TR-064 port `49000` for HTTP or
`49443` for HTTPS. Credentials are read only from the environment. The default
output is compact AXI text; `--json` emits JSON on stdout. All structured data and
errors use stdout; stderr is reserved for diagnostics, currently including watch's
`output_failed` fallback after stdout itself fails. `calls` returns at most 100 entries by default,
reports the omitted count, and accepts `--all` for the complete list.

If the router client cannot be constructed, every router-facing command
returns `invalid_configuration` with the same endpoint guidance: use an HTTP
or HTTPS host without userinfo, a query, a fragment, or a non-root path.
The underlying factory error and supplied endpoint are never included in
compact or JSON output.

`version` prints the CLI version without contacting the router. The bare `--version`, `-v`,
and `-V` aliases print only the version and must be used without other arguments. Compact
`status` output begins with the executable path and CLI description so callers can identify
which tool produced the report. When an option is unknown or invalid for a command, the
usage error includes the valid flags for that command or action form.

#### Exit codes

router-axi extends the common CLI convention that `0` means success, `1` an
internal error, and `2` a usage error with three additional router-domain
codes. A consumer only needs the rule that **any non-zero exit code means
failure**; the code classifies why it failed so agents can branch without
parsing output text:

| Code | Meaning |
| ---- | ------- |
| `0`  | Success, including an already-satisfied `wifi enable\|disable` or a reboot or `wan reconnect` preview. |
| `1`  | Internal failure, such as a local output or backup-file write error. |
| `2`  | Usage or configuration error (invalid arguments, missing `--output`, missing passphrase, ambiguous instance). |
| `3`  | Authentication failure; the router rejected the credentials from `ROUTER_AXI_USERNAME`/`ROUTER_AXI_PASSWORD`. |
| `4`  | Network failure; the router is unreachable, a request timed out, or its HTTPS certificate failed verification (`tls_untrusted`). |
| `5`  | Unsupported capability; the router does not advertise or implement the required service or action (`unsupported_capability`). |
| `6`  | Router or protocol error; the router responded with a fault, malformed data, or did not confirm a requested state. |

`watch` cancellation through Ctrl-C/SIGTERM exits `130`, the conventional
SIGINT/interrupted status. Its terminal structured error is the final stdout
record in the selected format; exit codes stay authoritative. If writing stdout
fails, watch instead emits its sanitized `output_failed` diagnostic on stderr.

The implementation discovers services through `/tr64desc.xml` and invokes only
read actions, plus the confirmed `wifi enable|disable`, `wan reconnect`, and `reboot`
mutations described above, which invoke only the documented `WLANConfiguration:SetEnable`,
`WANIPConnection` or `WANPPPConnection:ForceTermination`, and `DeviceConfig:Reboot`
respectively, and the documented
`DeviceConfig:X_AVM-DE_GetConfigFile` export described above.
Doctor uses only `DeviceInfo:GetInfo`; inspection commands use
`DeviceInfo:GetInfo`, `WANIPConnection` or
`WANPPPConnection:GetStatusInfo` and `GetExternalIPAddress`,
`WANCommonInterfaceConfig:GetTotalBytesReceived`, `GetTotalBytesSent`, and the explicit
`wan detail` actions `GetCommonLinkProperties`, `X_AVM-DE_GetAddonInfos`,
`X_AVM-DE_GetOnlineMonitor`, `X_AVM-DE_GetActiveProvider`, `GetTotalPacketsSent`, and
`GetTotalPacketsReceived` on WANCommonInterfaceConfig plus SCPD-advertised
`X_GetDNSServers` on the active WANIPConnection/WANPPPConnection service, AVM's
documented `X_AVM-DE_OnTel:GetCallList`, and the standard
`Hosts:GetHostNumberOfEntries` plus zero-based `GetGenericHostEntry(NewIndex)`,
the SCPD-advertised documented `Hosts:X_AVM-DE_GetSpecificHostEntryByIP` read of
`devices detail`,
the SCPD-advertised documented `LANHostConfigManagement:GetInfo` DHCP
configuration read (never reservation inventory), the SCPD-advertised documented
`WANDSLInterfaceConfig:X_AVM-DE_GetDSLInfo` DSL link read, the SCPD-advertised
`UserInterface:GetInfo` and `X_AVM-DE_GetInfo` firmware status reads, the four documented
account reads listed above, and `WLANConfiguration:GetInfo`,
`GetChannelInfo`, `GetTotalAssociations`, and
`GetBeaconType`. Guest inspection additionally uses the documented AVM
`WLANConfiguration:X_AVM-DE_GetWLANExtInfo` action only to read
`NewX_AVM-DE_APType`, then the same four status actions for explicitly classified
guest instances. Commands that need an active WAN (`wan`, `overview`, `watch`,
and `forwards`) use `Layer3Forwarding:GetDefaultConnectionService`; `forwards`
then uses the active `WANIPConnection`/`WANPPPConnection`:
`GetPortMappingNumberOfEntries` and `GetGenericPortMappingEntry(NewPortMappingIndex)`
when advertised in their SCPDs. Wi-Fi inspection does not call `GetSecurityKeys` or any other
key- or client-returning WLAN action.
Call-list URLs are accepted only from the same router origin. Device and lease inspection
do not use AVM host-list URLs, browser scraping, or network scanning.

## Development

Setup, checks, and the contribution workflow are in
[CONTRIBUTING.md](CONTRIBUTING.md). Release maintainers should follow
[RELEASING.md](RELEASING.md). Changes are submitted through the
[no-mistakes](https://github.com/kunchenguid/no-mistakes) gate, which validates
a feature branch and opens the pull request after the configured checks pass.

### Opt-in live router tests

The default live suite invokes only the read actions listed above. It is skipped unless
`ROUTER_AXI_LIVE_TEST` is exactly `1`, an explicit host and both credentials are
set, and `CI` is empty. Run it locally without placing credentials on the
command line:

```sh
export ROUTER_AXI_HOST='fritz.box'
read -rs 'ROUTER_AXI_USERNAME?Router username: '; export ROUTER_AXI_USERNAME; printf '\n'
read -rs 'ROUTER_AXI_PASSWORD?Router password: '; export ROUTER_AXI_PASSWORD; printf '\n'
ROUTER_AXI_LIVE_TEST=1 go test ./internal/tr064 -run '^(TestLiveReadOnlyCommands|TestLiveWiFi|TestLiveGuestWiFi|TestLiveForwards|TestLiveDSL|TestLiveLeases)$' -count=1
unset ROUTER_AXI_PASSWORD ROUTER_AXI_USERNAME ROUTER_AXI_HOST
```

Watch snapshot coverage is also available as `TestLiveWatchSnapshot` under the
same live gate. It performs at most two read-only snapshots with a 30s overall
deadline, discards their values, and never writes files. Run without `-v`:

```sh
ROUTER_AXI_LIVE_TEST=1 go test ./internal/tr064 -run '^TestLiveWatchSnapshot$' -count=1
```

Do not add `-v`: the test deliberately reports only command-level failures and
never logs responses, credentials, serial numbers, phone, device, radio, or
network data, mapping contents, lease observations, or router addresses.
`TestLiveLeases` checks only the observation schema and finite-time bounds;
missing capability is skipped explicitly. It neither creates reservations nor
claims to validate reservation semantics or countdown accuracy. The forwards live test
resolves the active WAN service and uses only the two enumeration actions; it
does not create test mappings;
unsupported enumeration is skipped explicitly, not counted as hardware mapping
validation. DSL live coverage makes exactly one documented
`WANDSLInterfaceConfig:X_AVM-DE_GetDSLInfo` query, discards the response, and
skips explicitly when cable or fiber hardware does not support that DSL action.
Wi-Fi live reads use only the four actions
above, including `GetInfo`, and never any key-returning action; missing
advertisement is checked as unsupported. Guest live coverage adds the documented
AP-type action, has a 30-second deadline, discards returned values, never logs them,
and skips when complete documented support is unavailable. Ordinary `go test ./...`
and all CI environments cannot enable the live test.

Live mutation coverage is additionally opt-in and skipped by default: it requires the
complete live gate above plus `ROUTER_AXI_LIVE_MUTATION_TEST=1`. This enables only
Wi-Fi coverage, never reboot or WAN reconnect. There is no live reboot or WAN
reconnect test. It targets the only
WLANConfiguration instance, or the instance named in `ROUTER_AXI_WIFI_INSTANCE`; with
several instances and no explicit instance it is skipped. The test reads the current
enable state, toggles the radio with `SetEnable`, restores the original state, and
requires the router to confirm both changes. It never logs SSIDs or radio data and
skips explicitly when `SetEnable` is unsupported.

```sh
ROUTER_AXI_LIVE_TEST=1 ROUTER_AXI_LIVE_MUTATION_TEST=1 \
  go test ./internal/tr064 -run 'TestLiveWiFiMutation$' -count=1
```

Warning: `TestLiveWiFiMutation` briefly toggles the selected radio off and on. Run it only
from a host connected to the router over wired LAN — if the test host reaches the router
through the radio being toggled, the connection drops mid-test and neither verification nor
restore can reach the router. The test reports only pass/fail; hardware validation of the
change direction is only meaningful when the router confirms both changes.

Live backup coverage is additionally opt-in and skipped by default: it requires the
complete live gate above plus `ROUTER_AXI_LIVE_BACKUP_TEST=1` and an exported
`ROUTER_AXI_BACKUP_PASSWORD`. The test invokes the documented
`DeviceConfig:X_AVM-DE_GetConfigFile` action and its one-time HTTPS download,
then **discards the payload**: it never writes a backup file anywhere and never
logs the export, the download URL, or the passphrase. It reports only
pass/fail, and skips explicitly when `ROUTER_AXI_HOST` is not an `https://`
origin, the action is unsupported, or the router certificate is not trusted
locally; the ordinary live-read and mutation flags
can never trigger an export.

```sh
export ROUTER_AXI_BACKUP_PASSWORD='export-passphrase'
ROUTER_AXI_LIVE_TEST=1 ROUTER_AXI_LIVE_BACKUP_TEST=1 \
  go test ./internal/tr064 -run 'TestLiveBackup$' -count=1
unset ROUTER_AXI_BACKUP_PASSWORD
```

## License

[MIT](LICENSE)

# router-axi

An agent-ergonomic CLI for inspecting and operating supported home routers.

The read-only MVP provides device information, WAN status, traffic statistics, call-list access, connected-device, observed lease metadata, Wi-Fi, and port-forward inspection through documented FRITZ!Box TR-064 interfaces.

## Design constraints

- Compact AXI output by default, with JSON available explicitly.
- Non-interactive commands, structured errors, and meaningful exit codes.
- Read-only behavior by default.
- Explicit confirmation for disruptive operations.
- Local operation without telemetry or a hosted account.

See [VISION.md](VISION.md) for the acceptance policy. Contributors should read
[CONTRIBUTING.md](CONTRIBUTING.md); security reports belong in
[SECURITY.md](SECURITY.md).

## Installation

Install v0.1.0 with Go:

```sh
go install github.com/Azd325/router-axi/cmd/router-axi@v0.1.0
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
router-axi doctor
router-axi doctor --json
router-axi status
router-axi overview
router-axi wan
router-axi traffic
router-axi calls
router-axi devices
router-axi devices --json
router-axi leases
router-axi leases --json
router-axi leases --all
router-axi wifi
router-axi wifi --json
router-axi wifi enable
router-axi wifi disable
router-axi wifi disable --instance 1 --confirm
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

`doctor` performs one bounded diagnosis: it fetches the TR-064 device
description once and, when `DeviceInfo` is advertised, invokes only
`DeviceInfo:GetInfo` to verify authentication and obtain model and firmware.
It reports endpoint reachability, TR-064 availability, authentication, and
whether the router advertises the services required by `status`, `overview`,
`wan`, `traffic`, `calls`, `devices`, `leases`, `wifi`, and `forwards`. For leases,
Hosts advertisement is a candidate capability, not proof of meaningful lease
metadata; doctor does not read the host table. For forwards,
WANIPConnection or WANPPPConnection service advertisement is only a candidate
capability: doctor does not invoke Layer3Forwarding, fetch SCPDs, or verify
enumeration actions. It does
not invoke those commands, retrieve a WAN
address or call list, infer support from a model name, or emit serial numbers,
WAN/phone addresses, call data, or credentials. Unsupported optional
capabilities are a successful diagnosis and include remediation.

Doctor preserves completed checks when diagnosis cannot continue: the partial
report is written to stdout and a structured error to stderr. Unreachable
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
output return at most 20 entries by default and accept `--all`; compact output
reports omitted entries, while JSON includes `total` and `omitted`. Empty output
is `devices[0]: no devices found` in compact form and
`{"devices":[],"total":0,"omitted":0}` in JSON.

The `Hosts` service is optional on some TR-064 implementations. A router that
does not advertise it returns `unsupported_capability` and exit `5`; router
faults, malformed or implausibly large host counts, and malformed active states
remain protocol errors with exit `6`. Names, addresses, and interface types can be empty when the router
does not know them. `interface_type` is the service's documented interface
classification, not a physical switch port or inferred connection detail.

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
Both formats show 20 entries by default, with `--all` for the complete inspected
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

### Wi-Fi radio mutation

`wifi enable|disable` is the first state-changing command. It uses only the documented
FRITZ! TR-064 [WLANConfiguration v48](https://fritz.support/resources/TR-064_WLAN_Configuration.pdf)
actions `GetInfo` (to read `NewEnable`) and `SetEnable` (to change it); no other mutating action,
undocumented endpoint, or browser scraping is used.

The mutation contract:

- Without `--confirm` the command reads the current state, prints a preview with the target
  instance and intended new state, changes nothing, and exits `0` with a `next:` suggestion.
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
- Errors are structured on stderr with the standard exit codes (`2` usage, `3` auth, `4`
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

### Wi-Fi inspection

Wi-Fi inspection requires the current checkout; it is not included in v0.1.0.

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

Reads are atomic at the command boundary: any instance or action failure leaves
stdout empty and emits a sanitized structured error with a nonzero exit code.
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

### Port-forward inspection

`forwards` requires the current checkout; it is not included in v0.1.0.
It reads all mappings, including disabled ones, from the router’s active WAN
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
20 entries by default. `--all` returns the complete inspected list; compact
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

`overview` reads router identity, WAN state, and traffic totals in that fixed
order. It is atomic: if any read fails, stdout is empty and the command emits
the failed operation as a structured error on stderr with its normal non-zero
exit code. It never presents a partial overview as successful. JSON output has
stable `router`, `wan`, and `traffic` objects in that order.

The router defaults to `http://fritz.box:49000`. Override it with `--host ADDRESS` or
`ROUTER_AXI_HOST`; an address without a port uses TR-064 port `49000` for HTTP or
`49443` for HTTPS. Credentials are read only from the environment. The default
output is compact AXI text; `--json` emits JSON on stdout. Errors are structured
on stderr in the selected format. `calls` returns at most 20 entries by default,
reports the omitted count, and accepts `--all` for the complete list.

Exit codes are `0` for success, `1` for local output/internal failure, `2` for
usage or configuration errors, `3` for authentication failure, `4` when the
router is unreachable, `5` for unsupported router capabilities, and `6` for a
router or protocol error.

The implementation discovers services through `/tr64desc.xml` and invokes only
read actions, plus the confirmed `wifi enable|disable` mutation described
above, which additionally invokes the documented `WLANConfiguration:SetEnable`.
Doctor uses only `DeviceInfo:GetInfo`; inspection commands use
`DeviceInfo:GetInfo`, `WANIPConnection` or
`WANPPPConnection:GetStatusInfo` and `GetExternalIPAddress`,
`WANCommonInterfaceConfig:GetTotalBytesReceived` and `GetTotalBytesSent`, AVM's
documented `X_AVM-DE_OnTel:GetCallList`, and the standard
`Hosts:GetHostNumberOfEntries` plus zero-based `GetGenericHostEntry(NewIndex)`,
and `WLANConfiguration:GetInfo`, `GetChannelInfo`, `GetTotalAssociations`, and
`GetBeaconType`, plus `Layer3Forwarding:GetDefaultConnectionService` and the
active `WANIPConnection`/`WANPPPConnection`:
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
ROUTER_AXI_LIVE_TEST=1 go test ./internal/tr064 -run '^(TestLiveReadOnlyCommands|TestLiveWiFi|TestLiveForwards|TestLiveLeases)$' -count=1
unset ROUTER_AXI_PASSWORD ROUTER_AXI_USERNAME ROUTER_AXI_HOST
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
validation. Wi-Fi live reads use only the four actions
above, including `GetInfo`, and never any key-returning action; missing
advertisement is checked as unsupported. Ordinary `go test ./...` and all CI environments cannot enable the
live test.

Live mutation coverage is additionally opt-in and skipped by default: it requires the
complete live gate above plus `ROUTER_AXI_LIVE_MUTATION_TEST=1`. It targets the only
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

## License

[MIT](LICENSE)

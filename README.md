# router-axi

An agent-ergonomic CLI for inspecting and operating supported home routers.

The read-only MVP provides device information, WAN status, traffic statistics, call-list access, connected-device, observed lease metadata, Wi-Fi, documented guest Wi-Fi, and port-forward inspection through documented FRITZ!Box TR-064 interfaces.

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
router-axi watch
router-axi watch --interval 2s --count 10 --json
router-axi calls
router-axi devices
router-axi devices --json
router-axi leases
router-axi leases --json
router-axi leases --all
router-axi wifi
router-axi wifi --json
router-axi guest
router-axi guest --json
router-axi wifi enable
router-axi wifi disable
router-axi wifi disable --instance 1 --confirm
router-axi reboot          # preview only; restart requires --confirm
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

`doctor` performs one bounded diagnosis: it fetches the TR-064 device
description once and, when `DeviceInfo` is advertised, invokes only
`DeviceInfo:GetInfo` to verify authentication and obtain model and firmware.
It reports endpoint reachability, TR-064 availability, authentication, and
whether the router advertises the services required by `status`, `overview`,
`wan`, `traffic`, `calls`, `devices`, `leases`, `wifi`, `forwards`, `reboot`, and
`backup`. Doctor does not report a separate `guest` capability: WLANConfiguration
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

### Bounded WAN and traffic watch

`watch` requires the current checkout; it is not in v0.2.0. It is a read-only,
non-interactive stream, not a daemon. Defaults are **6 samples with a 5s
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

A failed first sample emits no stdout. Any later failure retains earlier complete
samples, emits a sanitized structured error on **stderr** in the selected format,
and terminates immediately; consumers must check the exit status. There are no
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

### Router reboot

`reboot` requires the current checkout. It uses only the documented
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

### Configuration backup

`backup` requires the current checkout. It uses only the documented
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
certificate exits `4` with the code `tls_untrusted`. The export is bounded at 64 MiB; larger downloads are a
protocol error. Compatibility is fixture-backed with synthetic servers only;
the FRITZ!Box export flow is **not hardware-validated**, and `backup` does not
verify that the export can be restored.

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

### Guest Wi-Fi inspection

Guest Wi-Fi inspection requires the current checkout; it is not included in v0.1.0.

`guest` is a distinct top-level read command because the existing CLI grammar has
single-word inspection commands; `wifi` accepts only the established mutation
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

`overview` reads router identity, WAN state, and traffic totals in that fixed
order. It is atomic: if any read fails, stdout is empty and the command emits
the failed operation as a structured error on stderr with its normal non-zero
exit code. It never presents a partial overview as successful. JSON output has
stable `router`, `wan`, and `traffic` objects in that order.

The router defaults to `http://fritz.box:49000`. Override it with `--host ADDRESS` or
`ROUTER_AXI_HOST`; an address without a port uses TR-064 port `49000` for HTTP or
`49443` for HTTPS. Credentials are read only from the environment. The default
output is compact AXI text; `--json` emits JSON on stdout. Errors are structured
on stderr in the selected format. `calls` returns at most 100 entries by default,
reports the omitted count, and accepts `--all` for the complete list.

#### Exit codes

router-axi extends the common CLI convention that `0` means success, `1` an
internal error, and `2` a usage error with three additional router-domain
codes. A consumer only needs the rule that **any non-zero exit code means
failure**; the code classifies why it failed so agents can branch without
parsing stderr:

| Code | Meaning |
| ---- | ------- |
| `0`  | Success, including an already-satisfied `wifi enable\|disable` or a reboot preview. |
| `1`  | Internal failure, such as a local output or backup-file write error. |
| `2`  | Usage or configuration error (invalid arguments, missing `--output`, missing passphrase, ambiguous instance). |
| `3`  | Authentication failure; the router rejected the credentials from `ROUTER_AXI_USERNAME`/`ROUTER_AXI_PASSWORD`. |
| `4`  | Network failure; the router is unreachable, a request timed out, or its HTTPS certificate failed verification (`tls_untrusted`). |
| `5`  | Unsupported capability; the router does not advertise or implement the required service or action (`unsupported_capability`). |
| `6`  | Router or protocol error; the router responded with a fault, malformed data, or did not confirm a requested state. |

`watch` cancellation through Ctrl-C/SIGTERM exits `130`, the conventional
SIGINT/interrupted status. Error details are structured on stderr in the
selected output format; exit codes stay authoritative.

The implementation discovers services through `/tr64desc.xml` and invokes only
read actions, plus the confirmed `wifi enable|disable` and `reboot` mutations
described above, which invoke only the documented `WLANConfiguration:SetEnable`
and `DeviceConfig:Reboot` respectively, and the documented
`DeviceConfig:X_AVM-DE_GetConfigFile` export described above.
Doctor uses only `DeviceInfo:GetInfo`; inspection commands use
`DeviceInfo:GetInfo`, `WANIPConnection` or
`WANPPPConnection:GetStatusInfo` and `GetExternalIPAddress`,
`WANCommonInterfaceConfig:GetTotalBytesReceived` and `GetTotalBytesSent`, AVM's
documented `X_AVM-DE_OnTel:GetCallList`, and the standard
`Hosts:GetHostNumberOfEntries` plus zero-based `GetGenericHostEntry(NewIndex)`,
and `WLANConfiguration:GetInfo`, `GetChannelInfo`, `GetTotalAssociations`, and
`GetBeaconType`. Guest inspection additionally uses the documented AVM
`WLANConfiguration:X_AVM-DE_GetWLANExtInfo` action only to read
`NewX_AVM-DE_APType`, then the same four status actions for explicitly classified
guest instances. It also uses `Layer3Forwarding:GetDefaultConnectionService` and the
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
ROUTER_AXI_LIVE_TEST=1 go test ./internal/tr064 -run '^(TestLiveReadOnlyCommands|TestLiveWiFi|TestLiveGuestWiFi|TestLiveForwards|TestLiveLeases)$' -count=1
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
validation. Wi-Fi live reads use only the four actions
above, including `GetInfo`, and never any key-returning action; missing
advertisement is checked as unsupported. Guest live coverage adds the documented
AP-type action, has a 30-second deadline, discards returned values, never logs them,
and skips when complete documented support is unavailable. Ordinary `go test ./...`
and all CI environments cannot enable the live test.

Live mutation coverage is additionally opt-in and skipped by default: it requires the
complete live gate above plus `ROUTER_AXI_LIVE_MUTATION_TEST=1`. This enables only
Wi-Fi coverage, never reboot. There is no live reboot test. It targets the only
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

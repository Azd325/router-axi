# router-axi

An agent-ergonomic CLI for inspecting and operating supported home routers.

The read-only MVP provides device information, WAN status, traffic statistics, call-list access, connected-device inspection, and Wi-Fi inspection through documented FRITZ!Box TR-064 interfaces.

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
router-axi wifi
router-axi wifi --json
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
`wan`, `traffic`, `calls`, `devices`, and `wifi`. It does not invoke those commands, retrieve a WAN
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
read actions. Doctor uses only `DeviceInfo:GetInfo`; inspection commands use
`DeviceInfo:GetInfo`, `WANIPConnection` or
`WANPPPConnection:GetStatusInfo` and `GetExternalIPAddress`,
`WANCommonInterfaceConfig:GetTotalBytesReceived` and `GetTotalBytesSent`, AVM's
documented `X_AVM-DE_OnTel:GetCallList`, and the standard
`Hosts:GetHostNumberOfEntries` plus zero-based `GetGenericHostEntry(NewIndex)`,
and `WLANConfiguration:GetInfo`, `GetChannelInfo`, `GetTotalAssociations`, and
`GetBeaconType`. Wi-Fi inspection does not call `GetSecurityKeys` or any other
key- or client-returning WLAN action.
Call-list URLs are accepted only from the same router origin. Device inspection
does not use AVM host-list URLs, browser scraping, or network scanning.

## Development

Setup, checks, and the contribution workflow are in
[CONTRIBUTING.md](CONTRIBUTING.md). Release maintainers should follow
[RELEASING.md](RELEASING.md). Changes are submitted through the
[no-mistakes](https://github.com/kunchenguid/no-mistakes) gate, which validates
a feature branch and opens the pull request after the configured checks pass.

### Opt-in live router tests

The live suite invokes only the read actions listed above. It is skipped unless
`ROUTER_AXI_LIVE_TEST` is exactly `1`, an explicit host and both credentials are
set, and `CI` is empty. Run it locally without placing credentials on the
command line:

```sh
export ROUTER_AXI_HOST='fritz.box'
read -rs 'ROUTER_AXI_USERNAME?Router username: '; export ROUTER_AXI_USERNAME; printf '\n'
read -rs 'ROUTER_AXI_PASSWORD?Router password: '; export ROUTER_AXI_PASSWORD; printf '\n'
ROUTER_AXI_LIVE_TEST=1 go test ./internal/tr064 -run '^(TestLiveReadOnlyCommands|TestLiveWiFi)$' -count=1
unset ROUTER_AXI_PASSWORD ROUTER_AXI_USERNAME ROUTER_AXI_HOST
```

Do not add `-v`: the test deliberately reports only command-level failures and
never logs responses, credentials, serial numbers, phone, device, radio, or
network data, or router addresses. Wi-Fi live reads use only the four actions
above, including `GetInfo`, and never any key-returning action; missing
advertisement is checked as unsupported. Ordinary `go test ./...` and all CI environments cannot enable the
live test.

## License

[MIT](LICENSE)

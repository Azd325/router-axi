# router-axi

An agent-ergonomic CLI for inspecting and operating supported home routers.

The read-only MVP provides device information, WAN status, traffic statistics, and call-list access through documented FRITZ!Box TR-064 interfaces.

## Design constraints

- Compact TOON output by default, with JSON available explicitly.
- Non-interactive commands, structured errors, and meaningful exit codes.
- Read-only behavior by default.
- Explicit confirmation for disruptive operations.
- Local operation without telemetry or a hosted account.

See [VISION.md](VISION.md) for the acceptance policy.

## Usage

```sh
export ROUTER_AXI_USERNAME='router-user'
export ROUTER_AXI_PASSWORD='router-password'

router-axi                 # status is the default view
router-axi status
router-axi overview
router-axi wan
router-axi traffic
router-axi calls
router-axi wan --json
```

On the FRITZ!Box 6591 with FRITZ!OS 8.25, the advertised WAN service name does
not reliably identify the returned address family. `wan` therefore classifies
`ip_family` from the address itself. `traffic` includes `observed_at`, the UTC
RFC 3339 time at which the CLI completed both counter reads; compact output
shows `unknown` if an observation time is unavailable.

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
read actions: `DeviceInfo:GetInfo`, `WANIPConnection` or
`WANPPPConnection:GetStatusInfo` and `GetExternalIPAddress`,
`WANCommonInterfaceConfig:GetTotalBytesReceived` and `GetTotalBytesSent`, and AVM's documented
`X_AVM-DE_OnTel:GetCallList`. Call-list URLs are accepted only from the same
router origin.

## Development

Requirements:

- Nix with flakes enabled
- Optional: direnv

Enter the development environment with:

```sh
nix develop
```

Or, with direnv installed:

```sh
direnv allow
```

The environment provides Go 1.26, `golangci-lint`, and a `check` command.

Run the project checks with:

```sh
check
```

### Opt-in live router tests

The live suite invokes only the read actions listed above. It is skipped unless
`ROUTER_AXI_LIVE_TEST` is exactly `1`, an explicit host and both credentials are
set, and `CI` is empty. Run it locally without placing credentials on the
command line:

```sh
export ROUTER_AXI_HOST='fritz.box'
read -rs 'ROUTER_AXI_USERNAME?Router username: '; export ROUTER_AXI_USERNAME; printf '\n'
read -rs 'ROUTER_AXI_PASSWORD?Router password: '; export ROUTER_AXI_PASSWORD; printf '\n'
ROUTER_AXI_LIVE_TEST=1 go test ./internal/tr064 -run '^TestLiveReadOnlyCommands$' -count=1
unset ROUTER_AXI_PASSWORD ROUTER_AXI_USERNAME ROUTER_AXI_HOST
```

Do not add `-v`: the test deliberately reports only command-level failures and
never logs responses, credentials, serial numbers, phone data, or router
addresses. Ordinary `go test ./...` and all CI environments cannot enable the
live test.

## License

[MIT](LICENSE)

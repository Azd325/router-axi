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

router-axi status
router-axi wan
router-axi traffic
router-axi calls
router-axi wan --json
```

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
`WANCommonInterfaceConfig:GetAddonInfos`, and AVM's documented
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

## License

[MIT](LICENSE)

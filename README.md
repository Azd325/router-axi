# router-axi

An agent-ergonomic CLI for inspecting and operating supported home routers.

The project is in its initial design phase.
Its first release will provide read-only device information, WAN status, traffic statistics, and call-list access through documented router interfaces.

## Design constraints

- Compact TOON output by default, with JSON available explicitly.
- Non-interactive commands, structured errors, and meaningful exit codes.
- Read-only behavior by default.
- Explicit confirmation for disruptive operations.
- Local operation without telemetry or a hosted account.

See [VISION.md](VISION.md) for the acceptance policy.

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

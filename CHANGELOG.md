# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

## v0.1.0 - 2026-10-18

### Added

- `cx serve` runs the embedded mihomo core in the foreground with graceful
  shutdown, SIGHUP reloads, and optional subscription refresh.
- `cx run` starts an ephemeral proxy, runs a child command with proxy
  environment variables and signal forwarding, then removes its state.
- `cx sub update` fetches and merges subscriptions with base64 fallback,
  validation, and atomic last-good config replacement.
- `cx pack` builds preconfigured binaries with embedded config and geodata
  payloads; `cx version` and `cx config show` report the payload.
- Built-in metacubexd panel enabled with `--ui`, `--ui-addr ADDR`,
  `CX_UI`/`CX_UI_ADDR`, and generated controller secrets (`--secret`).
- Static builds support the slim tags `no_zerotier`, `no_easytier`, and
  `no_tailscale`; release builds bundle the panel with `ui`.

### Security

- `cx config show` redacts credentials, authorization headers, URL userinfo,
  query strings, and URL paths, and subscription errors sanitize URLs.
- Controller secrets are generated when needed and stripped from packed
  configs; `cx run` disables TLS, Unix-socket, and named-pipe controllers.

### Fixed

- Windows: the mihomo cache database is closed on Stop, occupied-port checks
  recognize `WSAEADDRINUSE`, and in-place packing closes handles and refuses to
  replace the running executable.
- `cx run` rejects occupied ports, verifies mihomo bound the selected port,
  defaults to warning logs, and cleans up child process groups.
- The bundled panel embeds underscore-prefixed asset directories.

### Platforms

- Linux, macOS, Windows, and FreeBSD builds for amd64/arm64, plus linux/armv7.

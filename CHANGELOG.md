# Changelog

All notable changes to this project are documented in this file.

## v0.1.1 - Unreleased

### Added

- Node selection with `--select SPEC` on `run`, `serve`, and `pack`,
  semicolon-separated `CX_SELECT`, and the local `x-cx-select` config key.
  Specs support the primary selector group, `GROUP=PATTERN`, case-insensitive
  Go regexps, term/subsequence matching, deterministic first-match selection,
  and closest-candidate errors. The key is stripped before mihomo starts.
- Override files deep-merged over subscription/config sources: state-directory
  `override.yaml`, repeatable `--override FILE`, and `CX_OVERRIDE`. Maps merge
  recursively while lists and scalars replace.
- `CX_SUB` and repeatable `--sub-file FILE` subscription sources, with comment
  and blank-line handling and a Unix permissions warning.
- Selection is embedded in packed binaries as `x-cx-select`; runtime flags and
  environment override it.

### Changed

- Ports `0` now disable listeners for `--port`, `--socks-port`, `--mixed-port`,
  `--redir-port`, and `--tproxy-port`; `cx run` auto-picks a mixed port when it
  is omitted or `0`.
- `cx run` binds loopback-only by default (`allow-lan: false`,
  `bind-address: 127.0.0.1`) unless `--allow-lan` is passed.
- Without an explicit selection, initial select-group nodes skip placeholder and
  info nodes; groups, `DIRECT`, and `REJECT` are preserved.
- `sub update` and config validation emit warning+ mihomo logs only.

### Documentation

- README (English and Chinese) documents selection, override precedence, new
  subscription sources, loopback-only `run`, and port `0` behavior.

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

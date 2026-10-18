# Changelog

All notable changes to this project are documented in this file.

## v0.2.0 - 2026-10-18

### Added

- Node selection with `--select SPEC`, `CX_SELECT`, and `x-cx-select` on
  `run`, `serve`, and `pack`: bare and `GROUP=PATTERN` specs, Go regexps, region
  aliases, and `type:PROTO`/protocol-word matching. Selections are embedded in
  packed binaries as `x-cx-select`.
- Deep-merged override files (`override.yaml`, `--override`, `CX_OVERRIDE`) and
  subscription sources (`CX_SUB`, `--sub-file`).
- TUN controls `--tun`, `--tun-stack`, `--tun-device`, `--tun-exclude`,
  `--tun-route-table`, and `--tun-rule-index`, plus `--interface`,
  `--routing-mark`, and `--no-auto-interface`. Non-default devices derive
  distinct route indexes and a separate fake-IP pool/TUN address.
- Host-TUN detection on Linux and Windows, including Linux netlink lookups for
  policy-routed interfaces; macOS logs a hint instead of auto-binding.
- Initial health checks for URL-test and fallback groups after startup and
  reload.
- Systemd `Type=notify` support, the `cx-tun.conf` drop-in, systemd/launchd
  reload hardening, `with_gvisor` builds, pinned UPX release compression, and
  the hardened public-server TUN example.
- `cx version` falls back to module or VCS build information when no release
  version is injected.

### Changed

- Selection now applies bare patterns across all matching select groups, lets
  precise group specs win, matches short region codes as whole tokens, selects
  protocol words by adapter type, removes fuzzy matching, and skips
  placeholder/info nodes with a one-line fallback warning listing affected
  groups.
- Port `0` disables listeners; `cx run` auto-picks a mixed port and binds
  loopback-only unless `--allow-lan` is passed.
- `--ui=false`/`CX_UI=false` disables the controller and all controller
  endpoints; an explicit `--external-controller` still wins.
- `cx serve` starts from a valid cached config and refreshes subscriptions in
  the background; startup signals are queued safely and failed refreshes keep
  the running configuration.
- `cx config show` redacts only credentials and secret-bearing URL parts.
- TUN startup and reload verify mihomo's listener state, roll back failed
  reloads, and report actionable causes. Default device names (`Meta`, `utunN`,
  packaged `cxtun`) keep the default pool and 2022/9000 indexes.
- systemd uses `Type=notify`, `Restart=always`, and reload support; launchd
  documents the equivalent `KeepAlive`/reload behavior. Normal shutdown
  suppresses mihomo's shutdown warning.

### Fixed

- SIGHUP during `cx serve` startup no longer kills the process.
- TUN startup failures are no longer silent; busy and `EEXIST` errors explain
  the conflicting device and suggest `--tun-device` or route/rule indexes.
- Linux host-TUN detection uses netlink instead of only the main routing table,
  and non-TUN `cx run` no longer loops outbound traffic back into a host TUN.
- Node-selection errors describe all select groups and rank real nodes by
  protocol, region, tokens, and text similarity instead of listing groups.
- `cx pack` warns when replacing an existing payload instead of silently
  discarding it.
- Non-default TUN IPv4 addresses come from a separate `198.19.N.1/24` fake-IP
  pool whose first /30 mihomo uses for the TUN, avoiding overlap with the
  default `198.18.0.0/16` pool.

### Security

- `cx serve` persists generated controller secrets in
  `<state-dir>/controller-secret` (`0600`) and logs only the path; `cx run`
  prints a generated secret only on a terminal and otherwise stores it in its
  temporary state directory.

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

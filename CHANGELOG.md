# Changelog

All notable changes to this project are documented in this file.

## v0.1.1 - Unreleased

### Added

- Node selection with `--select SPEC` on `run`, `serve`, and `pack`,
  semicolon-separated `CX_SELECT`, and the local `x-cx-select` config key.
  Specs support bare patterns across select groups, precise `GROUP=PATTERN`,
  case-insensitive Go regexps, region aliases (for example 日本/japan/jp and
  美国/usa/us expand to their codes and flags), and `type:PROTO` selectors such
  as `type:anytls`, `type:hysteria2`, and `type:hy2`. The key is stripped before
  mihomo starts.
- Override files deep-merged over subscription/config sources: state-directory
  `override.yaml`, repeatable `--override FILE`, and `CX_OVERRIDE`. Maps merge
  recursively while lists and scalars replace.
- `CX_SUB` and repeatable `--sub-file FILE` subscription sources, with comment
  and blank-line handling and a Unix permissions warning.
- Selection is embedded in packed binaries as `x-cx-select`; runtime flags and
  environment override it.
- `cx version` falls back to Go build information when the release version is
  not injected: the module version when available, otherwise the 12-character
  VCS revision with `-dirty` when the worktree was modified.
- The systemd unit now has `ExecReload=/bin/kill -HUP $MAINPID`,
  `EnvironmentFile=-/etc/cx/cx.env`, `LimitNOFILE=1048576`, and proxy-only
  hardening (`NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`,
  `PrivateTmp`). The launchd plist documents the reload command and the
  `KeepAlive`/`ThrottleInterval` equivalents.

### Changed

- Bare `PATTERN` selection applies to every select group that directly contains
  a matching member and sets groups containing the final `MATCH` selector to it;
  other groups are left unchanged. Precise `GROUP=PATTERN` specs run afterwards
  and win. Bare selection only errors when no group contains a match, and then
  lists actual proxy nodes ranked by matching words; group names appear only in
  `GROUP=PATTERN` errors.
- Short region codes match whole tokens, so `us` does not match `plus`, and the
  fuzzy subsequence fallback was removed (it matched `trojan` to `🇹🇷TR 01`).
- Ports `0` now disable listeners for `--port`, `--socks-port`, `--mixed-port`,
  `--redir-port`, and `--tproxy-port`; `cx run` auto-picks a mixed port when it
  is omitted or `0`.
- `cx run` binds loopback-only by default (`allow-lan: false`,
  `bind-address: 127.0.0.1`) unless `--allow-lan` is passed.
- Without an explicit selection, initial select-group nodes skip placeholder and
  info nodes; groups, `DIRECT`, and `REJECT` are preserved.
- `cx serve` registers SIGHUP/SIGTERM/SIGINT handlers before loading or fetching
  configuration. A SIGHUP during startup is queued and coalesced into one reload
  after the core is ready; SIGTERM/SIGINT cancel an in-flight fetch and exit
  cleanly.
- `cx config show` redacts only credentials and secret-bearing URL parts:
  password, uuid, private-key, pre-shared-key, auth/auth-str, token-like keys,
  secret, obfs-password, URL userinfo, and secret-looking query parameters.
  Proxy-provider and subscription URLs hide their whole query string. DoH paths,
  credential-free geox URLs, and rule-provider URLs without secret parameters
  are no longer over-redacted.
- Boolean flag help documents explicit forms such as `--allow-lan=false` and
  `--ui=false`.
- Normal `run`/`serve` shutdown suppresses mihomo's "Mihomo shutting down"
  warning. A shutdown reparse optimization was deliberately skipped.
- `sub update` and config validation emit warning+ mihomo logs only.

### Documentation

- README (English and Chinese) documents region/type selection, the removal of
  fuzzy matching, the narrowed redaction rules, and version fallback.
- Added a hardened public-server example: `override.yaml` with `allow-lan:
  false` and unused ports `0`, controller/panel on `127.0.0.1:9090` with a
  secret, and `--sub-file`/`CX_SUB` supplied through `/etc/cx/cx.env`. The docs
  warn that subscriptions may set `allow-lan: true` and must not be trusted.

### Fixed

- SIGHUP received during `cx serve` startup no longer kills the process through
  Go's default signal action; systemd now sees a live service and can apply the
  queued reload.
- Node-selection errors no longer say "primary group X"; they describe the
  all-select-groups behavior and list real proxy nodes instead of groups,
  `DIRECT`, or `REJECT`.
- Launchd and systemd reload behavior is documented and wired consistently.

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

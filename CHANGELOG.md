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
- `serve` and `run` accept `--tun`, `--tun-stack system|gvisor|mixed`,
  `--tun-device NAME`, and repeatable `--tun-exclude CIDR`. They override
  `tun.enable`, `tun.stack`, `tun.device`, `tun.auto-route`,
  `tun.auto-detect-interface`, and `tun.route-exclude-address`; CIDRs are
  validated before startup.
- `serve` and `run` accept `--interface NAME`, `--routing-mark N`, and
  `--no-auto-interface`. For non-TUN instances on Linux, macOS, and Windows,
  `cx` detects when the effective default route uses a host TUN and binds
  outbound sockets (including DNS) to the physical default interface unless
  `interface-name`/`routing-mark` is already set. On Linux the effective egress
  is resolved with netlink `RTM_GETROUTE` lookups for public IPv4/IPv6 targets,
  so policy routing tables such as sing-tun's table 2022 are honored; the
  physical interface is taken from the main table (254) default route.
  `--interface=` or `--no-auto-interface` disables auto-detection.
- URL-test and fallback groups receive an initial health check after startup and
  reload, so the first request does not pick an untested first member while the
  other nodes are still unmeasured.
- Added the `packaging/systemd/cx-tun.conf` drop-in with `CAP_NET_ADMIN`,
  `CAP_NET_BIND_SERVICE`, `/dev/net/tun` access, and `PrivateDevices=no`.
- All CI, release, and documented builds now include the `with_gvisor` mihomo
  tag so the gVisor and mixed TUN stacks are available.
- Added `examples/override-tun-server.yaml` with the system stack, automatic
  routing/interface detection, private/loopback/link-local/multicast/broadcast
  route exclusions, fake-IP, and local/cloud fake-IP filters. Comments document
  adding provider-internal DNS/metadata addresses and the host's public IP.
- The systemd unit now has `ExecReload=/bin/kill -HUP $MAINPID`,
  `EnvironmentFile=-/etc/cx/cx.env`, `LimitNOFILE=1048576`, and proxy-only
  hardening (`NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`,
  `PrivateTmp`). The launchd plist documents the reload command and the
  `KeepAlive`/`ThrottleInterval` equivalents.
- Release builds for Linux `amd64`/`arm64`/`armv7` and Windows `amd64`/`arm64`
  are compressed with pinned UPX 5.2.1 (`--best`). The UPX tarball is
  LZMA is not used: it saved ~3 MB but raised startup time from ~0.2 s to ~1 s.
  SHA-256 verified against a hard-coded digest, and `upx -t` runs on every
  compressed artifact before `SHA256SUMS` is generated. Darwin is left
  uncompressed; FreeBSD is skipped because UPX 5.2.1 has no FreeBSD
  `amd64`/`arm64` support.
- `cx serve` implements pure-Go sd_notify for systemd `Type=notify`: `READY=1`
  after the core and any enabled TUN are up, `RELOADING=1` with
  `MONOTONIC_USEC` then `READY=1` around reloads, `STOPPING=1` on shutdown, and
  short `STATUS=` messages. Notifications are no-ops without `NOTIFY_SOCKET` or
  on non-Linux.
- TUN flags `--tun-route-table` and `--tun-rule-index` map to mihomo's
  `iproute2-table-index` and `iproute2-rule-index`. A non-default
  `--tun-device` derives distinct indexes when neither the flags nor the
  config supply them, so a second TUN instance avoids the 2022/9000 conflict.

### Security

- `cx serve` no longer prints auto-generated controller secrets to stderr.
  The secret is persisted atomically in `<state-dir>/controller-secret` with
  mode `0600`, reused on later restarts while non-empty, and only the file path
  is logged. `cx run` prints a generated secret only when stderr is a terminal;
  otherwise it writes `<temp-dir>/controller-secret` and logs that path.

### Changed

- Bare `PATTERN` selection applies to every select group that directly contains
  a matching member and sets groups containing the final `MATCH` selector to it;
  other groups are left unchanged. Precise `GROUP=PATTERN` specs run afterwards
  and win. Bare selection only errors when no group contains a match, and then
  lists actual proxy nodes ranked by real similarity: protocol/type match (a
  bare word equal to a proxy type counts), region alias match, shared tokens,
  normalized longest common substring and shared characters, and only then
  group order. Group names appear only in `GROUP=PATTERN` errors. When the
  pattern matches top-level proxies that belong to no select group, the error
  lists them explicitly.
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
- `cx serve` now verifies mihomo's in-process TUN state after startup and after
  every reload. A requested TUN that fails to start makes startup exit non-zero;
  a failed TUN reload logs the error, re-applies the previous working config, and
  keeps serving.
- macOS host-TUN detection is now conservative: it never auto-binds outbound
  sockets, because `utun` devices are often system interfaces (iCloud Private
  Relay) or VPN apps. When the effective default route is a `utun`, `cx` logs a
  one-line hint to retry with `--interface en0` if proxy dials fail. Linux and
  Windows still auto-bind to the physical default interface.
- `cx serve` starts immediately from a valid state-directory `config.yaml` or
  `config.yaml.last-good` when subscriptions are configured, then refreshes
  subscriptions in the background. A failed fetch or invalid update logs a
  warning and keeps the running configuration; startup only fails when no
  usable configuration exists.
- Bare protocol words such as `trojan`, `anytls`, `hy2`/`hysteria2`, `ss`,
  `vmess`, `vless`, `tuic`, `wg`, `socks5`, and `http` now select by adapter
  type like `type:PROTO` instead of matching name substrings. Words of three
  characters or fewer match only whole tokens in selection and ranking.
- The systemd unit uses `Type=notify`, `NotifyAccess=main`, and
  `TimeoutStartSec=120s`, and the initial health-check start/result is logged at
  info level.
- The systemd unit uses `Restart=always`; the launchd agent uses
  `KeepAlive=true`. Both retain reload support.
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
- Documented TUN requirements (root or `CAP_NET_ADMIN`, plus
  `CAP_NET_BIND_SERVICE` for DNS port 53), installing the `cx-tun.conf` systemd
  drop-in and running `daemon-reload`, public-server route exclusions,
  provider-internal DNS/metadata addresses, fake-IP filtering for local/cloud
  domains, the `with_gvisor` build tag, `--tun-device`, and the `--interface`/
  `--routing-mark`/`--no-auto-interface` host-TUN workaround including Linux
  netlink detection and table 254 physical-interface lookup.
- README (English and Chinese) documents the controller-secret file for
  `serve`/`run`, TUN device busy guidance, Linux policy-route detection, and the
  similarity-based closest-candidate ranking.
- Added an operations note that `systemd-run --on-active=...` timers restart
  their countdown after `systemctl daemon-reload`.
- README (English and Chinese) documents which release targets are
  UPX-compressed, how to restore an uncompressed binary (`upx -d` or build from
  source), why UPX must not be run on packed binaries, and the macOS host-TUN
  hint.
- README (English and Chinese) documents cached startup with background
  subscription refresh, sd_notify/systemd behavior, TUN iproute2 indexes,
  protocol-word selection, and explicit `--ui=false` panel suppression.

### Fixed

- SIGHUP received during `cx serve` startup no longer kills the process through
  Go's default signal action; systemd now sees a live service and can apply the
  queued reload.
- TUN startup failures are no longer silent: the core checks mihomo's listener
  state, captures mihomo's underlying error from its log stream, and reports
  `mihomo TUN inbound did not start` with the actual cause plus remediation
  hints (`CAP_NET_ADMIN`/root, `/dev/net/tun`, device cgroups/`PrivateDevices`,
  or a gVisor/mixed stack that was not compiled in). An `EBUSY`/`device or
  resource busy` failure now says the TUN device is already in use by another
  process or cx instance and suggests `--tun-device NAME`.
- Linux host-TUN detection no longer parses only `/proc/net/route` (main table),
  which missed policy-routed TUN egress and left non-TUN `run` loops pointing at
  the wrong interface. It now queries the kernel's actual route with netlink.
- A non-TUN `cx run` no longer loops its outbound proxy connections back into a
  host TUN; it binds to the physical default interface unless overridden.
- Node-selection errors no longer say "primary group X"; they describe the
  all-select-groups behavior and list real proxy nodes instead of groups,
  `DIRECT`, or `REJECT`.
- Launchd and systemd reload behavior is documented and wired consistently.
- `cx pack` warns when the target binary already contains a payload and replaces
  it instead of silently discarding the old payload.
- TUN `EEXIST`/`file exists` errors now explain the iproute2 table/rule conflict
  and suggest `--tun-route-table`/`--tun-rule-index`.
- `--ui=false` no longer serves or prints the bundled web panel when a
  controller from the config remains enabled.
- Bare protocol selection no longer ranks unrelated `TR 01`/`TR 02` names first;
  exact adapter type matches win.

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

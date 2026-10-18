# cx — Clashix for the command line

`cx` is a single static Go binary that embeds [mihomo](https://github.com/MetaCubeX/mihomo)
as a library. It runs a foreground proxy service, starts ephemeral proxies for
individual commands, fetches subscriptions, and builds preconfigured binaries
with an embedded config and geodata payload.

`cx` and the embedded mihomo core are licensed under **GPL-3.0**; see
[LICENSE](LICENSE).

## English

### Install

Releases contain bare binaries plus `SHA256SUMS`:

`cx-linux-amd64`, `cx-linux-arm64`, `cx-linux-arm` (armv7), `cx-darwin-amd64`,
`cx-darwin-arm64`, `cx-windows-amd64.exe`, `cx-windows-arm64.exe`,
`cx-freebsd-amd64`, `cx-freebsd-arm64`, and `SHA256SUMS`.

Linux (`amd64`, `arm64`, `armv7`) and Windows (`amd64`, `arm64`) release
binaries are compressed with UPX 5.2.1 (`--best`). macOS and FreeBSD
artifacts are not compressed. To get an uncompressed binary, run
`upx -d cx-linux-amd64` with UPX 5.2.1 (or build from source below); `upx -d`
restores the executable from before compression. Packed binaries with an
embedded config are never UPX-compressed. UPX reduces the download size at the
cost of a decompression delay on every start; use `upx -d` or build from source
when startup latency matters. Do not run UPX on a packed binary: UPX 5.2.1
refuses a binary with an existing payload (`EOFException`) and leaves it
unchanged, while overlay-stripping options would discard the payload.

Linux/macOS:

```sh
curl -fLO https://github.com/DawnMagnet/clashix-go/releases/latest/download/cx-linux-amd64
curl -fLO https://github.com/DawnMagnet/clashix-go/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS   # macOS: shasum -a 256 -c SHA256SUMS
chmod +x cx-linux-amd64
sudo install -m 0755 cx-linux-amd64 /usr/local/bin/cx
cx version
```

Windows (PowerShell; use Git Bash or WSL for `sha256sum`):

```powershell
curl.exe -fLO https://github.com/DawnMagnet/clashix-go/releases/latest/download/cx-windows-amd64.exe
curl.exe -fLO https://github.com/DawnMagnet/clashix-go/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
Move-Item cx-windows-amd64.exe cx.exe
.\cx.exe version
```

`cx version` also reports whether an embedded payload is present and which
files it contains.

### Build from source

Requirements: Go 1.27 or newer. The binary is static; CGO is not needed.

```sh
git clone https://github.com/DawnMagnet/clashix-go.git
cd clashix-go

# Slim build without the web panel.
CGO_ENABLED=0 go build \
  -tags "with_gvisor no_zerotier no_easytier no_tailscale" \
  -trimpath -o cx ./cmd/cx

# Build with the bundled metacubexd panel.
./scripts/fetch-ui.sh
CGO_ENABLED=0 go build \
  -tags "ui with_gvisor no_zerotier no_easytier no_tailscale" \
  -trimpath -o cx ./cmd/cx
```

A plain `go build ./cmd/cx` still compiles, but reports the panel as not bundled.
The `with_gvisor` tag adds mihomo's gVisor TUN stack (needed for
`--tun-stack gvisor` and `mixed`). The `no_zerotier no_easytier no_tailscale`
tags drop the optional ZeroTier, EasyTier, and Tailscale outbounds; mainstream
protocols (ss, ssr, vmess, vless, reality, trojan, hysteria, hysteria2, tuic,
wireguard, socks5, http, snell, anytls) remain. Mihomo has no exclusion tags for mieru, SSH, AmneziaWG, or
Wazero, so those heavy dependencies stay in the binary.

### Configuration sources

Configuration is resolved in this order:

1. an embedded payload (a preconfigured `cx` binary),
2. `--config FILE`,
3. one or more `--sub URL` subscriptions,
4. the cached config in the state directory.

Later subscriptions contribute only `proxies`; the first subscription is the
base document. Controlled keys (ports, `allow-lan`, mode, log level, controller,
secret) are overlaid last. If none of the sources exist, `cx` exits with an
actionable error instead of starting with an empty configuration.

An override layer is deep-merged over the resolved source: `override.yaml` in
the state directory if present, each `--override FILE` in the order given, then
the file named by `CX_OVERRIDE`. Maps merge recursively; lists and scalars
replace. The effective precedence is **flags > environment > override files >
subscription/config**.

A port override of `0` disables that listener: `--port 0`, `--socks-port 0`,
`--mixed-port 0`, `--redir-port 0`, and `--tproxy-port 0`. `cx run` still needs
a mixed port, so it picks one automatically when `--mixed-port` is omitted or
`0`.

The state directory defaults to the per-user config directory plus `cx`
(for example `~/.config/cx` on Linux, `~/Library/Application Support/cx` on
macOS, or `%AppData%\cx` on Windows). Files are written atomically with mode
`0600`; the previous good config is kept as `config.yaml.last-good`.

### Node selection

`--select SPEC` (repeatable) works on `run`, `serve`, and `pack`. `CX_SELECT`
takes semicolon-separated specs, and a top-level `x-cx-select` list in the local
config or an override provides the default. Flags override `CX_SELECT`, which
overrides `x-cx-select`. The key is never passed to mihomo.

SPEC forms:

- `PATTERN` applies to every select group that directly contains a matching
  proxy, choosing that group's first match. Error messages describe this
  all-groups behavior and list the closest real proxy nodes, never groups,
  `DIRECT`, or `REJECT`. Groups without a matching direct member are left
  unchanged, except that a group containing the final `MATCH` selector is set
  to that selector.
- `GROUP=PATTERN` targets a select group whose name matches `GROUP`. Precise
  group specs run after bare patterns and win, and only these errors name the
  selected group.

Closest candidates are ranked by real similarity: a protocol/type match first
(bare protocol words and `type:` selectors count, so `trojan` ranks nodes whose
`type` is `trojan`), then a region alias match, then shared tokens, then
normalized longest common substring and shared characters, and finally the
original group order. If a pattern matches proxies that exist under top-level
`proxies` but are members of no select group, the error says so explicitly and
lists them.

A pattern starting with `re:` is a case-insensitive Go regular expression.
Otherwise every whitespace-separated word must match the candidate name with a
case-insensitive substring, except words of three characters or fewer, which
must match a whole token so `us` does not match `plus` and `syd` does not match
`Sydney`. Region words expand to their flag and code aliases:
日本/japan/jp → JP 🇯🇵, 香港/hongkong/hk → HK 🇭🇰, 台湾/taiwan/tw → TW 🇹🇼,
新加坡/singapore/sg → SG 🇸🇬, 美国/usa/us → US 🇺🇸, 韩国/korea/kr → KR 🇰🇷,
英国/uk/gb → UK/GB 🇬🇧, 德国/germany/de → DE 🇩🇪, 加拿大/canada/ca → CA 🇨🇦.
`type:PROTO` matches the proxy's type and can be combined with words, for
example `type:anytls`, `type:hysteria2`, or `type:hy2 hk`. Bare protocol words
(`trojan`, `anytls`, `hy2`/`hysteria2`, `hysteria`, `vmess`, `vless`,
`ss`/`shadowsocks`, `ssr`, `tuic`, `wireguard`/`wg`, `socks5`, `http`, `snell`)
behave like `type:PROTO`: they match and select that adapter type instead of a
name substring. There is no fuzzy subsequence fallback. Within each group, the
first matching member in member order wins. Without a selection,
placeholder/info nodes (reserved or empty servers, and names containing traffic
expiry/reset/website/support markers) are skipped when choosing every select
group's initial node. Groups, `DIRECT`, and `REJECT` are kept as-is.

```sh
cx run --config config.yaml --select 'hy2 us' -- curl https://example.com
cx serve --config config.yaml --select 'Proxy=re:^🇯🇵'
cx serve --config config.yaml --select 'Proxy=🇸🇬 Singapore'
cx serve --config config.yaml --select 'type:anytls hk'
CX_SELECT='Proxy=hy2 us;Auto=re:tokyo' cx serve --config config.yaml
cx pack -o cx-private --config config.yaml --select 'Proxy=hy2 us'
```

### `cx serve`

Run the service in the foreground. Logs go to stderr. SIGINT/SIGTERM stop it
gracefully. On Unix, SIGHUP re-reads the configuration sources, validates the
result, and applies it without replacing the last-good file until validation
succeeds. `--sub-interval 6h` periodically refreshes subscriptions and reloads.

When subscriptions are configured and a valid `<state-dir>/config.yaml` or
`config.yaml.last-good` exists, `cx serve` starts the core from it immediately
and refreshes subscriptions in the background. A failed fetch or invalid update
logs a warning and leaves the running configuration in place; startup fails
only when no usable configuration exists at all.

When any network controller endpoint (`external-controller` or
`external-controller-tls`) is enabled and the effective config has no secret,
`cx serve` generates a random controller secret at startup, persists it in
`<state-dir>/controller-secret` (mode `0600`, written atomically), and logs only
that file path so the secret does not end up in journald. A non-empty
`controller-secret` file is reused across restarts. A non-empty `secret` in the
config, `CX_SECRET`, or `--secret` always wins. When no controller is enabled,
no secret is added. Binding a non-loopback controller without a configured
secret prints an additional warning. Startup and reload finish loading
proxy/rule providers and trigger a bounded initial health check for URL-test and
fallback groups before reporting the configuration as active.

The built-in metacubexd panel is served at `/ui/` whenever a controller is
enabled and the binary was built with `-tags ui`. The boolean `--ui` flag (or a
boolean-like `CX_UI`, for example `1` or `true`) enables the controller and
panel. `--ui-addr ADDR` (or `CX_UI_ADDR`) chooses the bind address and implies
`--ui`; the default is `127.0.0.1:9090`. The login URL is printed at startup.
An explicit `--ui=false` does not serve or announce the bundled panel, even if
a controller from the config stays enabled. Panel version, checksum, and MIT
license are recorded in
[`docs/THIRD_PARTY.md`](docs/THIRD_PARTY.md).

```sh
cx serve --config ~/.config/cx/config.yaml
cx serve --config config.yaml --override extra.yaml --select 'Proxy=hy2 us'
cx serve --sub 'https://example.com/subscription' --sub-interval 12h
cx serve --dir /var/lib/cx
cx serve --ui                       # controller + panel on 127.0.0.1:9090
cx serve --ui --ui-addr 0.0.0.0:9090 --secret 'choose-a-strong-secret'
CX_UI=1 CX_UI_ADDR=127.0.0.1:9090 CX_SECRET=... cx serve
```

A systemd unit is provided at
[`packaging/systemd/cx.service`](packaging/systemd/cx.service):

```sh
sudo install -m 0644 packaging/systemd/cx.service /etc/systemd/system/cx.service
sudo systemctl daemon-reload
sudo systemctl enable --now cx
```

The unit uses `Type=notify` with `NotifyAccess=main` and `TimeoutStartSec=120s`.
`READY=1` is sent after the core and any enabled TUN are up; reloads send
`RELOADING=1` with `MONOTONIC_USEC` followed by `READY=1`, and shutdown sends
`STOPPING=1`. Short status messages and the initial health-check start/result
appear in the journal.

The packaged unit uses `/var/lib/cx` as its state directory. Seed it first with
`cx sub update --dir /var/lib/cx --sub URL` (run as root), or copy a config to
`/var/lib/cx/config.yaml`. The unit sets `ExecReload=/bin/kill -HUP $MAINPID`,
so `sudo systemctl reload cx` applies a new configuration without a restart.
The unit uses `Restart=always`; see [TUN mode](#tun-mode) for capabilities and
device access. For proxy-only operation uncomment `User=`/`Group=`.

#### Hardened public-server example

Subscriptions are untrusted input: a subscription may set `allow-lan: true` or
enable additional listeners. Override files are applied after subscriptions, so
pin the exposed surface explicitly instead of trusting the subscription.

```sh
sudo install -d -m 0700 /var/lib/cx /etc/cx
sudo tee /var/lib/cx/override.yaml >/dev/null <<'EOF'
allow-lan: false
bind-address: 127.0.0.1
port: 0
socks-port: 0
redir-port: 0
tproxy-port: 0
mixed-port: 7890
mode: rule
EOF
sudo tee /etc/cx/cx.env >/dev/null <<'EOF'
CX_SUB=https://example.com/subscription
CX_SECRET=choose-a-long-random-secret
EOF
sudo chmod 0600 /etc/cx/cx.env
```

Then enable the loopback-only panel and controller with a systemd drop-in:

```sh
sudo systemctl edit cx
```

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/cx serve --dir /var/lib/cx --ui-addr 127.0.0.1:9090
```

The secret comes from `CX_SECRET` in `/etc/cx/cx.env` (or use `--secret`). To
keep subscription URLs out of the unit, put one URL per line in
`/etc/cx/subscriptions` (mode `0600`) and add `--sub-file /etc/cx/subscriptions`
to `ExecStart` instead of setting `CX_SUB`. `--ui-addr` only binds the panel and
controller; keep it on loopback and access it through an SSH tunnel or a
reverse proxy with TLS and authentication. Do not expose the controller
directly to the internet.

#### TUN mode

TUN needs root or `CAP_NET_ADMIN`; binding mihomo's DNS listener on port 53
additionally needs `CAP_NET_BIND_SERVICE`. The gVisor and mixed stacks require
a build with the `with_gvisor` tag. For a systemd TUN deployment keep the
hardening in [`packaging/systemd/cx.service`](packaging/systemd/cx.service) and
install the drop-in instead of editing the unit:

```sh
sudo install -d -m 0755 /etc/systemd/system/cx.service.d
sudo install -m 0644 packaging/systemd/cx-tun.conf \
  /etc/systemd/system/cx.service.d/tun.conf
sudo systemctl daemon-reload
sudo systemctl restart cx
```

[`packaging/systemd/cx-tun.conf`](packaging/systemd/cx-tun.conf) sets
`AmbientCapabilities`/`CapabilityBoundingSet` to
`CAP_NET_ADMIN CAP_NET_BIND_SERVICE`, allows `/dev/net/tun` with
`DeviceAllow=/dev/net/tun rw`, and sets `PrivateDevices=no`.

`cx` checks mihomo's in-process TUN state after startup and after every reload.
If TUN was requested but did not come up, startup logs the underlying mihomo
error (for example `configure tun interface: operation not permitted`) and the
likely causes (missing `CAP_NET_ADMIN`/root, a missing or blocked
`/dev/net/tun` such as a device cgroup/`PrivateDevices`, or a gVisor/mixed stack
that was not compiled in), then exits non-zero. If the device is busy
(`EBUSY`/`device or resource busy`), the error names the TUN device as already
in use by another process or cx instance and suggests `--tun-device NAME`. A
failed reload logs the error, re-applies the previous working configuration,
and keeps the service alive.

The flags are `--tun`, `--tun-stack system|gvisor|mixed`, `--tun-device NAME`,
`--tun-route-table N`, `--tun-rule-index N`, and repeatable
`--tun-exclude CIDR`; they override the config/override/`tun` section. The last
two map to mihomo's `iproute2-table-index`/`iproute2-rule-index`. With a
non-default `--tun-device` and no explicit indexes, `cx` derives distinct values
(avoiding 2022/9000) so a second instance does not collide; an `EEXIST`/`file
exists` TUN error suggests these flags. On a
public server, exclude private, loopback, link-local, multicast, broadcast,
CGNAT, and metadata ranges, and keep local/cloud names out of fake-IP. A
ready-to-merge fragment with system stack, automatic routing/interface
detection, those route exclusions, fake-IP, and metadata/local-domain filters is
in [`examples/override-tun-server.yaml`](examples/override-tun-server.yaml).
The example also documents adding provider-internal DNS/metadata addresses
(for example Alibaba's `100.100.2.136`/`100.100.100.200`, Tencent's
`183.60.83.19`, and `169.254.169.254`) and the host's own public IP.

```sh
cx serve --config config.yaml --override examples/override-tun-server.yaml --tun
cx serve --config config.yaml --tun --tun-stack system \
  --tun-exclude 10.0.0.0/8 --tun-exclude 192.168.0.0/16
```

On public servers also exclude cloud metadata endpoints such as
`metadata.google.internal`, `metadata.tencentyun.com`, and `100.100.100.200`,
and use `fake-ip-filter` for `*.lan`, `*.local`, `+.localhost`, `+.internal`,
and local/cloud domains. Never route cloud metadata traffic through a
third-party proxy.

When TUN is not enabled, `cx` detects a host TUN on the effective route. On
Linux this uses netlink `RTM_GETROUTE` lookups for `1.1.1.1` and
`2606:4700:4700::1111`, so policy routing (for example sing-tun rules pointing
at table 2022) is honored instead of only reading the main table. Windows keeps
its platform routing-table reader. When the effective device is a TUN, the
physical interface is taken from the main table (254) default route. If
`interface-name` and `routing-mark` are not already set, `cx` binds outbound
sockets (including DNS) to the physical default interface and logs once to
stderr:
`cx run: host TUN detected (dev Meta); binding outbound to eth0`. This prevents
a non-TUN `cx run` from being captured by another `cx serve --tun`. On macOS,
`utun` devices are often created by the system (iCloud Private Relay) or VPN
apps, so `cx` never auto-binds there; when the effective default route is a
`utun`, it logs a one-line hint to retry with `--interface en0` if proxy dials
fail. Use `--interface NAME` to force an interface, `--routing-mark N` to force
a routing mark, and `--interface=` or `--no-auto-interface` to disable
auto-detection. Detection failures are silent.

#### Operations notes

A systemd transient timer created with `systemd-run --on-active=...` restarts
its countdown after `systemctl daemon-reload`. Re-create the timer if you need
the original deadline.

A launchd agent is provided at
[`packaging/launchd/com.dawnmagnet.cx.plist`](packaging/launchd/com.dawnmagnet.cx.plist):

```sh
install -m 0644 packaging/launchd/com.dawnmagnet.cx.plist \
  ~/Library/LaunchAgents/com.dawnmagnet.cx.plist
launchctl bootstrap gui/"$(id -u)" ~/Library/LaunchAgents/com.dawnmagnet.cx.plist
launchctl kickstart -k gui/"$(id -u)"/com.dawnmagnet.cx
launchctl kill SIGHUP gui/"$(id -u)"/com.dawnmagnet.cx   # reload
```

launchd has no `ExecReload` or `EnvironmentFile`; use `launchctl kill SIGHUP`
and `EnvironmentVariables` or a wrapper script. `KeepAlive=true` is the
`Restart=always` equivalent, and `ThrottleInterval` is the `RestartSec`
equivalent.

### `cx run`

Starts a temporary proxy on a free loopback mixed port, exports both lower- and
upper-case proxy variables, runs the child in its own process group when stdin
is not a terminal, forwards signals, and exits with the child's exit code. The
temporary state directory is always removed. `cx run` is loopback-only by
default (`allow-lan: false`, `bind-address: 127.0.0.1`); pass `--allow-lan` to
keep the configured LAN binding.

```sh
cx run --config direct.yaml -- curl https://example.com
cx run --config config.yaml --select 'Proxy=hy2 us' -- curl https://example.com
cx run --config config.yaml --override extra.yaml -- curl https://example.com
cx run --sub 'https://example.com/subscription' -- git clone https://example.com/repo.git
cx run --log-level info --config direct.yaml -- bash
cx run --ui --config direct.yaml -- curl http://127.0.0.1:9090/ui/
cx run --ui --ui-addr 127.0.0.1:9090 --config direct.yaml -- curl https://example.com
```

Run-mode log level defaults to `warning`; core logs are written only to stderr.
External controller access is disabled unless `--controller ADDR`, `--ui`,
`--ui-addr ADDR`, or `CX_UI`/`CX_UI_ADDR` is given. `--ui` enables the
controller and the bundled panel; `--ui-addr` implies `--ui` and defaults to
`127.0.0.1:9090`. When a controller is enabled and no secret is configured,
`cx run` prints the generated secret only if stderr is a terminal. Otherwise it
writes `<temp-dir>/controller-secret` (mode `0600`) and logs that path; the
temporary directory is removed when the command exits.

### Subscriptions

```sh
cx sub update --dir ~/.config/cx --sub 'https://example.com/subscription'
cx sub update --dir ~/.config/cx --sub URL1 --sub URL2
CX_SUB='URL1 URL2' cx sub update --dir ~/.config/cx
printf '%s\n' URL1 URL2 > subs.txt
cx sub update --dir ~/.config/cx --sub-file subs.txt
```

`CX_SUB` accepts whitespace- or newline-separated URLs. `--sub-file FILE` reads
one URL per line, ignores blank lines and lines starting with `#`, and warns on
Unix when the file is group or world readable. Subscriptions are fetched with
proxy environment variables ignored, a 30-second timeout, a 16 MiB size cap,
user agent `clash-verge/v2.4.3`, and a base64 fallback. `sub update` validates
the merged config with mihomo (warning+ logs only) before replacing the active
file.

### Packing preconfigured builds

```sh
cx pack -o cx-private \
  --config config.yaml \
  --geodata ./geodata \
  --target-binary ./cx \
  --select 'Proxy=hy2 us'

cx-private run -- curl https://example.com
```

`--geodata` picks the known files `geoip.metadb`, `geoip.dat`, `geosite.dat`,
`GeoLite2-ASN.mmdb`, and `ASN.mmdb` from the directory. Subscriptions are
resolved at pack time; subscription URLs are never embedded. Selections passed
to `pack` are stored in the embedded config as `x-cx-select`, so the packed
binary applies them by default; runtime `--select`/`CX_SELECT` overrides them.
The resulting binary is written with mode `0700` and uses its embedded config
and geodata with no flags.

`cx pack` strips the controller `secret` from the embedded config and prints a
note. A later `cx serve` from that binary generates a fresh secret at startup if
the external controller is enabled, storing it in
`<state-dir>/controller-secret`.

> **Security warning:** a packed binary contains resolved proxy configuration
> and credentials. Keep it private. Never commit, upload, publish, or share it.
> If proxies, a remote proxy provider, or any subscription was packed, `cx`
> prints a prominent warning to stderr.

### Inspecting config and payloads

```sh
cx config show                 # YAML on stdout, credentials and secret URL parameters redacted
cx config show --config FILE   # report payload status on stderr
cx version                     # version + embedded payload file list
```

`config show` redacts password/private-key/auth/token/secret fields, URL
userinfo, and secret-looking query parameters (`token`, `key`, `secret`,
`password`, `auth`, `sig`). All query parameters on proxy-provider and
subscription URLs are hidden. DoH paths, geox URLs without credentials, and
rule-provider URLs without secret parameters remain readable. When no release
version is injected, `cx version` falls back to module version or the Git
revision (12 characters, with `-dirty` when modified).

### Tests and release builds

```sh
gofmt -l .
go vet ./...
go test -short ./...
CGO_ENABLED=0 go build ./...
```

CI covers Linux, macOS, and Windows tests plus cross-builds for
linux/darwin/windows/freebsd `amd64`/`arm64` and linux `armv7`. CI and releases
fetch the pinned panel and build with
`-tags "ui with_gvisor no_zerotier no_easytier no_tailscale"`. Tagged releases use the same
tags plus `-trimpath -ldflags "-s -w -X main.version=<tag>"`.

Tagged releases then install pinned UPX 5.2.1 (verified against a hard-coded
SHA-256), compress the Linux `amd64`/`arm64`/`armv7` and Windows
`amd64`/`arm64` artifacts with `--best`, and run `upx -t` on each one
before generating `SHA256SUMS`. Darwin is left uncompressed because UPX changes
the Mach-O layout and can interfere with codesigning; FreeBSD `amd64`/`arm64`
is skipped because UPX 5.2.1 only supports FreeBSD `i386`. `cx pack` keeps the
upstream binary as-is, so a packed binary is not additionally compressed.

## 中文

`cx` 是一个静态 Go 二进制，以库的形式内嵌
[mihomo](https://github.com/MetaCubeX/mihomo)。它可以在前台运行代理服务、
为单条命令启动临时代理、更新订阅，并打包带内嵌配置与 geodata 的预配置二进制。

`cx` 与内嵌的 mihomo 核心均采用 **GPL-3.0** 许可证，详见 [LICENSE](LICENSE)。

### 安装

Release 提供裸二进制文件和 `SHA256SUMS`：

`cx-linux-amd64`、`cx-linux-arm64`、`cx-linux-arm`（armv7）、`cx-darwin-amd64`、
`cx-darwin-arm64`、`cx-windows-amd64.exe`、`cx-windows-arm64.exe`、
`cx-freebsd-amd64`、`cx-freebsd-arm64` 和 `SHA256SUMS`。

Linux（`amd64`、`arm64`、`armv7`）和 Windows（`amd64`、`arm64`）发布二进制使用
UPX 5.2.1（`--best`）压缩；macOS 与 FreeBSD 产物不压缩。要获得未压缩
二进制，可使用 UPX 5.2.1 执行 `upx -d cx-linux-amd64`（或按下文从源码构建）；
`upx -d` 会还原压缩前的可执行文件。带内嵌配置的打包产物不会使用 UPX 压缩。
UPX 以每次启动时的解压延迟换取更小的下载体积；若在意启动延迟，可使用
`upx -d` 或从源码构建。不要对打包后的二进制运行 UPX：UPX 5.2.1 会因已存在的
payload 报 `EOFException` 并保持文件不变，而使用剥离 overlay 的选项会丢弃
payload。

Linux/macOS：

```sh
curl -fLO https://github.com/DawnMagnet/clashix-go/releases/latest/download/cx-linux-amd64
curl -fLO https://github.com/DawnMagnet/clashix-go/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS   # macOS: shasum -a 256 -c SHA256SUMS
chmod +x cx-linux-amd64
sudo install -m 0755 cx-linux-amd64 /usr/local/bin/cx
cx version
```

Windows（PowerShell；`sha256sum` 可在 Git Bash 或 WSL 中执行）：

```powershell
curl.exe -fLO https://github.com/DawnMagnet/clashix-go/releases/latest/download/cx-windows-amd64.exe
curl.exe -fLO https://github.com/DawnMagnet/clashix-go/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
Move-Item cx-windows-amd64.exe cx.exe
.\cx.exe version
```

`cx version` 还会报告是否内嵌 payload，以及其中包含哪些文件。

### 从源码构建

需要 Go 1.27 或更高版本，不需要 CGO：

```sh
git clone https://github.com/DawnMagnet/clashix-go.git
cd clashix-go

# 不带内置面板的精简构建
CGO_ENABLED=0 go build \
  -tags "with_gvisor no_zerotier no_easytier no_tailscale" \
  -trimpath -o cx ./cmd/cx

# 带 metacubexd 面板的构建
./scripts/fetch-ui.sh
CGO_ENABLED=0 go build \
  -tags "ui with_gvisor no_zerotier no_easytier no_tailscale" \
  -trimpath -o cx ./cmd/cx
```

普通 `go build ./cmd/cx` 仍可编译，但会提示面板未内置。
`with_gvisor` tag 会加入 mihomo 的 gVisor TUN 栈（`--tun-stack gvisor` 和
`mixed` 需要）。`no_zerotier no_easytier no_tailscale` 会移除可选的 ZeroTier、
EasyTier 和 Tailscale 出站；主流协议（ss、ssr、vmess、vless、reality、trojan、
hysteria、hysteria2、tuic、wireguard、socks5、http、snell、anytls）保留。mihomo 没有
mieru、SSH、AmneziaWG、Wazero 的排除 tag，因此这些较重依赖仍会保留。

### 配置来源

配置按以下顺序解析：

1. 内嵌 payload（预配置的 `cx` 二进制），
2. `--config FILE`，
3. 一个或多个 `--sub URL` 订阅，
4. 状态目录中的缓存配置。

后续订阅只追加 `proxies`，第一个订阅作为基础文档。受控键（端口、`allow-lan`、
mode、日志等级、控制器、secret）最后覆盖。如果以上来源都不存在，`cx` 会立即
报错并给出可操作的提示，而不会用空配置启动。

覆盖层会深合并到已解析的配置之上：状态目录中的 `override.yaml`（若存在）、
按顺序给出的每个 `--override FILE`，最后是 `CX_OVERRIDE` 指定的文件。映射递归
合并，列表与标量直接替换。有效优先级为 **命令行参数 > 环境变量 > 覆盖文件 >
订阅/配置**。

端口覆盖值 `0` 会禁用对应监听：`--port 0`、`--socks-port 0`、`--mixed-port 0`、
`--redir-port 0`、`--tproxy-port 0`。`cx run` 仍需要混合端口，因此在未指定
`--mixed-port` 或指定为 `0` 时会自动选择。

状态目录默认为用户配置目录下的 `cx`（Linux 通常是 `~/.config/cx`，macOS 是
`~/Library/Application Support/cx`，Windows 是 `%AppData%\cx`）。配置以 `0600`
权限原子写入，上一份有效配置保留为 `config.yaml.last-good`。

### 节点选择

`--select SPEC`（可重复）适用于 `run`、`serve` 和 `pack`。`CX_SELECT` 使用分号
分隔多个 SPEC；本地配置或覆盖文件中的顶层 `x-cx-select` 列表提供默认值。
命令行参数覆盖 `CX_SELECT`，`CX_SELECT` 覆盖 `x-cx-select`。该键不会传给
mihomo。

SPEC 形式：

- `PATTERN`：作用于所有直接包含匹配代理的选择组，并为每个组选择组内第一个匹配
  项。错误信息会说明这一“所有组”行为，并列出真正最接近的代理节点，不会列出组、
  `DIRECT` 或 `REJECT`。没有匹配直接成员的组保持不变；但如果该组把最终 `MATCH`
  规则引用的选择组作为成员，则把该组设置为该选择组。
- `GROUP=PATTERN`：作用于名称匹配 `GROUP` 的选择组。精确组规则在裸模式之后
  执行，并且优先；只有这类错误会点名具体选择组。

最接近候选按真实相似度排序：先看协议/type 匹配（裸协议词与 `type:` 选择器
都算，例如 `trojan` 会优先列出 type 为 `trojan` 的节点），再看地区别名匹配、
共享 token、归一化最长公共子串与共享字符，最后才按原始组顺序。若模式匹配到
存在于顶层 `proxies`、但不属于任何选择组的代理，错误信息会明确说明这一点并
列出这些代理。

以 `re:` 开头的模式是不区分大小写的 Go 正则；否则按空白拆分，每个词都必须
不区分大小写地匹配候选名称的子串；但三个字符及以下的词必须作为完整 token
匹配，因此 `us` 不会匹配 `plus`，`syd` 也不会匹配 `Sydney`。地区词会展开为
对应旗帜和代码别名：
日本/japan/jp → JP 🇯🇵，香港/hongkong/hk → HK 🇭🇰，台湾/taiwan/tw → TW 🇹🇼，
新加坡/singapore/sg → SG 🇸🇬，美国/usa/us → US 🇺🇸，韩国/korea/kr → KR 🇰🇷，
英国/uk/gb → UK/GB 🇬🇧，德国/germany/de → DE 🇩🇪，加拿大/canada/ca → CA 🇨🇦。
`type:PROTO` 匹配代理的 type 字段，可与普通词组合，例如 `type:anytls`、
`type:hysteria2`、`type:hy2 hk`。裸协议词（`trojan`、`anytls`、
`hy2`/`hysteria2`、`hysteria`、`vmess`、`vless`、`ss`/`shadowsocks`、`ssr`、
`tuic`、`wireguard`/`wg`、`socks5`、`http`、`snell`）等同于 `type:PROTO`：
按适配器类型匹配和选择，而不再作为名称子串。不再有模糊子序列回退。每个组内
按成员顺序取第一个匹配项。未指定选择时，为每个选择组选择初始节点会跳过占位/信息节点
（保留地址或空地址，以及名称含流量到期、重置、官网、客服等标记的节点）。
组、`DIRECT` 和 `REJECT` 保持原样。

```sh
cx run --config config.yaml --select 'hy2 us' -- curl https://example.com
cx serve --config config.yaml --select 'Proxy=re:^🇯🇵'
cx serve --config config.yaml --select 'Proxy=🇸🇬 Singapore'
cx serve --config config.yaml --select 'type:anytls hk'
CX_SELECT='Proxy=hy2 us;Auto=re:tokyo' cx serve --config config.yaml
cx pack -o cx-private --config config.yaml --select 'Proxy=hy2 us'
```

### `cx serve`

前台运行服务。日志只写 stderr；SIGINT/SIGTERM 优雅退出。Unix 下 SIGHUP
会重新读取配置来源、校验后应用，校验成功前不会替换 last-good 文件。
`--sub-interval 6h` 可定期刷新订阅并重载。

配置了订阅且存在可用的 `<state-dir>/config.yaml` 或
`config.yaml.last-good` 时，`cx serve` 会立即用缓存配置启动核心，并在后台刷新
订阅。拉取失败或新配置无效时只记录警告，继续使用当前运行配置；只有完全不存在
可用配置且拉取失败时，启动才会失败。

启用任一网络控制器端点（`external-controller` 或 `external-controller-tls`）
且有效配置没有 secret 时，`cx serve` 会在启动时生成随机控制器 secret，以原子
方式写入 `<state-dir>/controller-secret`（权限 `0600`），并只向 stderr 打印该
文件路径，避免 secret 进入 journald。文件非空时会在后续重启中继续复用。配置中
的非空 `secret`、`CX_SECRET` 或 `--secret` 参数始终优先。未启用控制器时不会
添加 secret。若控制器绑定非 loopback 地址且未配置 secret，还会打印额外警告。
启动和重载会等待 proxy/rule provider 加载完成，并对 URL-test 与 fallback 组
触发一次有界初始健康检查，然后再报告配置已生效。

启用控制器且二进制使用 `-tags ui` 构建时，内置 metacubexd 面板会通过 `/ui/`
提供。布尔标志 `--ui`（或布尔风格的 `CX_UI`，例如 `1`、`true`）会启用控制器
和面板。`--ui-addr ADDR`（或 `CX_UI_ADDR`）用于指定监听地址，并隐含启用
`--ui`；默认地址为 `127.0.0.1:9090`。启动时会打印登录 URL。显式
`--ui=false` 时不提供也不打印内置面板地址，即使配置中的控制器仍然启用。面板
版本、校验和与 MIT 许可证记录在 [`docs/THIRD_PARTY.md`](docs/THIRD_PARTY.md)。

```sh
cx serve --config ~/.config/cx/config.yaml
cx serve --config config.yaml --override extra.yaml --select 'Proxy=hy2 us'
cx serve --sub 'https://example.com/subscription' --sub-interval 12h
cx serve --dir /var/lib/cx
cx serve --ui                       # 控制器 + 面板，127.0.0.1:9090
cx serve --ui --ui-addr 0.0.0.0:9090 --secret 'choose-a-strong-secret'
CX_UI=1 CX_UI_ADDR=127.0.0.1:9090 CX_SECRET=... cx serve
```

systemd 单元文件位于
[`packaging/systemd/cx.service`](packaging/systemd/cx.service)：

```sh
sudo install -m 0644 packaging/systemd/cx.service /etc/systemd/system/cx.service
sudo systemctl daemon-reload
sudo systemctl enable --now cx
```

该单元使用 `Type=notify`、`NotifyAccess=main` 和 `TimeoutStartSec=120s`。核心
应用完成且启用的 TUN 就绪后才发送 `READY=1`；重载前后发送带
`MONOTONIC_USEC` 的 `RELOADING=1` 与 `READY=1`，退出时发送 `STOPPING=1`。
简短状态消息和初始健康检查的开始/结果会写入 journal。

该单元使用 `/var/lib/cx`。请先以 root 执行
`cx sub update --dir /var/lib/cx --sub URL`，或把配置复制到
`/var/lib/cx/config.yaml`。单元设置了 `ExecReload=/bin/kill -HUP $MAINPID`，
因此 `sudo systemctl reload cx` 可应用新配置而无需重启。单元使用
`Restart=always`；TUN 所需能力与设备访问见 [TUN 模式](#tun-模式)。仅代理模式
可取消注释 `User=`/`Group=`。

#### 公网服务器加固示例

订阅是不可信输入：订阅中的配置可能设置 `allow-lan: true`，或启用额外监听端口。
覆盖文件在订阅之后应用，因此应显式固定对外暴露面，而不要信任订阅。

```sh
sudo install -d -m 0700 /var/lib/cx /etc/cx
sudo tee /var/lib/cx/override.yaml >/dev/null <<'EOF'
allow-lan: false
bind-address: 127.0.0.1
port: 0
socks-port: 0
redir-port: 0
tproxy-port: 0
mixed-port: 7890
mode: rule
EOF
sudo tee /etc/cx/cx.env >/dev/null <<'EOF'
CX_SUB=https://example.com/subscription
CX_SECRET=choose-a-long-random-secret
EOF
sudo chmod 0600 /etc/cx/cx.env
```

然后用 systemd drop-in 启用仅 loopback 的面板和控制器：

```sh
sudo systemctl edit cx
```

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/cx serve --dir /var/lib/cx --ui-addr 127.0.0.1:9090
```

secret 来自 `/etc/cx/cx.env` 中的 `CX_SECRET`（也可以使用 `--secret`）。
若不想把订阅 URL 写进单元文件，可把每行一个 URL 放入
`/etc/cx/subscriptions`（权限 `0600`），并在 `ExecStart` 中加入
`--sub-file /etc/cx/subscriptions`，不设置 `CX_SUB`。`--ui-addr` 只绑定面板
和控制器；请保持 loopback，并通过 SSH 隧道或带 TLS 与认证的反向代理访问。
不要直接把控制器暴露到公网。

#### TUN 模式

TUN 需要 root 或 `CAP_NET_ADMIN`；若 mihomo 的 DNS 监听绑定 53 端口，还需要
`CAP_NET_BIND_SERVICE`。gVisor 和 mixed 栈要求构建时带 `with_gvisor` tag。
systemd TUN 部署保留
[`packaging/systemd/cx.service`](packaging/systemd/cx.service) 中的加固项，并
安装 drop-in，而不是直接修改单元文件：

```sh
sudo install -d -m 0755 /etc/systemd/system/cx.service.d
sudo install -m 0644 packaging/systemd/cx-tun.conf \
  /etc/systemd/system/cx.service.d/tun.conf
sudo systemctl daemon-reload
sudo systemctl restart cx
```

[`packaging/systemd/cx-tun.conf`](packaging/systemd/cx-tun.conf) 将
`AmbientCapabilities`/`CapabilityBoundingSet` 设为
`CAP_NET_ADMIN CAP_NET_BIND_SERVICE`，用 `DeviceAllow=/dev/net/tun rw` 允许
TUN 设备，并设置 `PrivateDevices=no`。

`cx` 会在启动后和每次重载后检查 mihomo 进程内的 TUN 状态。若配置启用了 TUN
但未成功启动，启动时会打印 mihomo 的底层错误（例如
`configure tun interface: operation not permitted`）以及可能原因（缺少
`CAP_NET_ADMIN`/root、`/dev/net/tun` 不存在或被设备 cgroup/`PrivateDevices`
阻止、未编译 gVisor/mixed 栈），并以非零状态退出。若设备忙
（`EBUSY`/`device or resource busy`），错误会指明该 TUN 设备已被另一个进程或
cx 实例占用，并建议使用 `--tun-device NAME`。重载时打印错误、重新应用上一份
可用配置，并保持服务运行。

相关参数为 `--tun`、`--tun-stack system|gvisor|mixed`、`--tun-device NAME`、
`--tun-route-table N`、`--tun-rule-index N` 和可重复的 `--tun-exclude CIDR`；
它们优先于配置、覆盖文件和 `tun` 段。后两者对应 mihomo 的
`iproute2-table-index`/`iproute2-rule-index`。使用非默认 `--tun-device` 且未显式
指定索引时，`cx` 会派生不同值（避开 2022/9000），避免第二个实例冲突；
`EEXIST`/`file exists` TUN 错误会提示这两个参数。公网服务器应排除
私有、loopback、链路本地、组播、广播、CGNAT 和元数据地址段，并让本地/云域名
不进入 fake-IP。可直接合并的配置片段见
[`examples/override-tun-server.yaml`](examples/override-tun-server.yaml)，包含
system 栈、自动路由/网卡检测、上述路由排除、fake-IP 以及本地/元数据域名过滤。
示例还说明了如何按需加入云厂商内部 DNS/元数据地址（例如阿里云
`100.100.2.136`/`100.100.100.200`、腾讯云 `183.60.83.19`、`169.254.169.254`）
以及主机自身公网 IP。

```sh
cx serve --config config.yaml --override examples/override-tun-server.yaml --tun
cx serve --config config.yaml --tun --tun-stack system \
  --tun-exclude 10.0.0.0/8 --tun-exclude 192.168.0.0/16
```

公网服务器还应排除 `metadata.google.internal`、`metadata.tencentyun.com`、
`100.100.100.200` 等云元数据端点，并用 `fake-ip-filter` 保护 `*.lan`、
`*.local`、`+.localhost`、`+.internal` 及本地/云域名。不要把云元数据流量
路由到第三方代理。

未启用 TUN 时，`cx` 会检测实际生效的路由是否经过宿主 TUN。Linux 上使用
netlink `RTM_GETROUTE` 查询 `1.1.1.1` 和 `2606:4700:4700::1111`，因此
sing-tun 指向 table 2022 之类的策略路由也能被识别，而不是只读主路由表。
Windows 继续使用其平台路由表读取逻辑。若实际出口是 TUN，则从主路由表（254）
的默认路由取物理网卡。若配置中尚未设置 `interface-name`/`routing-mark`，
`cx` 会把出站套接字（包括 DNS）绑定到真实物理默认网卡，并向 stderr 打印一行：
`cx run: host TUN detected (dev Meta); binding outbound to eth0`。这样非 TUN 的
`cx run` 不会被另一个 `cx serve --tun` 再次捕获。macOS 上 `utun` 设备常由系统
（iCloud 私密中继）或 VPN 应用创建，因此 `cx` 不在 macOS 自动绑定；若实际默认
路由是 `utun`，只打印一行提示：代理拨号失败时可重试 `--interface en0`。可用
`--interface NAME` 强制指定网卡，用 `--routing-mark N` 强制指定路由标记，用
`--interface=` 或 `--no-auto-interface` 关闭自动检测。检测失败时静默处理。

#### 运维提示

使用 `systemd-run --on-active=...` 创建的 systemd 临时计时器，在
`systemctl daemon-reload` 后会重新开始倒计时。若需保留原到期时间，请重新创建
计时器。

launchd 配置位于
[`packaging/launchd/com.dawnmagnet.cx.plist`](packaging/launchd/com.dawnmagnet.cx.plist)：

```sh
install -m 0644 packaging/launchd/com.dawnmagnet.cx.plist \
  ~/Library/LaunchAgents/com.dawnmagnet.cx.plist
launchctl bootstrap gui/"$(id -u)" ~/Library/LaunchAgents/com.dawnmagnet.cx.plist
launchctl kickstart -k gui/"$(id -u)"/com.dawnmagnet.cx
launchctl kill SIGHUP gui/"$(id -u)"/com.dawnmagnet.cx   # 重载
```

launchd 没有 `ExecReload` 或 `EnvironmentFile`；请使用 `launchctl kill SIGHUP`
以及 `EnvironmentVariables` 或包装脚本。`KeepAlive=true` 对应
`Restart=always`，`ThrottleInterval` 对应 `RestartSec`。

### `cx run`

在空闲 loopback 混合端口上启动临时代理，导出大小写两套代理环境变量，运行子
进程，转发信号，并以子进程退出码退出。stdin 不是终端时子进程使用独立进程组。
临时状态目录在所有退出路径上都会被删除。`cx run` 默认仅绑定 loopback
（`allow-lan: false`、`bind-address: 127.0.0.1`）；传入 `--allow-lan` 才保留配置
中的局域网绑定。

```sh
cx run --config direct.yaml -- curl https://example.com
cx run --config config.yaml --select 'Proxy=hy2 us' -- curl https://example.com
cx run --config config.yaml --override extra.yaml -- curl https://example.com
cx run --sub 'https://example.com/subscription' -- git clone https://example.com/repo.git
cx run --log-level info --config direct.yaml -- bash
cx run --ui --config direct.yaml -- curl http://127.0.0.1:9090/ui/
cx run --ui --ui-addr 127.0.0.1:9090 --config direct.yaml -- curl https://example.com
```

`run` 模式日志等级默认为 `warning`，核心日志只写 stderr。除非传入
`--controller ADDR`、`--ui`、`--ui-addr ADDR` 或设置 `CX_UI`/`CX_UI_ADDR`，
否则外部控制器关闭。`--ui` 会启用控制器和内置面板；`--ui-addr` 隐含启用
`--ui`，默认地址为 `127.0.0.1:9090`。启用控制器且未配置 secret 时，只有
stderr 是终端才会打印生成的 secret；否则写入 `<临时目录>/controller-secret`
（权限 `0600`）并打印该路径。命令退出时临时目录会被删除。

### 订阅

```sh
cx sub update --dir ~/.config/cx --sub 'https://example.com/subscription'
cx sub update --dir ~/.config/cx --sub URL1 --sub URL2
CX_SUB='URL1 URL2' cx sub update --dir ~/.config/cx
printf '%s\n' URL1 URL2 > subs.txt
cx sub update --dir ~/.config/cx --sub-file subs.txt
```

`CX_SUB` 接受以空白或换行分隔的 URL。`--sub-file FILE` 每行一个 URL，忽略空行
和以 `#` 开头的行；Unix 下若文件可被组或其他用户读取会给出警告。订阅请求忽略
代理环境变量，超时 30 秒，大小上限 16 MiB，User-Agent 为
`clash-verge/v2.4.3`，并支持 base64 回退。`sub update` 会先用 mihomo 校验合并
后的配置（仅输出 warning 及以上日志），再替换活动配置。

### 打包预配置二进制

```sh
cx pack -o cx-private \
  --config config.yaml \
  --geodata ./geodata \
  --target-binary ./cx \
  --select 'Proxy=hy2 us'

cx-private run -- curl https://example.com
```

`--geodata` 只从目录中挑选已知文件：`geoip.metadb`、`geoip.dat`、
`geosite.dat`、`GeoLite2-ASN.mmdb`、`ASN.mmdb`。订阅在打包时解析，订阅 URL
不会写入二进制。传给 `pack` 的选择项会以 `x-cx-select` 存入内嵌配置，打包后的
二进制默认应用；运行时的 `--select`/`CX_SELECT` 会覆盖它。产物以 `0700` 权限
写入，无需任何参数即可使用内嵌配置和 geodata 执行 `run`。

`cx pack` 会从内嵌配置中删除控制器 `secret` 并打印提示。之后用该二进制运行
`cx serve` 时，如果启用了外部控制器，会在启动时生成新的随机 secret，并存入
`<state-dir>/controller-secret`。

> **安全警告：** 打包后的二进制包含解析后的代理配置和凭据。必须保持私有，
> 不要提交、上传、发布或分享。只要打包了代理、远程 proxy-provider 或任何订阅，
> `cx` 都会在 stderr 打印醒目警告。

### 查看配置与 payload

```sh
cx config show                 # stdout 输出 YAML，凭据和敏感 URL 参数已脱敏
cx config show --config FILE   # payload 信息输出到 stderr
cx version                     # 版本 + 内嵌 payload 文件列表
```

`config show` 只脱敏 password/private-key/auth/token/secret 等字段、URL
userinfo，以及名称类似密钥的查询参数（`token`、`key`、`secret`、`password`、
`auth`、`sig`）。proxy-provider 和订阅 URL 的所有查询参数都会隐藏。DoH 路径、
不带凭据的 geox URL，以及不含敏感参数的 rule-provider URL 会保持可读。若未注入
发布版本，`cx version` 会回退到模块版本或 Git revision（12 位，修改过时追加
`-dirty`）。

### 测试与发布构建

```sh
gofmt -l .
go vet ./...
go test -short ./...
CGO_ENABLED=0 go build ./...
```

CI 覆盖 Linux、macOS、Windows 测试，以及 linux/darwin/windows/freebsd
`amd64`/`arm64` 与 linux `armv7` 交叉构建。CI 和发布流程会拉取固定版本面板，
并使用 `-tags "ui with_gvisor no_zerotier no_easytier no_tailscale"` 构建。打 tag 发布时
还使用 `-trimpath -ldflags "-s -w -X main.version=<tag>"`。

打 tag 发布时还会安装固定版本 UPX 5.2.1（使用硬编码 SHA-256 校验），对 Linux
`amd64`/`arm64`/`armv7` 与 Windows `amd64`/`arm64` 产物使用 `--best`
压缩，并在生成 `SHA256SUMS` 前对每个产物执行 `upx -t`。Darwin 不压缩，因为
UPX 会改变 Mach-O 布局，可能影响代码签名；FreeBSD `amd64`/`arm64` 跳过，
因为 UPX 5.2.1 仅支持 FreeBSD `i386`。`cx pack` 产物保持上游二进制不变，
不会再次压缩。

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
  -tags "no_zerotier no_easytier no_tailscale" \
  -trimpath -o cx ./cmd/cx

# Build with the bundled metacubexd panel.
./scripts/fetch-ui.sh
CGO_ENABLED=0 go build \
  -tags "ui no_zerotier no_easytier no_tailscale" \
  -trimpath -o cx ./cmd/cx
```

A plain `go build ./cmd/cx` still compiles, but reports the panel as not bundled.
The `no_zerotier no_easytier no_tailscale` tags drop the optional ZeroTier,
EasyTier, and Tailscale outbounds; mainstream protocols (ss, ssr, vmess, vless,
reality, trojan, hysteria, hysteria2, tuic, wireguard, socks5, http, snell,
anytls) remain. Mihomo has no exclusion tags for mieru, SSH, AmneziaWG, or
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
  proxy, choosing that group's first match. Groups without a matching direct
  member are left unchanged, except that a group containing the primary
  selector group as a member is set to the primary group. The primary selector
  group is the select group used by the final `MATCH` rule, otherwise the first
  select group in `proxy-groups`. A bare pattern errors only when no group
  contains a match, listing closest candidates from the primary group.
- `GROUP=PATTERN` targets a select group whose name matches `GROUP`. Precise
  group specs run after bare patterns and win.

A pattern starting with `re:` is a case-insensitive Go regular expression.
Otherwise every whitespace-separated term must be a case-insensitive substring
of the candidate name, with a fuzzy subsequence fallback. Within each group,
the first matching member in member order wins. Without a selection,
placeholder/info nodes (reserved or empty servers, and names containing traffic
expiry/reset/website/support markers) are skipped when choosing every select
group's initial node. Groups, `DIRECT`, and `REJECT` are kept as-is.

```sh
cx run --config config.yaml --select 'hy2 us' -- curl https://example.com
cx serve --config config.yaml --select 'Proxy=re:^🇯🇵'
cx serve --config config.yaml --select 'Proxy=🇸🇬 Singapore'
CX_SELECT='Proxy=hy2 us;Auto=re:tokyo' cx serve --config config.yaml
cx pack -o cx-private --config config.yaml --select 'Proxy=hy2 us'
```

### `cx serve`

Run the service in the foreground. Logs go to stderr. SIGINT/SIGTERM stop it
gracefully. On Unix, SIGHUP re-reads the configuration sources, validates the
result, and applies it without replacing the last-good file until validation
succeeds. `--sub-interval 6h` periodically refreshes subscriptions and reloads.

When any network controller endpoint (`external-controller` or
`external-controller-tls`) is enabled and the effective config has no secret,
`cx serve` generates a random controller secret at startup and prints it to
stderr once so you can log in. A non-empty `secret` in the config, `CX_SECRET`,
or `--secret` always wins. When no controller is enabled, no secret is added.
Binding a non-loopback controller without a configured secret prints an
additional warning.

The built-in metacubexd panel is served at `/ui/` whenever a controller is
enabled and the binary was built with `-tags ui`. The boolean `--ui` flag (or a
boolean-like `CX_UI`, for example `1` or `true`) enables the controller and
panel. `--ui-addr ADDR` (or `CX_UI_ADDR`) chooses the bind address and implies
`--ui`; the default is `127.0.0.1:9090`. The login URL is printed at startup.
Panel version, checksum, and MIT license are recorded in
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

The packaged unit uses `/var/lib/cx` as its state directory. Seed it first with
`cx sub update --dir /var/lib/cx --sub URL` (run as root), or copy a config to
`/var/lib/cx/config.yaml`. TUN mode may need root; for proxy-only operation
uncomment `User=`/`Group=`.

A launchd agent is provided at
[`packaging/launchd/com.dawnmagnet.cx.plist`](packaging/launchd/com.dawnmagnet.cx.plist):

```sh
install -m 0644 packaging/launchd/com.dawnmagnet.cx.plist \
  ~/Library/LaunchAgents/com.dawnmagnet.cx.plist
launchctl bootstrap gui/"$(id -u)" ~/Library/LaunchAgents/com.dawnmagnet.cx.plist
launchctl kickstart -k gui/"$(id -u)"/com.dawnmagnet.cx
```

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
`127.0.0.1:9090`. As in `serve`, a missing secret is generated and printed once.

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
the external controller is enabled.

> **Security warning:** a packed binary contains resolved proxy configuration
> and credentials. Keep it private. Never commit, upload, publish, or share it.
> If proxies, a remote proxy provider, or any subscription was packed, `cx`
> prints a prominent warning to stderr.

### Inspecting config and payloads

```sh
cx config show                 # YAML on stdout, secrets redacted
cx config show --config FILE   # report payload status on stderr
cx version                     # version + embedded payload file list
```

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
`-tags "ui no_zerotier no_easytier no_tailscale"`. Tagged releases use the same
tags plus `-trimpath -ldflags "-s -w -X main.version=<tag>"`.

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
  -tags "no_zerotier no_easytier no_tailscale" \
  -trimpath -o cx ./cmd/cx

# 带 metacubexd 面板的构建
./scripts/fetch-ui.sh
CGO_ENABLED=0 go build \
  -tags "ui no_zerotier no_easytier no_tailscale" \
  -trimpath -o cx ./cmd/cx
```

普通 `go build ./cmd/cx` 仍可编译，但会提示面板未内置。
`no_zerotier no_easytier no_tailscale` 会移除可选的 ZeroTier、EasyTier 和
Tailscale 出站；主流协议（ss、ssr、vmess、vless、reality、trojan、hysteria、
hysteria2、tuic、wireguard、socks5、http、snell、anytls）保留。mihomo 没有
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
  项。没有匹配直接成员的组保持不变；但如果该组把主选择组作为成员，则把该组设置
  为主选择组。主选择组是最终 `MATCH` 规则引用的选择组；若它不存在或不是选择组，
  则取 `proxy-groups` 中第一个选择组。裸模式只有在所有组都没有匹配时才报错，并
  列出主选择组中最接近的候选。
- `GROUP=PATTERN`：作用于名称匹配 `GROUP` 的选择组。精确组规则在裸模式之后
  执行，并且优先。

以 `re:` 开头的模式是不区分大小写的 Go 正则；否则按空白拆分，每个词都必须
不区分大小写地作为候选名称的子串，若都不匹配再回退到模糊子序列匹配。每个组内
按成员顺序取第一个匹配项。未指定选择时，为每个选择组选择初始节点会跳过占位/信息
节点（保留地址或空地址，以及名称含流量到期、重置、官网、客服等标记的节点）。
组、`DIRECT` 和 `REJECT` 保持原样。

```sh
cx run --config config.yaml --select 'hy2 us' -- curl https://example.com
cx serve --config config.yaml --select 'Proxy=re:^🇯🇵'
cx serve --config config.yaml --select 'Proxy=🇸🇬 Singapore'
CX_SELECT='Proxy=hy2 us;Auto=re:tokyo' cx serve --config config.yaml
cx pack -o cx-private --config config.yaml --select 'Proxy=hy2 us'
```

### `cx serve`

前台运行服务。日志只写 stderr；SIGINT/SIGTERM 优雅退出。Unix 下 SIGHUP
会重新读取配置来源、校验后应用，校验成功前不会替换 last-good 文件。
`--sub-interval 6h` 可定期刷新订阅并重载。

启用任一网络控制器端点（`external-controller` 或 `external-controller-tls`）
且有效配置没有 secret 时，`cx serve` 会在启动时生成随机控制器 secret，并向
stderr 打印一次，方便登录。配置中的非空 `secret`、`CX_SECRET` 或 `--secret`
参数始终优先。未启用控制器时不会添加 secret。若控制器绑定非 loopback 地址且
未配置 secret，还会打印额外警告。

启用控制器且二进制使用 `-tags ui` 构建时，内置 metacubexd 面板会通过 `/ui/`
提供。布尔标志 `--ui`（或布尔风格的 `CX_UI`，例如 `1`、`true`）会启用控制器
和面板。`--ui-addr ADDR`（或 `CX_UI_ADDR`）用于指定监听地址，并隐含启用
`--ui`；默认地址为 `127.0.0.1:9090`。启动时会打印登录 URL。面板版本、校验和
与 MIT 许可证记录在 [`docs/THIRD_PARTY.md`](docs/THIRD_PARTY.md)。

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

该单元使用 `/var/lib/cx`。请先以 root 执行
`cx sub update --dir /var/lib/cx --sub URL`，或把配置复制到
`/var/lib/cx/config.yaml`。TUN 模式可能需要 root；仅代理模式可取消注释
`User=`/`Group=`。

launchd 配置位于
[`packaging/launchd/com.dawnmagnet.cx.plist`](packaging/launchd/com.dawnmagnet.cx.plist)：

```sh
install -m 0644 packaging/launchd/com.dawnmagnet.cx.plist \
  ~/Library/LaunchAgents/com.dawnmagnet.cx.plist
launchctl bootstrap gui/"$(id -u)" ~/Library/LaunchAgents/com.dawnmagnet.cx.plist
launchctl kickstart -k gui/"$(id -u)"/com.dawnmagnet.cx
```

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
`--ui`，默认地址为 `127.0.0.1:9090`。与 `serve` 一样，缺少 secret 时会自动
生成并打印一次。

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
`cx serve` 时，如果启用了外部控制器，会在启动时生成新的随机 secret。

> **安全警告：** 打包后的二进制包含解析后的代理配置和凭据。必须保持私有，
> 不要提交、上传、发布或分享。只要打包了代理、远程 proxy-provider 或任何订阅，
> `cx` 都会在 stderr 打印醒目警告。

### 查看配置与 payload

```sh
cx config show                 # stdout 输出 YAML，secret 与凭据已脱敏
cx config show --config FILE   # payload 信息输出到 stderr
cx version                     # 版本 + 内嵌 payload 文件列表
```

### 测试与发布构建

```sh
gofmt -l .
go vet ./...
go test -short ./...
CGO_ENABLED=0 go build ./...
```

CI 覆盖 Linux、macOS、Windows 测试，以及 linux/darwin/windows/freebsd
`amd64`/`arm64` 与 linux `armv7` 交叉构建。CI 和发布流程会拉取固定版本面板，
并使用 `-tags "ui no_zerotier no_easytier no_tailscale"` 构建。打 tag 发布时
还使用 `-trimpath -ldflags "-s -w -X main.version=<tag>"`。

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

The state directory defaults to the per-user config directory plus `cx`
(for example `~/.config/cx` on Linux, `~/Library/Application Support/cx` on
macOS, or `%AppData%\cx` on Windows). Files are written atomically with mode
`0600`; the previous good config is kept as `config.yaml.last-good`.

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
enabled and the binary was built with `-tags ui`. `--ui` (or `CX_UI`) enables
the controller and panel; `--ui` alone uses `127.0.0.1:9090`. The login URL is
printed at startup. Panel version, checksum, and MIT license are recorded in
[`docs/THIRD_PARTY.md`](docs/THIRD_PARTY.md).

```sh
cx serve --config ~/.config/cx/config.yaml
cx serve --sub 'https://example.com/subscription' --sub-interval 12h
cx serve --dir /var/lib/cx
cx serve --ui                       # controller + panel on 127.0.0.1:9090
cx serve --ui 0.0.0.0:9090 --secret 'choose-a-strong-secret'
CX_UI=127.0.0.1:9090 CX_SECRET=... cx serve
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
temporary state directory is always removed.

```sh
cx run --config direct.yaml -- curl https://example.com
cx run --sub 'https://example.com/subscription' -- git clone https://example.com/repo.git
cx run --log-level info --config direct.yaml -- bash
cx run --ui --config direct.yaml -- curl http://127.0.0.1:9090/ui/
```

Run-mode log level defaults to `warning`; core logs are written only to stderr.
External controller access is disabled unless `--controller ADDR`, `--ui`, or
`CX_UI` is given. `--ui` enables the controller and the bundled panel; `--ui`
alone uses `127.0.0.1:9090`. As in `serve`, a missing secret is generated and
printed once.

### Subscriptions

```sh
cx sub update --dir ~/.config/cx --sub 'https://example.com/subscription'
cx sub update --dir ~/.config/cx --sub URL1 --sub URL2
```

Subscriptions are fetched with proxy environment variables ignored, a 30-second
timeout, a 16 MiB size cap, user agent `clash-verge/v2.4.3`, and a base64
fallback. `sub update` validates the merged config with mihomo before replacing
the active file.

### Packing preconfigured builds

```sh
cx pack -o cx-private \
  --config config.yaml \
  --geodata ./geodata \
  --target-binary ./cx

cx-private run -- curl https://example.com
```

`--geodata` picks the known files `geoip.metadb`, `geoip.dat`, `geosite.dat`,
`GeoLite2-ASN.mmdb`, and `ASN.mmdb` from the directory. Subscriptions are
resolved at pack time; subscription URLs are never embedded. The resulting
binary is written with mode `0700` and uses its embedded config and geodata with
no flags.

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

状态目录默认为用户配置目录下的 `cx`（Linux 通常是 `~/.config/cx`，macOS 是
`~/Library/Application Support/cx`，Windows 是 `%AppData%\cx`）。配置以 `0600`
权限原子写入，上一份有效配置保留为 `config.yaml.last-good`。

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
提供。`--ui`（或 `CX_UI`）会启用控制器和面板；单独使用 `--ui` 时地址为
`127.0.0.1:9090`。启动时会打印登录 URL。面板版本、校验和与 MIT 许可证记录在
[`docs/THIRD_PARTY.md`](docs/THIRD_PARTY.md)。

```sh
cx serve --config ~/.config/cx/config.yaml
cx serve --sub 'https://example.com/subscription' --sub-interval 12h
cx serve --dir /var/lib/cx
cx serve --ui                       # 控制器 + 面板，127.0.0.1:9090
cx serve --ui 0.0.0.0:9090 --secret 'choose-a-strong-secret'
CX_UI=127.0.0.1:9090 CX_SECRET=... cx serve
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
临时状态目录在所有退出路径上都会被删除。

```sh
cx run --config direct.yaml -- curl https://example.com
cx run --sub 'https://example.com/subscription' -- git clone https://example.com/repo.git
cx run --log-level info --config direct.yaml -- bash
cx run --ui --config direct.yaml -- curl http://127.0.0.1:9090/ui/
```

`run` 模式日志等级默认为 `warning`，核心日志只写 stderr。除非传入
`--controller ADDR`、`--ui` 或设置 `CX_UI`，否则外部控制器关闭。`--ui`
会启用控制器和内置面板；单独使用 `--ui` 时地址为 `127.0.0.1:9090`。
与 `serve` 一样，缺少 secret 时会自动生成并打印一次。

### 订阅

```sh
cx sub update --dir ~/.config/cx --sub 'https://example.com/subscription'
cx sub update --dir ~/.config/cx --sub URL1 --sub URL2
```

订阅请求忽略代理环境变量，超时 30 秒，大小上限 16 MiB，User-Agent 为
`clash-verge/v2.4.3`，并支持 base64 回退。`sub update` 会先用 mihomo 校验合并
后的配置，再替换活动配置。

### 打包预配置二进制

```sh
cx pack -o cx-private \
  --config config.yaml \
  --geodata ./geodata \
  --target-binary ./cx

cx-private run -- curl https://example.com
```

`--geodata` 只从目录中挑选已知文件：`geoip.metadb`、`geoip.dat`、
`geosite.dat`、`GeoLite2-ASN.mmdb`、`ASN.mmdb`。订阅在打包时解析，订阅 URL
不会写入二进制。产物以 `0700` 权限写入，无需任何参数即可使用内嵌配置和
geodata 执行 `run`。

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

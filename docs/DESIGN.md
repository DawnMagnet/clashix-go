# cx — Clashix Go rewrite: design

## Background
The original Clashix is a set of Nix modules (NixOS / Home Manager / devShell) wrapping the
`mihomo` binary: it renders a base config, fetches subscriptions (first URL = primary config,
later URLs contribute only `proxies`; base64 fallback; validate before replace; keep
`config.yaml.last-good`), overlays controlled keys (ports, mode, log-level, controller, secret,
TUN/DNS), pre-seeds geodata (geoip.metadb / geoip.dat / geosite.dat), and offers a shell mode
that starts mihomo in a temp state dir and exports proxy env vars.

## Goals
One static Go binary `cx` (CGO_ENABLED=0) sharing core, config handling and lifecycle for:
- `cx serve` — foreground service (systemd/launchd friendly: no daemonizing, logs to stderr,
  graceful stop on SIGINT/SIGTERM, reload config on SIGHUP where supported).
- `cx run [flags] -- <cmd> [args...]` — start an ephemeral proxy on free loopback ports, run the
  command with proxy env vars, forward signals, propagate exit code, then stop the core and
  remove the temp state dir.

## Core: mihomo as a library
`github.com/metacubex/mihomo` is embedded in-process (`hub/executor`, `config.Parse`,
`constant.SetHomeDir`). No subprocess, no external binary, single static artifact.
Consequence: the combined work is GPL-3.0 (mihomo license); cx is therefore GPL-3.0.
Only one core instance per process (mihomo uses globals) — fine for both modes.

## Config model (internal/config)
Sources, in order: embedded payload (preconfigured build) → `--config FILE` → subscriptions
(`--sub URL`, repeatable, or from `cx.yaml`). Subscription merge rules identical to Clashix.
Overlay of controlled keys (ports, allow-lan, mode, log-level, external-controller, secret)
applied last. Fetch with proxy env vars ignored, UA `clash-verge/v2.4.3`, timeout, size cap.
Validation via mihomo parser before atomically replacing the active config; last-good kept.
`cx sub update` refreshes the cached subscription config in the state dir.

State dir: `serve` uses `--dir` (default per-OS user config dir `.../cx`); `run` uses a fresh
`os.MkdirTemp` dir removed on exit. Geodata from embedded payload is written into the dir.

## Embedded / preconfigured builds (internal/payload)
Payload = tar (config.yaml + optional geodata files) appended to a copy of the executable,
followed by a trailer: 8-byte length + 8-byte magic `CXPAYLD1`. At startup cx reads its own
executable tail. `cx pack -o out [--config F] [--sub URL] [--geodata DIR] [--target-binary B]`
produces a new binary without a Go toolchain; with `--target-binary` a release binary for a
different OS/arch can be packed. Works for ELF/Mach-O/PE as trailing data is ignored by loaders
(macOS: packed binary must be re-signed ad-hoc by the user; documented).

## Secrets
- Public CI artifacts are always empty-config builds; CI never receives subscription data.
- `cx pack` resolves subscriptions locally and embeds the *resolved config*, never the URL,
  unless `--keep-sub-urls`. It warns loudly that the output contains proxy credentials and
  must not be published. Controller secret is randomized per run unless configured.
- Files written with 0600, dirs 0700.

## Proxy environment for `run`
Set `http_proxy https_proxy all_proxy no_proxy` and upper-case variants to the mixed port
(`http://127.0.0.1:P`, `all_proxy=socks5h://127.0.0.1:P`), `no_proxy=localhost,127.0.0.1,::1`.
Ports chosen by binding `127.0.0.1:0` (overridable). Child is in its own process group on Unix;
on cancel cx sends SIGTERM to the group, then SIGKILL after a grace period.

## Layout
cmd/cx (main) · internal/cli · internal/config · internal/core · internal/payload ·
internal/runner · .github/workflows (ci.yml, release.yml on tags v*).
Release matrix: linux/darwin/windows × amd64/arm64 (+linux/arm/v7 optional), `-trimpath
-ldflags "-s -w -X main.version"`, SHA256SUMS.

# Third-party components

`cx` is distributed under GPL-3.0. The following third-party components are
included or fetched for release builds.

## mihomo

- Project: <https://github.com/MetaCubeX/mihomo>
- Version: `v1.19.32` (Go module `github.com/metacubex/mihomo`)
- License: GPL-3.0
- Usage: embedded in-process proxy core.

## metacubexd web panel

- Project: <https://github.com/MetaCubeX/metacubexd>
- Version: `v1.273.1`
- Release asset: `compressed-dist.tgz`
- Download: <https://github.com/MetaCubeX/metacubexd/releases/download/v1.273.1/compressed-dist.tgz>
- SHA-256: `a178e00b67acabcda2dcef00afa90be6a7bb261e466a67dad58c8478d9553603`
- License: MIT (<https://github.com/MetaCubeX/metacubexd/blob/main/LICENSE>)
- Usage: optional dashboard assets, fetched by `scripts/fetch-ui.sh` and embedded
  into binaries built with `-tags ui`. The MIT license is compatible with
  GPL-3.0.

The fetched assets are not committed to this repository. `internal/ui/dist/` is
ignored by Git.

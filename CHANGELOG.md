# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

### Added

- Optional built-in metacubexd web panel, fetched at build time and enabled with
  `--ui`, `CX_UI`, or `external-controller`.
- Payload reporting in `cx version` and `cx config show`.
- Documentation, packaging files, CI, and release automation.

### Changed

- `--ui` is now a boolean switch; use `--ui-addr ADDR` or `CX_UI_ADDR` to
  choose the controller and panel address.
- `cx run` now defaults to `log-level: warning`, keeps core logs on stderr, and
  suppresses info-level shutdown logs.
- Commands fail with an actionable error when no configuration source exists.
- Packed binaries are written with mode `0700`.
- Release builds use `no_zerotier no_easytier no_tailscale` to drop optional
  outbounds and reduce binary size.

### Fixed

- TTY-mode `cx run` now escalates a surviving child from SIGINT to SIGTERM and
  then SIGKILL after the grace periods.
- `cx run` forwards the signal-aware context to the child runner and cleans up
  isolated process groups before returning.
- `cx run` rejects occupied mixed ports and verifies mihomo actually bound the
  selected port before launching the child.
- Subscription fetch errors use sanitized URLs, hiding credentials, query
  parameters, and path tokens.
- `cx config show` redacts additional credential fields, authorization headers,
  URL userinfo, query strings, and URL paths.
- `cx run` disables TLS, Unix-socket, and named-pipe controller endpoints.
- The bundled panel now embeds underscore-prefixed asset directories such as
  `_nuxt/` and `_fonts/`.
- The expected external-controller "Server closed" error is filtered from
  shutdown logs.
- Windows: mihomo's cache database is closed on Stop so temporary state
  directories can be removed.
- Windows: occupied-port detection recognizes WSAEADDRINUSE.
- Windows: in-place `cx pack` closes source handles before replacement and
  refuses to overwrite the running executable.
- CI actions updated to current major versions.

## [0.1.0] - 2026-10-18

### Added

- Static `cx` binary with `serve`, `run`, `sub update`, `pack`, `config show`,
  and `version` commands.
- Embedded mihomo core with atomic config writes, last-good copies, and mihomo
  validation.
- Subscription fetching with base64 fallback, merge rules, and controlled-key
  overlays.
- Embedded payload support for preconfigured builds, including geodata.
- Ephemeral proxied command execution with process-group signal forwarding.
- Optional payload geodata and jsDelivr `geox-url` defaults.

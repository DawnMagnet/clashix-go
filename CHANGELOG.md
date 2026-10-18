# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

### Added

- Payload reporting in `cx version` and `cx config show`.
- Documentation, packaging files, CI, and release automation.

### Changed

- `cx run` now defaults to `log-level: warning`, keeps core logs on stderr, and
  suppresses info-level shutdown logs.
- Commands fail with an actionable error when no configuration source exists.
- Packed binaries are written with mode `0700`.

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

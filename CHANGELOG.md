# Changelog

All notable changes to fluxzy-console are documented here. Versions follow
[Semantic Versioning](https://semver.org/); `VERSION` is the single source of
truth.

## [1.0.0] - 2026-11-01

### Added

- Single-container supervisor for the bundled patched fluxzy CLI.
- Live traffic, session history, request/response details and body previews.
- Rule editor with nested AND/OR filters and request/response body transforms.
- In-process rule hot reload with pending/live staging and failure rollback.
- Runtime startup configuration, retention policy and rotation policy in the web UI.
- Session rotation by time, exchange count, session size and free disk space.
- Custom CA upload, regeneration and reset.
- Audit log, Prometheus metrics and light/dark Chinese-first UI.
- GitHub Actions CI and release workflow publishing `linux/amd64` images to GHCR.

### Changed

- Bundled fluxzy upgraded to upstream `1.39.52.38106`.
- All application configuration is now built in; runtime-tunable settings live in
  the web UI and SQLite. No application environment variables are read.
- `docker-compose.yml` contains no configuration environment variables.
- Default administrator is `admin / admin`; change it after first login.
- Password minimum length is 5 characters.

### Fixed

- Rule save/apply staging semantics, runtime config applied state and failure rollback.
- Login rate limiting and KDF concurrency limits.
- Retention/indexing data-loss paths and index scan reliability.
- WebSocket/response-size caps, proxy header handling and shutdown ordering.

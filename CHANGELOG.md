# Changelog

All notable changes to this project are documented here.

## [1.0.0] - Unreleased

### Added

- Versioned PostgreSQL migrations for the eight application tables, cart persistence, user token-version revocation, and legacy column-contract convergence, with fresh/adopted equivalence and NULL-preflight integration tests.
- Atomic inventory delta adjustments that cannot overwrite concurrent order deductions.
- `/livez`, `/readyz`, and `/health` compatibility health endpoints.
- Validated PostgreSQL max-open, max-idle, lifetime, and idle-time settings.
- Eight reproducible k6 release scenarios and a measured local smoke report.
- CI gates for migrations, formatting of changed files, vet, tests, race detection, Staticcheck, Govulncheck, Actionlint, Gitleaks, Trivy, and container builds.
- A release checklist and explicit deployment/rollback procedure.

### Changed

- Redis increment and first expiration now execute atomically with Lua, including repair of legacy counters that have no TTL.
- Product list caches use normalized query hashes and namespace versions; detail invalidation uses exact keys.
- Generic Redis pattern maintenance uses `SCAN` instead of `KEYS`.
- Production startup rejects GORM `AutoMigrate` and relies on reviewed migrations.
- Legacy-schema hardening checks ambiguous NULL data before bounded default backfills and documents dirty-state recovery.
- Cart creation and line replacement now use explicit transaction-scoped repository operations, and database failures are no longer treated as a missing cart.
- The runtime image is multi-stage, non-root, minimal, read-only under Compose, and healthchecked through `/livez`.
- The release toolchain baseline is Go 1.26.5.

### Security

- gRPC access and refresh token types are enforced per method.
- Changing a password increments the user token version and revokes all previously issued refresh tokens.
- Password hashes now use bcrypt default cost, hashing errors propagate, and successful login upgrades legacy low-cost hashes.
- Order idempotency is durable in PostgreSQL with request fingerprints and remains correct after Redis completion loss or expiry.
- Product prices are restricted to two decimals and order totals use checked integer minor-unit arithmetic.
- Swagger and metrics are opt-in in production, while remaining convenient defaults in development.
- GORM logs retain SQL templates but omit parameter values, JWTs contain only required authorization claims, and release/CI container dependencies are digest-pinned.
- Malformed gRPC JWT identity claims return `Unauthenticated` instead of panicking.
- gRPC reflection is disabled by default in production.
- Local configuration and environment files are excluded from the container build context.
- JWT, Redis, PostgreSQL, gRPC, and Go security-sensitive dependencies were upgraded; Govulncheck reports no reachable vulnerabilities.
- Protobuf stubs were regenerated with current Go generators so Staticcheck covers generated packages without exclusions.

### Reliability

- Order rate-limit and consumer-failure keys cannot be left without TTL because of an `INCR`/`EXPIRE` gap.
- Readiness validates PostgreSQL, required schema tables, Redis, and production configuration without leaking raw dependency errors.
- CI applies migrations to a fresh database before integration tests.

[1.0.0]: https://github.com/Neura-Shadow/Scalable-E-Commerce-Platform/releases/tag/v1.0.0

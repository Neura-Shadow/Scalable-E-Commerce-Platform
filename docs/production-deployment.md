# Production Deployment

This repository is a production-minded, single-region e-commerce backend. It does not implement multi-region active-active operation, Redis Cluster, cross-region caching, database sharding, service mesh, or multi-region failover. Measured local smoke results are recorded in `docs/benchmark-report-v1.md`; they are not a capacity guarantee.

## Runtime services

- one or more API replicas built from the reviewed image
- PostgreSQL as the system of record
- Redis for cache, idempotency, rate limits, and optional Streams processing
- TLS termination, restricted metrics access, logs, metrics, and alerts

Run migrations as a deployment job before API rollout. Do not let every API replica mutate the schema at startup.

## Required configuration

Supply configuration through environment variables or a secret manager. Never bake `pkg/config/config.yaml`, `.env` files, JWTs, database credentials, or Redis credentials into an image.

```text
environment=production
http_port
grpc_port
grpc_reflection_enabled=false
swagger_enabled=false
auth_secret
database_uri
database_auto_migrate=false
database_max_open_conns
database_max_idle_conns
database_conn_max_lifetime_seconds
database_conn_max_idle_time_seconds
redis_uri
redis_password
redis_db
metrics_enabled=false
```

Production validation rejects missing ports, database/Redis endpoints, placeholder JWT secrets, invalid booleans, invalid pool settings, and `database_auto_migrate=true`.

The sample file also documents HTTP limits, metrics, order protection, publisher, and consumer settings. Keep `/metrics` internal or restrict it to trusted Prometheus scrapers. gRPC reflection is disabled by default in production and may be explicitly enabled only for a controlled diagnostic environment.

Apply account and source-aware throttling to HTTP and gRPC login/registration at the ingress or API gateway. The application rate-limits checkout, but this release does not implement authentication-endpoint throttling itself. Treat a deployment without an equivalent edge policy as incomplete.

Refresh tokens are 30-day bearer tokens with user-wide token-version revocation after a password change. They are not rotated per use and there is no per-session logout store; a stolen refresh token therefore remains usable until expiry or password-change revocation. Access tokens expire after five minutes, so an already issued access token may remain valid for that bounded period after a password change.

## Database migrations

The authoritative schema is under `migrations/`. Version 1 adopts or creates the six core tables and outbox schema after a legacy-data preflight, version 2 adds `carts` and `cart_lines`, version 3 adds the user token version used for refresh-token revocation, version 4 converges legacy AutoMigrate column nullability/defaults, and version 5 adds durable order idempotency plus two-decimal monetary constraints. Version 4 backfills only safe defaults. Versions 1, 4, and 5 stop before their schema changes when ambiguous or constraint-violating legacy data requires operator remediation. The release uses `golang-migrate` v4.18.3 because it supports versioned SQL, PostgreSQL, deterministic up/down execution, and a small deployment-time CLI. It is intentionally kept outside the application dependency graph.

Install the pinned CLI:

```bash
go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3
```

Apply, inspect, and roll back one reviewed migration:

```bash
migrate -path migrations -database "$DATABASE_URI" up
migrate -path migrations -database "$DATABASE_URI" version
migrate -path migrations -database "$DATABASE_URI" down 1
```

`down 1` is for an explicitly reviewed rollback and removes version 5 idempotency metadata, so it must not be used while clients rely on durable replay. Version 4 intentionally keeps its stricter column contracts when rolled back because automatically restoring nullable production columns is unsafe; version 3 application code remains compatible with that stricter schema. The initial down migration drops production tables and must never be run against data that must be retained. A second `up` is safe and reports no change. CI applies migrations to an empty PostgreSQL database before integration tests and validates indexes, constraints, re-application, down behavior, fresh/adopted column equivalence, and legacy preflight recovery.

An ambiguous-NULL preflight failure is reported as migration version 4 in a dirty state. The preflight runs before any v4 backfill. After taking a backup, repair the named table and column, verify that no other ambiguous NULL values remain, reset only the migration metadata to the last completed version, and rerun the remaining migrations:

```bash
migrate -path migrations -database "$DATABASE_URI" version
# Repair and verify the data named by the preflight error in PostgreSQL.
migrate -path migrations -database "$DATABASE_URI" force 3
migrate -path migrations -database "$DATABASE_URI" up
migrate -path migrations -database "$DATABASE_URI" version
```

The final command must report version 5 without `dirty`. `force 3` does not execute rollback SQL and is verified only for this v4 preflight path.

A version 1 legacy-data preflight failure occurs before v1 creates indexes or constraints. After backup and repair of the exact duplicate, invalid status, negative value, or orphan named by the error, use the tested recovery sequence:

```bash
migrate -path migrations -database "$DATABASE_URI" version
# Repair and verify the legacy invariant named by the preflight error.
migrate -path migrations -database "$DATABASE_URI" force -- -1
migrate -path migrations -database "$DATABASE_URI" up
migrate -path migrations -database "$DATABASE_URI" version
```

A version 5 monetary-scale preflight failure requires repairing values with more than two decimal places according to business policy, then `force 4` and `up`. Never use `force` for an unexplained failure or after a migration that may have partially changed schema; restore from backup or obtain a database review instead.

Development and tests may opt into GORM `AutoMigrate`, but production startup requires `database_auto_migrate=false`. Do not mix both schema ownership models in production.

## PostgreSQL pool sizing

The application applies all four settings:

```text
database_max_open_conns=25
database_max_idle_conns=5
database_conn_max_lifetime_seconds=300
database_conn_max_idle_time_seconds=60
```

The defaults are safe starting points, not throughput recommendations. Calculate the per-replica maximum from PostgreSQL `max_connections` after reserving capacity for migrations, workers, monitoring, backups, and administrators. The sum across API replicas must remain below that budget. Increasing the pool can increase contention and does not automatically improve throughput.

## Health endpoints

- `GET /livez` proves only that the process and HTTP server are alive.
- `GET /health` is a compatibility alias of `/livez`.
- `GET /readyz` checks production configuration, PostgreSQL connectivity, required schema tables, and Redis connectivity with short timeouts.

Readiness responses expose only `ready` or `unavailable` component states. They do not return DSNs, credentials, hostnames, or raw errors. Outbox backlog, Redis pending entries, and DLQ entries belong in metrics and alerts and do not make the API unready.

## Container workflow

Build the release image:

```bash
docker build -t scalable-ecommerce-platform:v1.0.0 .
```

The image uses a multi-stage Go build, an Alpine runtime without a compiler, UID/GID `10001`, explicit HTTP/gRPC ports, `SIGTERM`, and a `/livez` healthcheck. The Compose API additionally uses a read-only root filesystem, `/tmp` tmpfs, and `no-new-privileges`.

Set the required Compose substitutions and start only local PostgreSQL and Redis:

```bash
export DATABASE_URI="postgres://postgres:postgres@postgres:5432/goshop_test?sslmode=disable"
export AUTH_SECRET="$(openssl rand -hex 32)"
docker compose up -d
```

Start migrations and the API profile with a secret supplied by the environment:

```bash
DATABASE_URI="postgres://postgres:postgres@postgres:5432/goshop_test?sslmode=disable" \
AUTH_SECRET="$(openssl rand -hex 32)" \
docker compose --profile application up -d --build
```

The Compose defaults are for isolated local development. Production should use managed services or independently hardened stateful services and must replace all local database credentials.

## Outbox operations

Order, lines, inventory deduction, and `order.created` outbox insertion commit in one PostgreSQL transaction. The publisher uses claim, commit, external publish, and short finalize transactions. Stale `processing` rows become claimable after `outbox_processing_timeout_seconds`.

Monitor:

```sql
SELECT status, count(*) FROM outbox_events GROUP BY status;
SELECT count(*) FROM outbox_events
WHERE status = 'processing'
  AND locked_at < now() - interval '15 minutes';
```

```bash
redis-cli XINFO GROUPS stream:orders
redis-cli XPENDING stream:orders order-events
redis-cli XLEN stream:orders:dead_letter
```

The built-in consumer handler records metadata only; payment, email, fulfillment, and analytics side effects are outside this release.

## Security and observability

- terminate TLS before the API and keep PostgreSQL/Redis on private networks
- rotate JWT, database, and Redis credentials
- enforce and monitor HTTP/gRPC login and registration throttling at the ingress
- restrict `/metrics` and Swagger to trusted networks in production
- keep logs free of passwords, JWTs, idempotency keys, request payloads, Redis credentials, and DSNs
- keep Prometheus labels bounded to normalized route/status/result categories
- understand that refresh tokens are not per-session or one-time-use in this release
- review Govulncheck, Gitleaks, and Trivy output before promotion

Staticcheck covers all packages, including generated protobuf code. The checked-in protobuf files are generated with `protoc-gen-go` v1.36.11 and `protoc-gen-go-grpc` v1.6.2 so the release gate does not need a generated-code exclusion.

## Rollout

1. Confirm the release branch and image digest were built from a reviewed commit.
2. Confirm CI, race, vulnerability, secret, workflow, migration, and image gates are green.
3. Back up PostgreSQL and record the current migration version.
4. Apply migrations with the pinned migration job.
5. Deploy one API replica and verify `/livez` and `/readyz`.
6. Exercise login, authorization, idempotent order placement, and limited stock.
7. Scale replicas within the connection budget.
8. Watch 5xx, p95/p99, pool usage, outbox failures, pending entries, and DLQ growth.

## Rollback

1. Stop further rollout and route traffic to the previously accepted image.
2. Keep the forward-compatible schema whenever possible.
3. Run `migrate down 1` only after reviewing data loss and old-image compatibility.
4. Restore PostgreSQL from backup if a destructive migration has already changed retained data.
5. Recheck `/livez`, `/readyz`, order placement, inventory, and outbox processing.
6. Document the failed revision and leave the release tag unchanged until a corrected build passes all gates.

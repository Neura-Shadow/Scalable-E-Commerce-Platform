# Scalable E-Commerce Platform

Production-minded Go backend for an e-commerce ordering system. The project demonstrates clean module boundaries, transaction-safe order placement, atomic inventory deduction, Redis-backed request protection, JWT authentication, PostgreSQL persistence, and CI-ready integration tests.

## Architecture

The codebase is organized by feature under `internal/`:

- `internal/user`: registration, login, JWT refresh, profile, password changes.
- `internal/product`: product catalog APIs with Redis caching.
- `internal/inventory`: stock read/write APIs and atomic stock mutation.
- `internal/order`: order placement, ownership checks, cancellation, and order queries.
- `internal/outbox`: durable database-backed event records with Redis Streams publisher and consumer foundations.
- `internal/server`: HTTP and gRPC composition roots.

The order module follows a clean dependency direction:

```text
HTTP handler -> order service -> repository/inventory/product ports -> GORM/Redis adapters
```

`internal/server/http` wires dependencies. `internal/order/repository/unit_of_work.go` creates transaction-scoped dependencies for order use cases.

## Implemented Features

- Gin REST API and gRPC service skeletons.
- PostgreSQL persistence with GORM.
- Versioned Redis product-list caching and exact product-detail cache keys.
- JWT authentication with access and refresh tokens.
- Admin-only product and inventory write routes.
- Customer order ownership checks.
- Transaction-safe order placement.
- Atomic inventory deduction to prevent overselling.
- Durable idempotent `POST /orders` with PostgreSQL uniqueness, request fingerprints, Redis acceleration, and `Idempotency-Key`.
- Redis-backed order-placement rate limiting with atomic increment-and-expiration.
- Explicit versioned PostgreSQL migrations with production `AutoMigrate` disabled.
- Separate `/livez` and dependency-aware `/readyz` health endpoints.
- Validated, configurable PostgreSQL connection-pool settings.
- Transactional outbox foundation for `order.created` events.
- Optional log or Redis Streams outbox publisher worker with retry and dead-letter bookkeeping.
- Optional Redis Streams consumer group foundation with idempotent event processing, bounded failure tracking, and poison-message dead-letter handling.
- Coordinated HTTP/gRPC/background worker lifecycle with root-context shutdown.
- HTTP server hardening with explicit timeouts, max header size, body size limits, trusted proxy lockdown, and graceful shutdown.
- Swagger API documentation.
- Docker Compose for PostgreSQL and Redis.
- Unit and HTTP integration tests, including concurrent limited-stock ordering.

## Reliability Highlights

### Transaction-Safe Ordering

Order placement runs product loading, stock deduction, order creation, order-line creation, and `order.created` outbox event creation inside one Unit of Work transaction. If any step fails, the whole use case rolls back.

### Atomic Inventory Deduction

Stock consumption uses a conditional update:

```sql
UPDATE inventories
SET quantity = quantity - ?
WHERE product_id = ?
AND quantity >= ?;
```

The repository checks affected rows to detect insufficient stock. This avoids unsafe read-check-write behavior under concurrent requests.

### Idempotent Order Creation

Clients can send:

```text
Idempotency-Key: checkout-attempt-123
```

Keys are scoped by authenticated user ID. A SHA-256 key digest and normalized request fingerprint are committed with the order under a PostgreSQL unique index; Redis provides the in-flight reservation and fast replay cache. Duplicate successful requests return the original order even after the Redis record expires or cannot be completed, while reusing a key with a different request returns `409`.

Product prices are limited to two decimal places. Order line totals and order totals are calculated in integer minor units before conversion to the API and PostgreSQL decimal representation.

### Request Protection

`POST /orders` is protected by a Redis-backed per-user rate limit. Defaults are documented in `pkg/config/config.sample.yaml` and `docs/order-production-readiness.md`.

The counter uses one Redis Lua operation to increment and set the first TTL. It also repairs a legacy counter that has no TTL without extending an existing positive TTL window. Correctness no longer depends on separate `INCR` and `EXPIRE` commands. Checkout remains fail-open if Redis is temporarily unavailable.

### Product Cache Safety

Product details use `cache:product:{productID}`. Product lists use a normalized query hash under `cache:products:v{version}:{hash}`. Creates and updates increment `cache:products:version`; updates also delete the exact detail key. Old list entries expire naturally, and production request paths never use Redis `KEYS`.

### Transactional Outbox

Successful order placement creates one pending `order.created` row in `outbox_events` with this payload shape:

```json
{
  "order_id": "order-id",
  "user_id": "user-id",
  "total_price": 125.5,
  "status": "new",
  "lines": [
    {
      "product_id": "product-id",
      "quantity": 1,
      "price": 125.5
    }
  ]
}
```

The current implementation stores durable outbox rows and supports publish bookkeeping, retries, `processing` claims, and `dead_letter` status. It also provides a controlled `RunOnce` publisher worker and optional background startup controlled by `outbox_publisher_enabled`, which defaults to `false`.

The publisher uses a claim/publish/finalize flow: it claims ready rows as `processing` inside a short `FOR UPDATE SKIP LOCKED` transaction, commits that claim, publishes to the configured publisher outside the database transaction, and finalizes each row in short follow-up transactions. This keeps PostgreSQL row locks out of Redis Streams `XADD` and other external publish I/O. Stuck `processing` rows older than `outbox_processing_timeout_seconds` can be claimed again by a later worker run.

The default publisher type is `log`, which records event metadata only. Redis Streams can be enabled when Redis should receive durable stream entries:

```yaml
outbox_publisher_enabled: true
outbox_publisher_type: redis_stream
outbox_redis_stream_name: stream:orders
outbox_processing_timeout_seconds: 900
```

The Redis Streams publisher writes `event_id`, `aggregate_type`, `aggregate_id`, `event_type`, `payload`, and `created_at` with `XADD`. Real downstream business side-effect handlers are not implemented yet; see `docs/order-outbox-pattern.md` for the consumer group foundation and future handler design.

The Redis Streams consumer foundation is also disabled by default. It can create/read from consumer group `order-events`, skip duplicate `event_id` values with Redis processed-event keys, acknowledge successful messages with `XACK`, and claim stale pending messages with `XAUTOCLAIM`. Repeated handler failures are tracked with `consumer:failures:{stream}:{group}:{eventID}` keys, default to 5 delivery attempts with a 24 hour failure TTL, and move poison messages to `stream:orders:dead_letter` before acknowledging the original stream entry. Invalid messages are dead-lettered instead of being retried forever. Duplicate processed events are acknowledged without incrementing failure counters or writing to the dead-letter stream. The included handler only logs metadata and performs no payment, email, fulfillment, or analytics side effects yet.

```yaml
outbox_consumer_enabled: true
outbox_consumer_group: order-events
outbox_consumer_name: local-consumer-1
outbox_consumer_batch_size: 10
outbox_consumer_block_seconds: 5
outbox_consumer_processed_ttl_seconds: 86400
outbox_consumer_claim_min_idle_seconds: 60
outbox_consumer_claim_batch_size: 10
outbox_consumer_max_delivery_attempts: 5
outbox_consumer_failure_ttl_seconds: 86400
outbox_dead_letter_stream_name: stream:orders:dead_letter
```

The consumer writes dead-letter entries with the original stream/group/message ID, event metadata, JSON payload, failure count, error type, and `dead_lettered_at`. It calls `XACK` only after the dead-letter `XADD` succeeds. Useful operational checks:

```bash
redis-cli XLEN stream:orders:dead_letter
redis-cli XRANGE stream:orders:dead_letter - +
redis-cli XPENDING stream:orders order-events
```

Monitor duplicate skip counts from consumer batch logs and alert on sustained dead-letter stream growth.

### Prometheus Metrics

The API exposes a lightweight Prometheus-style endpoint at `/metrics` by default in development. Production disables it unless explicitly enabled:

```yaml
metrics_enabled: true
metrics_path: /metrics
```

Recommended scrape config:

```yaml
scrape_configs:
  - job_name: goshop-api
    metrics_path: /metrics
    static_configs:
      - targets: ["api:8888"]
```

Important metric families include HTTP request totals/durations, order placement outcomes, idempotency duplicates, rate-limited orders, insufficient stock conflicts, outbox claims, publish attempts/success/failure, finalize failures, consumer reads/acks/failures, duplicate skips, stale claims, and dead-letter writes. Labels are intentionally bounded to `method`, `path`, `status`, `event_type`, `result`, and `reason`; do not add user IDs, order IDs, event IDs, idempotency keys, or raw Redis keys as labels.

## Permission Model

- Public users can list/read products and inventory.
- Authenticated customers can place orders, list their own orders, read their own orders, and cancel their own cancellable orders.
- Admin users can create/update products and set/adjust inventory.
- Customers cannot mutate products or inventory.
- Users cannot read or cancel another user's order.

## Local Setup

Requirements:

- Go 1.26.5+
- Docker Desktop or Docker Engine
- Docker Compose

Set the Compose substitutions and start infrastructure. The secret is still required for Compose interpolation even when the application profile is not selected:

```bash
export DATABASE_URI="postgres://postgres:postgres@postgres:5432/goshop_test?sslmode=disable"
export AUTH_SECRET="$(openssl rand -hex 32)"
docker compose -f docker-compose.yml up -d
```

Install the pinned migration CLI and create the schema:

```bash
go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3
migrate -path migrations -database "postgres://postgres:postgres@localhost:5432/goshop_test?sslmode=disable" up
```

Create local config:

```bash
cp pkg/config/config.sample.yaml pkg/config/config.yaml
```

Example local config:

```yaml
environment: development
http_port: 8888
grpc_port: 8889
auth_secret: local-dev-secret
database_uri: postgres://postgres:postgres@localhost:5432/goshop_test
redis_uri: localhost:6379
redis_password:
redis_db: 0
database_auto_migrate: false
```

Run the API:

```bash
go run cmd/api/main.go
```

Health checks:

```bash
curl http://localhost:8888/livez
curl http://localhost:8888/readyz
```

`/health` remains an alias of `/livez` for compatibility. `/readyz` checks PostgreSQL, Redis, production configuration, all eight application tables, and a clean migration version compatible with the application's minimum version 5 without returning connection strings or raw errors.

Run the complete container stack with an environment-supplied secret:

```bash
DATABASE_URI="postgres://postgres:postgres@postgres:5432/goshop_test?sslmode=disable" \
AUTH_SECRET="$(openssl rand -hex 32)" \
docker compose --profile application up -d --build
```

Prometheus metrics, when `metrics_enabled=true`:

```bash
curl http://localhost:8888/metrics
```

Swagger UI, when `swagger_enabled=true`:

```text
http://localhost:8888/swagger/index.html
```

## Testing

Run the full test suite:

```bash
go test ./... -count=1 -timeout 240s
```

Run the concurrent ordering regression repeatedly:

```bash
go test ./test/http -run TestOrderAPI_ConcurrentOrdersNeverOversell -count=5 -timeout 180s
```

Run vet checks:

```bash
go vet ./...
```

Release CI also runs the race detector, Staticcheck, Govulncheck, Actionlint, Gitleaks, Trivy filesystem/image scans, a migration-up gate, and a Docker build.

## API Examples

Register:

```bash
curl -X POST http://localhost:8888/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"customer@example.com","password":"test123456"}'
```

Login:

```bash
curl -X POST http://localhost:8888/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"customer@example.com","password":"test123456"}'
```

Place an idempotent order:

```bash
curl -X POST http://localhost:8888/api/v1/orders \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: checkout-123" \
  -d '{"lines":[{"product_id":"<product_id>","quantity":1}]}'
```

Set inventory as an admin:

```bash
curl -X PUT http://localhost:8888/api/v1/inventory/<product_id> \
  -H "Authorization: Bearer <admin_access_token>" \
  -H "Content-Type: application/json" \
  -d '{"quantity":25}'
```

## Documentation

- `docs/order-transaction-safety.md`: transaction boundary and overselling prevention.
- `docs/order-production-readiness.md`: idempotency, rate limiting, HTTP hardening, and Prometheus observability.
- `docs/load-testing.md`: load, concurrency, and optional outbox publisher testing guidance.
- `docs/benchmark-report-v1.md`: measured local smoke results and explicit measurement gaps.
- `docs/production-deployment.md`: production deployment checklist and operational notes.
- `docs/order-outbox-pattern.md`: transactional outbox foundation, Redis Streams publishing, and consumer group foundation.
- `docs/release-checklist-v1.md`: v1.0.0 release and rollback gate.
- `migrations/`: authoritative versioned PostgreSQL schema migrations.

## Production-Readiness Notes

- Keep `pkg/config/config.yaml` local and out of git.
- Use environment variables or a secret manager for production secrets.
- Run PostgreSQL and Redis as managed services or hardened containers.
- Put the API behind TLS at the edge.
- Expose `/metrics` only to trusted Prometheus scrapers or behind a restricted reverse proxy in production.
- Let the root process context handle `SIGINT`/`SIGTERM`; HTTP, gRPC, publisher, and consumer loops all exit through the coordinated lifecycle path.
- Tune order rate limits to match real checkout traffic.
- Keep `database_auto_migrate=false` in production and apply reviewed migrations before deploying the API.
- Enforce account/source-aware throttling for HTTP and gRPC login/registration at the ingress; application-level auth throttling is not included in this release.
- Treat refresh tokens as 30-day bearer credentials. Password change revokes all existing refresh tokens through the user token version, but per-session rotation/logout is not implemented.
- Use the Redis Streams outbox publisher and consumer foundation when order events must leave the database, and add real business side-effect handlers before treating downstream processing as complete.

This is a production-minded, single-region e-commerce backend. It is not a multi-region active-active system and makes no national-scale or railway-capacity claim. Redis Cluster, cross-region caching, database sharding, and multi-region failover are not implemented.

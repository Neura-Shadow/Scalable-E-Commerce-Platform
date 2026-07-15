# Order Production Readiness

## Idempotent order creation

`POST /api/v1/orders` supports the `Idempotency-Key` header.

The key is scoped to the authenticated user:

```text
idempotency:orders:{userID}:{sha256(idempotencyKey)}
```

Short-lived coordination metadata is stored in Redis:

- `status`
- `order_id`
- request fingerprint

The request body and other sensitive request data are not stored.

The same SHA-256 key digest and a canonical request fingerprint are stored with the order in PostgreSQL. A unique `(user_id, idempotency_key)` index is the durable correctness boundary; Redis is an in-flight reservation and replay accelerator. A cache completion failure or TTL expiry cannot permit a second order. Reusing a key with a different product/quantity request returns `409`.

Behavior:

- Missing `Idempotency-Key`: the request is processed normally and repeat requests can create separate orders.
- First request for a user/key pair: Redis `SETNX` reserves the key with a TTL, then the order use case runs.
- Successful first request: the Redis record is updated with the created `order_id`.
- Duplicate successful request: the handler loads and returns the original order for the same user.
- Duplicate in-flight request: the handler returns a conflict response instead of creating another order.
- Failed order placement: the temporary idempotency reservation is removed so the client can retry.

## Request protection

Order placement is protected by a Redis-backed user rate limit:

```text
rate-limit:orders:{userID}
```

The default limit is 120 order-placement attempts per 60 seconds per authenticated user. Redis failures fail open to avoid blocking checkout because of a transient cache outage.

The counter is one Redis Lua operation: `INCR`, inspect `PTTL`, set `PEXPIRE` for a new counter or a legacy counter with no TTL, and return the count. Existing positive TTL windows are not restarted. A process or network failure cannot leave a permanent key in the gap between separate increment and expiration commands. The same atomic helper is used for Redis Streams consumer failure counters.

JSON request bodies are capped by a global Gin middleware. The default limit is 1 MiB.

## HTTP server hardening

The HTTP server uses explicit production-safe defaults:

- `ReadTimeout`: 10 seconds
- `WriteTimeout`: 30 seconds
- `IdleTimeout`: 60 seconds
- `ReadHeaderTimeout`: 5 seconds
- `MaxHeaderBytes`: 1 MiB

The server also uses graceful shutdown for `SIGINT` and `SIGTERM`, with a 10 second shutdown timeout.

Gin trusted proxies are disabled with `SetTrustedProxies(nil)`.

## Health and schema readiness

- `/livez` and its `/health` compatibility alias do not call PostgreSQL or Redis.
- `/readyz` checks validated production configuration, PostgreSQL connectivity, all eight application tables, a clean migration version compatible with minimum version 5, and Redis connectivity with short context deadlines.
- readiness returns fixed component states and never returns DSNs, credentials, hostnames, or raw internal errors.
- outbox backlog, consumer pending entries, and DLQ entries remain metrics/alert concerns rather than readiness failures.

## Product cache correctness

Product details use exact `cache:product:{productID}` keys. Product lists use a stable SHA-256 hash of normalized request fields under `cache:products:v{version}:{hash}`. Create/update increments `cache:products:version`, and update deletes the exact detail key. Old list entries expire naturally. Redis failures remain fail-open, and production request paths do not call Redis `KEYS`; the generic maintenance helper uses bounded cursor-based `SCAN`.

## Database pool

The configured pool applies max-open, max-idle, max-lifetime, and max-idle-time settings. Values must be positive after defaults are applied, and max-idle cannot exceed max-open. Size the pool from PostgreSQL connection capacity divided across API replicas after reserving migration, worker, monitoring, backup, and administrator connections. A larger pool is not a throughput guarantee.

## Monetary precision

Product writes accept at most two decimal places. Order line and total calculations use integer minor units with overflow checks, and migration 5 installs matching PostgreSQL scale constraints. This avoids binary floating-point accumulation in persisted orders and outbox payloads while preserving numeric JSON responses.

## Configuration

The defaults can be overridden through config/env fields:

```text
http_read_timeout_seconds
http_write_timeout_seconds
http_idle_timeout_seconds
http_read_header_timeout_seconds
http_max_header_bytes
max_request_body_bytes
metrics_enabled
metrics_path
swagger_enabled
order_idempotency_ttl_seconds
order_rate_limit_limit
order_rate_limit_window_seconds
database_auto_migrate
database_max_open_conns
database_max_idle_conns
database_conn_max_lifetime_seconds
database_conn_max_idle_time_seconds
grpc_reflection_enabled
```

Production validation also requires `database_auto_migrate=false`, a non-placeholder JWT secret, valid ports, and configured PostgreSQL/Redis endpoints. gRPC protected methods require access tokens, while the refresh RPC requires a refresh token. Refresh tokens carry the user's token version, and changing a password increments the stored version so all previously issued refresh tokens are rejected. Malformed token identity or token-version claims return `Unauthenticated` rather than panicking the server.

## Observability

Order placement logs concise structured-style events for high-signal cases:

- idempotency duplicate
- idempotency cache failures
- rate-limited requests
- rate-limit cache failures
- order placement failure category
- order placement latency on failure and duplicate paths

These logs avoid storing full request payloads or idempotency key values.

Development exposes Prometheus metrics at `/metrics` by default. Production keeps both metrics and Swagger disabled unless explicitly enabled for a restricted internal network:

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

Order-facing metric families:

- `http_requests_total`
- `http_request_duration_seconds`
- `order_place_total`
- `order_place_failed_total`
- `order_place_duration_seconds`
- `order_idempotency_duplicate_total`
- `order_rate_limited_total`
- `inventory_insufficient_stock_total`
- `outbox_events_created_total`

Recommended dashboard panels:

- order placement rate
- order error rate by `reason`
- p95/p99 order latency from `order_place_duration_seconds`
- idempotency duplicate count split by replay vs in-flight conflict
- rate-limited order requests
- insufficient stock conflicts

Alert ideas:

- rising insufficient stock conflicts
- rate-limited order requests above expected checkout traffic
- high p99 order latency
- sudden increase in internal order placement failures

Label cardinality rules:

- allowed labels are `method`, `path`, `status`, `event_type`, `result`, and `reason`
- use normalized Gin route paths such as `/api/v1/orders/:id`
- never label by `user_id`, `order_id`, `event_id`, idempotency key, raw Redis key, JWT subject, or request payload fields

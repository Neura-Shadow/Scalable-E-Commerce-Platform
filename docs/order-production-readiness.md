# Order Production Readiness

## Idempotent order creation

`POST /api/v1/orders` supports the `Idempotency-Key` header.

The key is scoped to the authenticated user:

```text
idempotency:orders:{userID}:{sha256(idempotencyKey)}
```

Only short-lived metadata is stored in Redis:

- `status`
- `order_id`

The request body and other sensitive request data are not stored.

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
order_idempotency_ttl_seconds
order_rate_limit_limit
order_rate_limit_window_seconds
```

## Observability

Order placement logs concise structured-style events for high-signal cases:

- idempotency duplicate
- idempotency cache failures
- rate-limited requests
- rate-limit cache failures
- order placement failure category
- order placement latency on failure and duplicate paths

These logs avoid storing full request payloads or idempotency key values.

The application also exposes Prometheus metrics at `/metrics` by default. Disable or move it with:

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

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

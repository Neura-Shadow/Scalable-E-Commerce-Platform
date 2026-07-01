# Production Deployment

This project is an API backend. A production deployment should run the Go service, PostgreSQL, and Redis as separate managed services or hardened containers.

## Required services

- Go API container or VM process
- PostgreSQL
- Redis
- TLS terminator or reverse proxy
- Log aggregation
- Metrics and alerting

## Configuration

Configure the service with environment variables or a secret manager. Do not commit `pkg/config/config.yaml` with real secrets.

Required values:

```text
environment
http_port
grpc_port
auth_secret
database_uri
redis_uri
redis_password
redis_db
```

Recommended hardening values:

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
outbox_publisher_enabled
outbox_publish_batch_size
outbox_publish_max_attempts
outbox_publish_retry_base_seconds
outbox_publish_interval_seconds
```

## Build

Example container build:

```bash
docker build -t scalable-ecommerce-platform:latest .
```

## Run

Example local container run:

```bash
docker run --rm -p 8888:8888 \
  -e environment=production \
  -e http_port=8888 \
  -e grpc_port=8889 \
  -e auth_secret="${AUTH_SECRET}" \
  -e database_uri="${DATABASE_URI}" \
  -e redis_uri="${REDIS_URI}" \
  scalable-ecommerce-platform:latest
```

## Health checks

The HTTP service exposes:

```text
GET /health
```

Use this endpoint for readiness/liveness checks at the platform layer.

## Database operations

The current app uses GORM auto-migration during startup. The auto-migrated models include users, products, inventory, orders, order lines, and `outbox_events`.

For a long-lived production database, replace startup auto-migration with explicit, reviewed migrations before scaling deployments. The outbox table should be managed by the same migration process as the order tables so event durability is not dependent on runtime schema changes.

Use `docs/migrations/outbox_events.sql` as the production migration reference for the outbox table and indexes.

## Outbox operations

Order placement writes one `order.created` row to `outbox_events` in the same transaction as inventory deduction, order creation, and order-line creation.

Current behavior:

- no external broker publisher is wired yet
- optional background publisher startup is controlled by `outbox_publisher_enabled`
- default startup leaves the publisher disabled
- pending batch fetches use `FOR UPDATE SKIP LOCKED`
- the no-op/log publisher logs event metadata only and does not log payloads

Operational expectations for the future publisher:

- process only `pending` rows whose `next_attempt_at` is due
- mark successful publishes as `published`
- increment `attempts` and reschedule transient failures
- move exhausted events to `dead_letter`
- alert on dead-letter growth

Recommended outbox metrics:

- `outbox_pending_count`
- `outbox_published_count`
- `outbox_publish_failed_count`
- `outbox_dead_letter_count`
- `outbox_publish_latency_ms`
- `outbox_oldest_pending_age_seconds`

## Security checklist

- Keep `auth_secret` in a secret manager.
- Use TLS at the edge.
- Keep Redis private to the application network.
- Keep PostgreSQL private to the application network.
- Rotate credentials regularly.
- Set resource limits for containers.
- Keep Go and base images patched.
- Run `go test ./...` in CI before deploying.

## Rollout checklist

1. Build the image from a reviewed commit.
2. Run CI tests.
3. Apply database migrations.
4. Deploy the API with new configuration.
5. Verify `/health`.
6. Run a smoke test for login and order placement.
7. Verify that successful order placement creates one pending `order.created` outbox row.
8. Monitor order failure logs, rate-limited counts, outbox dead-letter counts, and latency.
9. Roll back if error rate, outbox failures, or latency exceed the deployment threshold.

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
outbox_publisher_type
outbox_redis_stream_name
outbox_publish_batch_size
outbox_publish_max_attempts
outbox_publish_retry_base_seconds
outbox_publish_interval_seconds
outbox_consumer_enabled
outbox_consumer_group
outbox_consumer_name
outbox_consumer_batch_size
outbox_consumer_block_seconds
outbox_consumer_processed_ttl_seconds
outbox_consumer_claim_min_idle_seconds
outbox_consumer_claim_batch_size
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

- supported publisher types are `log` and `redis_stream`
- optional background publisher startup is controlled by `outbox_publisher_enabled`
- default startup leaves the publisher disabled and uses `outbox_publisher_type=log`
- Redis Streams consumer startup is separately disabled by `outbox_consumer_enabled=false`
- pending batch fetches use `FOR UPDATE SKIP LOCKED`
- the no-op/log publisher logs event metadata only and does not log payloads
- the Redis Streams publisher writes event metadata and JSON payloads to `outbox_redis_stream_name`
- the Redis Streams consumer foundation reads with `XREADGROUP`, uses processed-event keys by `event_id`, acknowledges success with `XACK`, and claims stale pending entries with `XAUTOCLAIM`

Enable Redis Streams publishing with:

```yaml
outbox_publisher_enabled: true
outbox_publisher_type: redis_stream
outbox_redis_stream_name: stream:orders
```

Enable Redis Streams consuming with:

```yaml
outbox_consumer_enabled: true
outbox_consumer_group: order-events
outbox_consumer_name: local-consumer-1
outbox_consumer_batch_size: 10
outbox_consumer_block_seconds: 5
outbox_consumer_processed_ttl_seconds: 86400
outbox_consumer_claim_min_idle_seconds: 60
outbox_consumer_claim_batch_size: 10
```

Operational expectations for the publisher:

- process only `pending` rows whose `next_attempt_at` is due
- mark successful publishes as `published`
- increment `attempts` and reschedule transient failures
- move exhausted events to `dead_letter`
- alert on dead-letter growth

The repository includes a minimal Redis Streams consumer foundation, but the built-in handler only logs metadata and performs no real business side effects. Future handlers should process messages idempotently by `event_id`, inspect pending entries with `XPENDING`, claim stale pending entries with `XAUTOCLAIM`, acknowledge only after successful side effects, and move poison messages to `stream:orders:dead_letter` after bounded retries.

Recommended outbox metrics:

- `outbox_pending_count`
- `outbox_published_count`
- `outbox_publish_failed_count`
- `outbox_dead_letter_count`
- `outbox_publish_latency_ms`
- `outbox_oldest_pending_age_seconds`
- Redis stream length and consumer group pending count
- stale claimed message count
- duplicate skipped count

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
8. If Redis Streams publishing is enabled, verify that `stream:orders` receives an entry and the outbox row is marked `published`.
9. If Redis Streams consuming is enabled, verify that group `order-events` exists and messages are acknowledged after the log/no-op handler succeeds.
10. Monitor order failure logs, rate-limited counts, outbox dead-letter counts, stream length, pending entries, stale claims, duplicate skips, and latency.
11. Roll back if error rate, outbox failures, consumer failures, or latency exceed the deployment threshold.

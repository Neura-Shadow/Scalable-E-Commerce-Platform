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
metrics_enabled
metrics_path
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
outbox_consumer_max_delivery_attempts
outbox_consumer_failure_ttl_seconds
outbox_dead_letter_stream_name
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

Use this endpoint as a lightweight liveness check only. It intentionally does not prove PostgreSQL, Redis, publisher, or consumer readiness. For production readiness, combine `/health` with a platform-level check that verifies database connectivity, Redis connectivity, migration status, and the Prometheus scrape path.

The metrics endpoint is enabled by default:

```text
GET /metrics
```

In production, expose it only to trusted Prometheus scrapers or restrict it behind an internal reverse proxy. Disable it in the app if the platform supplies metrics another way:

```yaml
metrics_enabled: false
metrics_path: /metrics
```

Recommended Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: goshop-api
    metrics_path: /metrics
    static_configs:
      - targets: ["api:8888"]
```

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
- the Redis Streams consumer foundation reads with `XREADGROUP`, uses processed-event keys by `event_id`, acknowledges success with `XACK`, claims stale pending entries with `XAUTOCLAIM`, tracks handler failures with `consumer:failures:{stream}:{group}:{eventID}`, and writes poison messages to `outbox_dead_letter_stream_name`

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
outbox_consumer_max_delivery_attempts: 5
outbox_consumer_failure_ttl_seconds: 86400
outbox_dead_letter_stream_name: stream:orders:dead_letter
```

Operational expectations for the publisher:

- process only `pending` rows whose `next_attempt_at` is due
- mark successful publishes as `published`
- increment `attempts` and reschedule transient failures
- move exhausted events to `dead_letter`
- alert on dead-letter growth

The repository includes a Redis Streams consumer foundation, but the built-in handler only logs metadata and performs no real business side effects. Future handlers should process messages idempotently by `event_id`, inspect pending entries with `XPENDING`, claim stale pending entries with `XAUTOCLAIM`, and acknowledge only after successful side effects. If handling fails repeatedly, the current consumer moves the message to `stream:orders:dead_letter` after bounded retries and acknowledges the original only after the dead-letter write succeeds. Failure counters expire after `outbox_consumer_failure_ttl_seconds`. Invalid messages are dead-lettered instead of retried forever. Duplicate processed events are acknowledged without counting as failures.

Useful Redis Streams checks:

```bash
redis-cli XLEN stream:orders
redis-cli XRANGE stream:orders - + COUNT 10
redis-cli XINFO STREAM stream:orders
redis-cli XINFO GROUPS stream:orders
redis-cli XINFO CONSUMERS stream:orders order-events
redis-cli XLEN stream:orders:dead_letter
redis-cli XRANGE stream:orders:dead_letter - + COUNT 10
redis-cli XPENDING stream:orders order-events
```

Application Prometheus metrics:

- `outbox_publish_attempt_total`
- `outbox_publish_success_total`
- `outbox_publish_failure_total`
- `outbox_publish_duration_seconds`
- `outbox_consumer_read_total`
- `outbox_consumer_ack_total`
- `outbox_consumer_failure_total`
- `outbox_consumer_duplicate_skipped_total`
- `outbox_consumer_stale_claim_total`
- `outbox_consumer_dead_letter_total`

Backlog/current-state signals such as DB pending row count, oldest pending row age, Redis stream length, consumer group pending count, and DLQ length should come from SQL/Redis exporter queries or operational scripts until the application owns a dedicated sampler.

Example PromQL:

```promql
histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le, path))
histogram_quantile(0.99, sum(rate(order_place_duration_seconds_bucket[5m])) by (le))
sum(rate(order_place_failed_total[5m])) by (reason)
sum(rate(outbox_publish_failure_total[5m])) by (event_type, reason)
increase(outbox_consumer_dead_letter_total[15m])
```

Recommended dashboard panels:

- HTTP request rate and status mix
- p95/p99 HTTP request latency
- order placement rate
- order error rate by `reason`
- p95/p99 order latency
- idempotency duplicate count
- rate-limited order requests
- outbox publish success/failure
- Redis Streams consumer pending and stale-claim behavior
- DLQ growth

Alert ideas:

- sustained DLQ growth
- rising insufficient stock conflicts
- outbox publish failure spike
- consumer failure spike
- high p99 latency
- Redis stream pending growth

Label cardinality rules:

- allowed labels: `method`, `path`, `status`, `event_type`, `result`, and `reason`
- use normalized route patterns, not raw URL paths
- never label by user ID, order ID, event ID, idempotency key, raw Redis key, JWT claims, or payload contents

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
6. Verify `/metrics` from the Prometheus network path if metrics are enabled.
7. Run a smoke test for login and order placement.
8. Verify that successful order placement creates one pending `order.created` outbox row.
9. If Redis Streams publishing is enabled, verify that `stream:orders` receives an entry and the outbox row is marked `published`.
10. If Redis Streams consuming is enabled, verify that group `order-events` exists and messages are acknowledged after the log/no-op handler succeeds.
11. Verify `XLEN stream:orders:dead_letter`, `XRANGE stream:orders:dead_letter - +`, and `XPENDING stream:orders order-events` during rollout.
12. Monitor order failure logs, rate-limited counts, outbox dead-letter counts, stream length, pending entries, stale claims, duplicate skips, metrics scrape health, and latency.
13. Roll back if error rate, outbox failures, consumer failures, dead-letter growth, metrics scrape failures, or latency exceed the deployment threshold.

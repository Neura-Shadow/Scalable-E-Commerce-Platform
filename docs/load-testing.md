# Load Testing

This project can be load-tested with any HTTP load generator. The examples below use `hey` because it is small and easy to install.

## Setup

Start dependencies and the API:

```bash
docker compose -f docker-compose.yml up -d
go run cmd/api/main.go
```

The outbox background publisher is disabled by default. To include Redis Streams publishing in a local load run, set:

```yaml
outbox_publisher_enabled: true
outbox_publisher_type: redis_stream
outbox_redis_stream_name: stream:orders
```

To include the consumer group foundation in the same local run, also set:

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

Metrics are available by default during load runs:

```yaml
metrics_enabled: true
metrics_path: /metrics
```

Create or seed:

- one customer account
- one admin account
- one product
- inventory for the product
- a customer JWT access token

## Normal order placement

```bash
hey -n 200 -c 20 \
  -m POST \
  -H "Authorization: Bearer ${CUSTOMER_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"lines":[{"product_id":"'"${PRODUCT_ID}"'","quantity":1}]}' \
  http://localhost:8888/api/v1/orders
```

Track:

- success count
- conflict count
- p95 latency
- p99 latency
- final stock

## Concurrent limited-stock ordering

Set inventory to a small number, for example `10`, then run more concurrent requests than available stock.

Expected result:

- exactly `10` successful orders
- remaining requests fail with conflict or another safe non-2xx response
- final stock is `0`
- final stock is never negative

The automated regression is:

```bash
go test ./test/http -run TestOrderAPI_ConcurrentOrdersNeverOversell -count=5 -timeout 180s
```

## Idempotent retries

Send the same request repeatedly with the same user token and `Idempotency-Key`:

```bash
hey -n 20 -c 5 \
  -m POST \
  -H "Authorization: Bearer ${CUSTOMER_TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: checkout-load-test-1" \
  -d '{"lines":[{"product_id":"'"${PRODUCT_ID}"'","quantity":1}]}' \
  http://localhost:8888/api/v1/orders
```

Expected result:

- one order is created for that user/key pair
- duplicate successful responses return the same order
- in-flight duplicates may return `409 Conflict`

## Rate limit behavior

Use a high request count against `POST /orders`.

Expected result:

- successful requests while below the per-user window limit
- `429 Too Many Requests` after the configured limit
- no duplicate orders caused by retries

## Metrics to record

At minimum, record:

- total requests
- success count
- conflict count
- rate-limited count
- p95 latency
- p99 latency
- final stock quantity
- order count for the tested user
- p95/p99 order latency from `order_place_duration_seconds`
- HTTP p95/p99 latency from `http_request_duration_seconds`
- idempotency duplicate count
- rate-limited request count
- outbox publish success/failure counts
- consumer failure and dead-letter counts

The final stock quantity must never be negative.

## Optional outbox publisher check

When Redis Streams publishing is enabled during a load run, successful orders should eventually create entries in the configured stream and mark their outbox rows as `published`. When the consumer foundation is also enabled, messages should be read with `XREADGROUP`, skipped if `processed:events:{eventID}` already exists, and acknowledged with `XACK` after the metadata-only handler succeeds. Repeated handler failures are counted with `consumer:failures:{stream}:{group}:{eventID}`, expire after `outbox_consumer_failure_ttl_seconds`, and route to `stream:orders:dead_letter` after the configured max attempts. Invalid messages are dead-lettered instead of being retried forever. Originals are acknowledged only after the dead-letter write succeeds.

Useful checks:

```bash
redis-cli XLEN stream:orders
redis-cli XRANGE stream:orders - +
redis-cli XINFO GROUPS stream:orders
redis-cli XPENDING stream:orders order-events
redis-cli XLEN stream:orders:dead_letter
redis-cli XRANGE stream:orders:dead_letter - +
curl -s http://localhost:8888/metrics
```

Track:

- stream length growth
- outbox publish failure count
- dead-letter count
- publisher batch latency
- oldest pending outbox row age
- Redis consumer group pending count
- stale messages claimed with `XAUTOCLAIM`
- duplicate event IDs skipped
- dead-letter stream growth rate
- `outbox_publish_failure_total`
- `outbox_consumer_failure_total`
- `outbox_consumer_dead_letter_total`

This repository does not yet include real downstream business side effects. Future consumer group load tests should keep group `order-events`, process messages idempotently by `event_id`, acknowledge after side effects commit, and alert on unexpected growth in `stream:orders:dead_letter`.

Dashboard panels worth watching during load tests:

- order placement rate
- order error rate
- p95/p99 order latency
- idempotency duplicate count
- rate-limited requests
- outbox publish success/failure
- consumer pending/dead-letter behavior
- DLQ growth

Keep metrics labels bounded to `method`, `path`, `status`, `event_type`, `result`, and `reason`. Do not add user IDs, order IDs, event IDs, idempotency keys, or raw Redis keys.

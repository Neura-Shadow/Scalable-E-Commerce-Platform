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

The final stock quantity must never be negative.

## Optional outbox publisher check

When Redis Streams publishing is enabled during a load run, successful orders should eventually create entries in the configured stream and mark their outbox rows as `published`.

Useful checks:

```bash
redis-cli XLEN stream:orders
redis-cli XRANGE stream:orders - +
```

Track:

- stream length growth
- outbox publish failure count
- dead-letter count
- publisher batch latency
- oldest pending outbox row age

This repository does not yet include a downstream Redis Streams consumer. A future consumer group load test should use group `order-events`, claim stale pending entries, process messages idempotently by `event_id`, acknowledge after side effects commit, and route poison messages to `stream:orders:dead_letter`.

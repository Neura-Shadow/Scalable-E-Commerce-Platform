# Load Testing

The reproducible k6 entry point is `loadtest/k6/release-smoke.js`. It targets k6 v2.0.0 and defines eight independent scenarios.

| Scenario | k6 name | Purpose |
| --- | --- | --- |
| Product list | `product-list` | Versioned list-cache and query path |
| Product detail | `product-detail` | Exact product cache key |
| Normal order | `normal-order` | Successful checkout path |
| Limited stock | `limited-stock` | Concurrent conflicts and nonnegative stock |
| Idempotency retry | `idempotency` | Same key returns the same order |
| Rate limit | `rate-limit` | Expected `429` behavior |
| Outbox publisher | `outbox-publisher` | Publisher metric/error observation |
| Redis consumer | `redis-consumer` | Consumer failure and DLQ observation |

## Required environment

```text
BASE_URL
CUSTOMER_TOKEN
ADMIN_TOKEN
PRODUCT_ID
EXPECTED_STOCK
VUS
DURATION
```

Order scenarios also accept separate `NORMAL_ORDER_PRODUCT_ID`, `LIMITED_PRODUCT_ID`, `IDEMPOTENCY_PRODUCT_ID`, and `RATE_LIMIT_PRODUCT_ID`. Use separate seeded products when scenarios run together so a normal-order scenario cannot consume the limited-stock fixture. Tokens are runtime inputs and must never be committed.

Optional gates:

```text
SCENARIOS
EXPECT_CONFLICTS
P95_MS
P99_MS
```

The default `SCENARIOS=product-list,product-detail` is a non-mutating smoke. `SCENARIOS=all` enables all eight scenarios.

## Run with Docker

Start the application profile and seed users, products, and inventory first. From the repository root:

```bash
docker run --rm \
  -v "$PWD:/work:ro" \
  -w /work \
  -e BASE_URL=http://host.docker.internal:8888 \
  -e PRODUCT_ID="$PRODUCT_ID" \
  -e CUSTOMER_TOKEN="$CUSTOMER_TOKEN" \
  -e ADMIN_TOKEN="$ADMIN_TOKEN" \
  -e VUS=1 \
  -e DURATION=5s \
  -e SCENARIOS=product-list,product-detail \
  grafana/k6:2.0.0@sha256:a33a0cfdc4d2483d6b7a3a22e726a499ff2831a671a49239104cd34a9937523c run loadtest/k6/release-smoke.js
```

Example limited-stock gate:

```bash
docker run --rm \
  -v "$PWD:/work:ro" \
  -w /work \
  -e BASE_URL=http://host.docker.internal:8888 \
  -e PRODUCT_ID="$LIMITED_PRODUCT_ID" \
  -e LIMITED_PRODUCT_ID="$LIMITED_PRODUCT_ID" \
  -e CUSTOMER_TOKEN="$CUSTOMER_TOKEN" \
  -e ADMIN_TOKEN="$ADMIN_TOKEN" \
  -e EXPECTED_STOCK=10 \
  -e EXPECT_CONFLICTS=true \
  -e VUS=20 \
  -e DURATION=10s \
  -e SCENARIOS=limited-stock \
  grafana/k6:2.0.0@sha256:a33a0cfdc4d2483d6b7a3a22e726a499ff2831a671a49239104cd34a9937523c run loadtest/k6/release-smoke.js
```

## Built-in thresholds

- zero transport failures and unexpected 5xx responses
- p95 and p99 HTTP latency below configurable smoke baselines
- successful limited-stock orders exactly equal seeded stock
- zero observed negative stock
- expected conflicts when their explicit gate is enabled
- at least one `429` whenever the `rate-limit` scenario is selected
- zero idempotency response mismatch
- zero observed outbox claim/finalize/publish failure samples
- zero consumer failure or DLQ-growth samples

Expected `409` and `429` responses count as `http_req_failed` in k6's built-in metric. Use the custom safe-status checks and counters to distinguish those intentional outcomes from unexpected failures.

## External measurements

k6 cannot directly prove every database and Redis invariant. For an accepted run, record these from isolated infrastructure before and after the scenario:

- PostgreSQL pool usage, locks, slow queries, pending/processing/dead-letter outbox rows
- Redis command latency, stream length, consumer pending count, and DLQ length
- final inventory and successful order count
- API/container CPU and memory

Useful checks:

```sql
SELECT quantity FROM inventories WHERE product_id = '<limited-product-id>';
SELECT count(*) FROM order_lines WHERE product_id = '<limited-product-id>';
SELECT status, count(*) FROM outbox_events GROUP BY status;
```

```bash
redis-cli XLEN stream:orders
redis-cli XPENDING stream:orders order-events
redis-cli XLEN stream:orders:dead_letter
curl -s http://localhost:8888/metrics
```

Record hardware, replicas, database/Redis settings, dataset size, VUs, duration, RPS/TPS, p50/p95/p99, 4xx/5xx, pool usage, Redis latency, outbox state, final stock, and the observed bottleneck in `docs/benchmark-report-v1.md`.

The short local runs in that report validate script and safety behavior only. They do not establish sustained capacity, multi-replica scaling, or national-scale performance.

# v1.0.0 Local Smoke Report

## Scope

These measurements validate the k6 scripts, container path, health endpoints, cache reads, and limited-stock safety on one developer machine. They are short smoke runs, not a sustained capacity benchmark or production sizing recommendation.

Run date: 2026-07-14, Asia/Taipei.

## Environment

| Item | Measured value |
| --- | --- |
| Host | Windows with Docker Desktop Linux backend |
| CPU | Intel Core i7-13700HX |
| Host memory | 31.7 GiB |
| Docker allocation | 24 CPUs, 15.5 GiB memory |
| Docker server | 28.5.1 |
| API replicas | 1 |
| API image | Go 1.26.5 multi-stage build, Alpine 3.22 runtime, UID/GID 10001 |
| PostgreSQL | 16.14, `max_connections=100`, `shared_buffers=128MB` |
| Redis | 7.4.7 Alpine, `maxmemory=0`, `maxmemory-policy=noeviction` |
| Dataset after smoke | 10 users, 3 products, 78 orders |
| Background paths | Redis Streams publisher and consumer enabled |

## Product browsing smoke

Configuration: `product-list,product-detail`, one VU each, 5 seconds active duration.

| Metric | Result |
| --- | --- |
| HTTP requests | 52 |
| Request rate | 10.04 requests/second |
| Median | 4.92 ms |
| p95 | 7.08 ms |
| p99 | 10.03 ms |
| Maximum | 12.69 ms |
| Checks | 52/52 passed |
| 4xx | 0 |
| 5xx | 0 |
| Transport failures | 0 |

## Limited-stock smoke

Configuration: one product seeded with 5 units, 10 VUs, 3 seconds active duration, concurrent order and inventory reads.

| Metric | Result |
| --- | --- |
| HTTP requests | 526 |
| Request rate | 102.27 requests/second |
| Median | 5.49 ms |
| p95 | 13.43 ms |
| p99 | 85.88 ms |
| Maximum | 105.30 ms |
| Checks | 526/526 passed |
| Expected 4xx | 256/526 (48.66%): 114 conflicts and 142 rate limits |
| Unexpected 5xx | 0 |
| Successful orders | 5 |
| Observed conflicts | 114 |
| Final stock | 0 |
| Negative-stock observations | 0 |
| PostgreSQL connections after run | 2 of 100, point-in-time snapshot rather than peak usage |

The stock cap deliberately limits successful order throughput, so the five successful orders are not a checkout TPS capacity measurement.

## Runtime transaction and inventory concurrency check

A fresh admin created a product and seeded 32 units through the HTTP API. Thirty-two admin `+1` adjustments and 32 customer orders were then started concurrently against the release container. All 64 requests returned `200`, and the final inventory remained exactly 32. A subsequent duplicate checkout using one idempotency key returned the same order twice, created one order row and one outbox row, and reduced inventory only once to 31. A customer attempt to create a product returned `403`.

## Final release-image correctness spot check

Run date: 2026-07-15, Asia/Taipei. Image: `sha256:d8aef209cd73f8ac542b3d2d53cdce2021b9a142aec39035c6d89475c53143af`.

The final image returned `200` from `/livez`, `/health`, and `/readyz`; readiness reported PostgreSQL, Redis, and configuration as ready. A fresh admin and customer registered and logged in successfully. The customer received `403` when creating a product, an admin received `400` for a three-decimal price, and a valid `0.10` product was created and seeded with three units.

The customer placed one order with an idempotency key. After all Redis idempotency cache records were removed, replaying the same request returned `200` with the same order ID. Reusing the key with a different quantity returned `409`. Final stock was two, with exactly one durable order row and one outbox row. The registered customer's password used bcrypt cost 10.

The non-mutating `product-list,product-detail` k6 smoke was repeated against this image with one VU per scenario for five seconds. All 52 checks passed at 10.07 requests/second; median latency was 4.09 ms, p95 was 7.57 ms, and p99 was 11.18 ms. There were no unexpected 5xx responses or transport failures.

After the worker settled, the point-in-time dataset contained 14 users, 5 products, and 80 orders. PostgreSQL had 80 published outbox rows and two active database connections. The Redis order stream had one retained entry, consumer-group pending and lag were zero, and the dead-letter stream was empty. These are correctness and operational-state observations, not capacity measurements.

## Rate-limit smoke

Configuration: a fresh customer, one product with 31 remaining units after the concurrent inventory/order runtime check, 5 VUs, and 3 seconds active duration. The scenario continued through expected stock conflicts until the per-customer order limit returned `429` responses.

| Metric | Result |
| --- | --- |
| HTTP requests | 4,059, including `/livez` and `/readyz` setup checks |
| Request rate | 578.16 requests/second |
| Median | 3.09 ms |
| p95 | 5.18 ms |
| p99 | 11.89 ms |
| Maximum | 53.46 ms |
| Checks | 4,059/4,059 passed |
| Observed rate limits | 3,937 |
| Unexpected 5xx | 0 |
| Transport failures | 0 |
| Final stock | 0 |
| Negative-stock observations | 0 |

The rate-limit run proves that the scenario cannot pass without observing a real `429`. It is intentionally a short policy smoke, not a throughput benchmark; most requests were expected `409` or `429` responses after stock and rate-limit boundaries were reached.

## Outbox and consumer state

After the release smoke runs and runtime transaction check:

- PostgreSQL outbox: 78 `published`, 0 pending, 0 processing, 0 dead-letter.
- Redis `stream:orders`: 64 retained entries.
- Redis consumer pending: 0.
- Redis dead-letter stream: 0 entries.

An adversarial one-second normal-order run with an invalid customer token exited with code 99 and crossed the global `checks` threshold. Only 2 of 7 checks passed, confirming that authentication or scenario-check failures cannot produce a successful k6 exit.

## Measurements not produced

- sustained RPS/TPS under a stable multi-minute workload
- peak PostgreSQL connection usage and wait time
- per-command Redis latency
- per-container CPU, memory, and network saturation
- multi-replica scaling behavior
- large-dataset cache and query behavior
- controlled downstream side-effect throughput

No capacity ceiling or national-scale claim can be derived from these runs. A sustained benchmark should isolate fixtures, capture database/Redis/container telemetry, repeat multiple trials, and compare against a previously accepted baseline.

## Observed bottleneck

No infrastructure saturation was identified in these short runs. The limited-stock workload became application-policy bound as intended: stock conflicts and rate limiting dominated after five successful orders. Longer instrumented tests are required to identify a hardware, database, Redis, or application bottleneck.

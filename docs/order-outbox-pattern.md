# Order Outbox Pattern

The ordering flow writes orders, order lines, inventory mutations, and an `order.created` outbox event in one database transaction. Future integrations such as payment, email, fulfillment, or analytics should publish from the durable outbox table instead of publishing directly from the order request handler.

## Why direct publishing is risky

If the service publishes to a broker inside the transaction and the database later rolls back, downstream services may receive an event for an order that does not exist.

If the service commits the transaction and then fails before publishing, the order exists but no downstream service is notified.

The transactional outbox pattern solves this by writing events to an outbox table in the same database transaction as the order change.

## Implemented scope

Implemented now:

- `internal/outbox/model.OutboxEvent`
- `internal/outbox/repository.IOutboxRepository`
- `internal/outbox/service.IOutboxService`
- `internal/outbox/service.EventPublisher`
- `internal/outbox/service.PublisherWorker`
- Redis Streams `EventPublisher` adapter backed by `pkg/redis.IRedis.XAdd`
- `internal/outbox/consumer.RedisConsumer`
- Redis Streams consumer group reads with `XREADGROUP`, success acknowledgements with `XACK`, and stale pending recovery with `XAUTOCLAIM`
- Redis-backed processed-event keys for consumed-event idempotency
- bounded Redis Streams consumer failure counters and dead-letter stream movement
- `order.created` event creation inside the existing order Unit of Work transaction
- retry bookkeeping with `attempts`, `next_attempt_at`, `processing`, and `dead_letter`
- short claim transactions with `FOR UPDATE SKIP LOCKED`, followed by publish outside the DB transaction

Not implemented yet:

- real downstream payment, email, fulfillment, or analytics handlers
- always-on publisher startup by default

## Candidate future order events

- `payment.requested`
- `inventory.reserved`
- `order.cancelled`

## Outbox table

Example table shape:

```sql
CREATE TABLE outbox_events (
  id TEXT PRIMARY KEY,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  locked_at TIMESTAMPTZ,
  locked_by VARCHAR(128),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ
);
```

When an order is created, the transaction writes:

1. product reads
2. atomic inventory deduction
3. the order
4. the order lines
5. one `outbox_events` row for `order.created`

All writes commit or roll back together.

## Event schema example

```json
{
  "order_id": "order-id",
  "user_id": "user-id",
  "total_price": 125.5,
  "status": "new",
  "lines": [
    {
      "product_id": "product-id",
      "quantity": 1,
      "price": 125.5
    }
  ]
}
```

The outbox row itself carries the event metadata:

```text
aggregate_type = "order"
aggregate_id = "<order_id>"
event_type = "order.created"
status = "pending"
attempts = 0
```

## Background publisher

The code currently defines the minimal publisher abstraction:

```go
type EventPublisher interface {
    Publish(ctx context.Context, event *model.OutboxEvent) error
}
```

The operational worker can run one controlled batch through `PublisherWorker.RunOnce(ctx)`. Optional background startup is controlled by config and is disabled by default.

Relevant config values:

```text
outbox_publisher_enabled = false
outbox_publisher_type = log
outbox_redis_stream_name = stream:orders
outbox_publish_batch_size = 100
outbox_publish_max_attempts = 3
outbox_publish_retry_base_seconds = 60
outbox_publish_interval_seconds = 30
outbox_processing_timeout_seconds = 900
outbox_consumer_enabled = false
outbox_consumer_group = order-events
outbox_consumer_name = local-consumer-1
outbox_consumer_batch_size = 10
outbox_consumer_block_seconds = 5
outbox_consumer_processed_ttl_seconds = 86400
outbox_consumer_claim_min_idle_seconds = 60
outbox_consumer_claim_batch_size = 10
outbox_consumer_max_delivery_attempts = 5
outbox_consumer_failure_ttl_seconds = 86400
outbox_dead_letter_stream_name = stream:orders:dead_letter
```

Supported publisher types are `log` and `redis_stream`. The current default publisher is `log`, which records event metadata such as event type and aggregate ID, and does not log full payloads.

Enable Redis Streams publishing locally with:

```yaml
outbox_publisher_enabled: true
outbox_publisher_type: redis_stream
outbox_redis_stream_name: stream:orders
```

When `redis_stream` is selected, the worker publishes each due outbox row with Redis Streams `XADD`. The stream entry contains:

```text
event_id
aggregate_type
aggregate_id
event_type
payload
created_at
```

The payload is included as JSON in the stream entry, but payload contents are not logged by the publisher.

Enable the Redis Streams consumer foundation locally with:

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

The worker:

1. claim pending outbox rows whose `next_attempt_at <= now()` in a short transaction
2. mark claimed rows as `processing` with `locked_at` and `locked_by`
3. commit the claim transaction before calling the configured `EventPublisher`
4. publish each event outside the database transaction
5. finalize each event in a short transaction:
   - success: mark the row `published`
   - transient failure: increment `attempts`, clear the processing claim, and set `next_attempt_at`
   - exhausted failure: move the row to `dead_letter`

Claim queries use `FOR UPDATE SKIP LOCKED` so multiple enabled workers avoid claiming the same row. Publishing happens after the claim commits, so Redis Streams `XADD` or future broker I/O does not hold PostgreSQL row locks. Rows left in `processing` longer than `outbox_processing_timeout_seconds` are treated as stale and may be claimed again by a later worker. This recovery path favors durable eventual delivery; future non-idempotent publishers should keep downstream writes idempotent by `event_id`.

## Redis Streams consumer foundation

The current consumer foundation:

- ensures consumer group `order-events` exists with `XGROUP CREATE ... MKSTREAM`
- reads new stream messages with `XREADGROUP GROUP`
- parses `event_id`, `aggregate_type`, `aggregate_id`, `event_type`, `payload`, and `created_at`
- checks `processed:events:{eventID}` before handling
- dispatches to an `EventHandler`
- marks the event processed after successful handling
- acknowledges successful or already-processed messages with `XACK`
- increments `consumer:failures:{stream}:{group}:{eventID}` on handler failures
- leaves failed messages unacknowledged while the failure count is below `outbox_consumer_max_delivery_attempts`
- writes poison messages and invalid messages to `stream:orders:dead_letter`
- acknowledges dead-lettered originals with `XACK` only after the dead-letter `XADD` succeeds
- claims stale pending messages with `XAUTOCLAIM`
- logs metadata only and does not log payload contents

Failure increments and first-key expiration execute atomically in one Redis Lua operation. The retry window cannot become permanent because a process failed between separate `INCR` and `EXPIRE` calls.

The built-in handler is intentionally a metadata-only log/no-op handler. It provides a safe framework for future side-effect handlers without sending emails, starting fulfillment, authorizing payments, or writing analytics yet.

## Idempotent consumers

Consumers must not rely only on `XACK` for idempotency because messages can be redelivered. The current Redis processed-event store uses:

```text
processed:events:{eventID}
```

The foundation checks the key before handling and sets it after the current handler succeeds. This ordering prevents acknowledging an event before handler success. If a future handler performs non-idempotent business side effects, it should either make those side effects idempotent by `event_id` or move the processed-event marker into the same durable commit boundary as the side effect.

## Pending and processing recovery

Database outbox rows recover through two paths. Rows in `pending` retry when `next_attempt_at` becomes due. Rows in `processing` retry when `locked_at` is older than `outbox_processing_timeout_seconds`; a later publisher claim clears the stale ownership by assigning a new `locked_at` and `locked_by`.

The Redis Streams consumer pending recovery path uses `XAUTOCLAIM` for messages older than `outbox_consumer_claim_min_idle_seconds`, processes them through the same handler, and applies the same failure counter and dead-letter rules as new messages.

Failure counters use this Redis key pattern:

```text
consumer:failures:{stream}:{group}:{eventID}
```

If the event ID cannot be parsed, the stream message ID is used in dead-letter metadata. Handler failures below `outbox_consumer_max_delivery_attempts` stay pending for later retry. Failure counters expire after `outbox_consumer_failure_ttl_seconds`, which defaults to 86400 seconds. When the failure count reaches the max attempts, the consumer writes the original message to `outbox_dead_letter_stream_name` and acknowledges the original message only after that write succeeds.

Dead-letter entries include:

- `original_stream`
- `original_group`
- `original_message_id`
- `event_id`
- `event_type`
- `aggregate_type`
- `aggregate_id`
- `payload`
- `failure_count`
- `error_type`
- `dead_lettered_at`

Invalid messages are copied to the dead-letter stream with `error_type=parse_error` and are acknowledged only after the dead-letter write succeeds. Raw payloads are not logged. Duplicate processed events are acknowledged and counted as skipped without incrementing failure counters or creating dead-letter entries.

## Retry strategy

The current service records failures by incrementing `attempts` and scheduling `next_attempt_at`. The default foundation uses a bounded retry count and moves exhausted events to `dead_letter`.

Future production workers should add:

- short retry delay for transient broker/network errors
- maximum attempt count
- dead-letter status after repeated failures
- visibility into last error details if the schema is extended

## Dead-letter handling

Dead-lettered events should be visible in logs and dashboards. Operators should be able to inspect the payload in the dead-letter stream, understand the failure class from metadata, and manually retry after fixing the root cause.

Useful Redis checks:

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

Useful PostgreSQL checks:

```sql
SELECT count(*) FROM outbox_events WHERE status = 'pending';
SELECT count(*) FROM outbox_events WHERE status = 'processing' AND locked_at < now() - interval '15 minutes';
SELECT status, count(*) FROM outbox_events GROUP BY status;
```

Alert on sustained dead-letter stream growth, old pending entries, and unexpected duplicate skip spikes.

## Observability

Recommended metrics and logs:

- `outbox_events_created_total`
- `outbox_publish_attempt_total`
- `outbox_publish_success_total`
- `outbox_publish_failure_total`
- `outbox_publish_duration_seconds`
- `outbox_dead_letter_total`
- `outbox_claim_total`
- `outbox_claim_failure_total`
- `outbox_finalize_failure_total`
- `outbox_consumer_read_total`
- `outbox_consumer_ack_total`
- `outbox_consumer_failure_total`
- `outbox_consumer_duplicate_skipped_total`
- `outbox_consumer_stale_claim_total`
- `outbox_consumer_dead_letter_total`

Use SQL/Redis exporter queries or operational scripts for current-state gauges that the app does not emit yet, such as DB pending count, oldest pending age, Redis consumer group pending count, and DLQ stream length.

Current logs include batch completion counts, publish failures, consumer failures, dead-letter counts, duplicate skip counts, event type, event ID, and aggregate ID. Payloads are intentionally not logged by default.

Prometheus labels for outbox and consumer metrics are bounded to `event_type`, `result`, and `reason`. Unknown event types are collapsed to `unknown` to avoid high-cardinality label growth. Do not add event IDs, aggregate IDs, order IDs, raw Redis keys, or payload values as metric labels.

## Migration note

`migrations/000001_initial_schema.up.sql` is the authoritative outbox schema. It includes `pending`, `processing`, `published`, and `dead_letter` status validation; `locked_at`; `locked_by`; the due-pending partial index; and the stale-processing partial index. Production requires `database_auto_migrate=false` and applies the reviewed migration before API rollout. `docs/migrations/outbox_events.sql` is retained only as a readable reference and must not replace the versioned migration workflow.

## Reliability benefit

The outbox pattern makes order events durable and transactionally aligned with order writes. It avoids the dual-write problem between PostgreSQL and an external broker while still allowing downstream services to process order lifecycle events asynchronously.

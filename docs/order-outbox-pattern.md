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
- `order.created` event creation inside the existing order Unit of Work transaction
- retry bookkeeping with `attempts`, `next_attempt_at`, and `dead_letter`
- locked pending-event batch fetching with `FOR UPDATE SKIP LOCKED`

Not implemented yet:

- real downstream payment, email, fulfillment, or analytics handlers
- poison-message dead-letter stream movement
- always-on publisher startup by default

## Candidate future order events

- `payment.requested`
- `inventory.reserved`
- `order.cancelled`

## Outbox table

Example table shape:

```sql
CREATE TABLE outbox_events (
  id UUID PRIMARY KEY,
  aggregate_type TEXT NOT NULL,
  aggregate_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
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
outbox_consumer_enabled = false
outbox_consumer_group = order-events
outbox_consumer_name = local-consumer-1
outbox_consumer_batch_size = 10
outbox_consumer_block_seconds = 5
outbox_consumer_processed_ttl_seconds = 86400
outbox_consumer_claim_min_idle_seconds = 60
outbox_consumer_claim_batch_size = 10
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
```

The worker:

1. select pending outbox rows whose `next_attempt_at <= now()`
2. publish each event through the configured `EventPublisher`
3. mark successful rows as published
4. increment attempts and set `next_attempt_at` with backoff for failures
5. move exhausted events to a dead-letter state

Locked fetches use `FOR UPDATE SKIP LOCKED` so multiple enabled workers can avoid claiming the same pending rows while a batch transaction is active.

## Redis Streams consumer foundation

The current consumer foundation:

- ensures consumer group `order-events` exists with `XGROUP CREATE ... MKSTREAM`
- reads new stream messages with `XREADGROUP GROUP`
- parses `event_id`, `aggregate_type`, `aggregate_id`, `event_type`, `payload`, and `created_at`
- checks `processed:events:{eventID}` before handling
- dispatches to an `EventHandler`
- marks the event processed after successful handling
- acknowledges successful or already-processed messages with `XACK`
- leaves failed or invalid messages unacknowledged
- claims stale pending messages with `XAUTOCLAIM`
- logs metadata only and does not log payload contents

The built-in handler is intentionally a metadata-only log/no-op handler. It provides a safe framework for future side-effect handlers without sending emails, starting fulfillment, authorizing payments, or writing analytics yet.

## Idempotent consumers

Consumers must not rely only on `XACK` for idempotency because messages can be redelivered. The current Redis processed-event store uses:

```text
processed:events:{eventID}
```

The foundation checks the key before handling and sets it after the current handler succeeds. This ordering prevents acknowledging an event before handler success. If a future handler performs non-idempotent business side effects, it should either make those side effects idempotent by `event_id` or move the processed-event marker into the same durable commit boundary as the side effect.

## Pending recovery and dead-letter strategy

The current pending recovery path uses `XAUTOCLAIM` for messages older than `outbox_consumer_claim_min_idle_seconds`, processes them through the same handler, and acknowledges only on success.

Future poison-message handling should use:

- pending inspection with `XPENDING`
- bounded delivery attempts stored outside the stream or in message metadata
- dead-letter stream: `stream:orders:dead_letter`
- no `XACK` until the original message is either successfully handled or durably copied to the dead-letter stream

## Retry strategy

The current service records failures by incrementing `attempts` and scheduling `next_attempt_at`. The default foundation uses a bounded retry count and moves exhausted events to `dead_letter`.

Future production workers should add:

- short retry delay for transient broker/network errors
- maximum attempt count
- dead-letter status after repeated failures
- visibility into last error details if the schema is extended

## Dead-letter handling

Dead-lettered events should be visible in logs and dashboards. Operators should be able to inspect the payload, understand the failure, and manually retry after fixing the root cause.

## Observability

Recommended metrics and logs:

- `outbox_pending_count`
- `outbox_published_count`
- `outbox_publish_failed_count`
- `outbox_dead_letter_count`
- `outbox_publish_latency_ms`
- `outbox_oldest_pending_age_seconds`

Current logs include batch completion counts, publish failures, event type, event ID, and aggregate ID. Payloads are intentionally not logged by default.

## Migration note

The current application uses GORM `AutoMigrate`, and `cmd/api/main.go` includes `OutboxEvent` in that startup migration list. For a long-lived production database, move the outbox table to explicit reviewed migrations before running multiple production instances. Use `docs/migrations/outbox_events.sql` as the production migration reference.

## Reliability benefit

The outbox pattern makes order events durable and transactionally aligned with order writes. It avoids the dual-write problem between PostgreSQL and an external broker while still allowing downstream services to process order lifecycle events asynchronously.

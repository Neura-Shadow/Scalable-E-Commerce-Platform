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
- `order.created` event creation inside the existing order Unit of Work transaction
- retry bookkeeping with `attempts`, `next_attempt_at`, and `dead_letter`
- locked pending-event batch fetching with `FOR UPDATE SKIP LOCKED`

Not implemented yet:

- external broker integration
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
outbox_publish_batch_size = 100
outbox_publish_max_attempts = 3
outbox_publish_retry_base_seconds = 60
outbox_publish_interval_seconds = 30
```

The current default publisher is a no-op/log publisher. It logs event metadata such as event type and aggregate ID, and it does not log full payloads.

The worker:

1. select pending outbox rows whose `next_attempt_at <= now()`
2. publish each event through the configured `EventPublisher`
3. mark successful rows as published
4. increment attempts and set `next_attempt_at` with backoff for failures
5. move exhausted events to a dead-letter state

Locked fetches use `FOR UPDATE SKIP LOCKED` so multiple enabled workers can avoid claiming the same pending rows while a batch transaction is active.

## Idempotent consumers

Consumers should store processed `event_id` values or use natural idempotency keys. This protects downstream services from duplicate delivery caused by publisher retries.

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

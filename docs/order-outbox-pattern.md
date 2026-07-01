# Order Outbox Pattern

The current ordering flow writes orders and order lines in a single database transaction. Future integrations such as payment, email, fulfillment, or analytics should not publish external events directly inside that transaction.

## Why direct publishing is risky

If the service publishes to a broker inside the transaction and the database later rolls back, downstream services may receive an event for an order that does not exist.

If the service commits the transaction and then fails before publishing, the order exists but no downstream service is notified.

The transactional outbox pattern solves this by writing events to an outbox table in the same database transaction as the order change.

## Candidate order events

- `order.created`
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

When an order is created, the transaction should write:

1. the order
2. the order lines
3. one `outbox_events` row for `order.created`

All three writes commit or roll back together.

## Event schema example

```json
{
  "event_id": "2e47e7a7-8ad6-4d8d-9aa5-72fd651a18f8",
  "event_type": "order.created",
  "occurred_at": "2026-07-01T00:00:00Z",
  "order": {
    "id": "order-id",
    "user_id": "user-id",
    "total_price": 125.50,
    "status": "new",
    "lines": [
      {
        "product_id": "product-id",
        "quantity": 1,
        "price": 125.50
      }
    ]
  }
}
```

## Background publisher

A separate worker should:

1. select pending outbox rows whose `next_attempt_at <= now()`
2. publish each event to the message broker
3. mark successful rows as published
4. increment attempts and set `next_attempt_at` with backoff for failures
5. move exhausted events to a dead-letter state

Use row locking such as `FOR UPDATE SKIP LOCKED` if multiple publishers run concurrently.

## Idempotent consumers

Consumers should store processed `event_id` values or use natural idempotency keys. This protects downstream services from duplicate delivery caused by publisher retries.

## Retry strategy

Use bounded exponential backoff:

- short retry delay for transient broker/network errors
- maximum attempt count
- dead-letter status after repeated failures

## Dead-letter handling

Dead-lettered events should be visible in logs and dashboards. Operators should be able to inspect the payload, understand the failure, and manually retry after fixing the root cause.

## Reliability benefit

The outbox pattern makes order events durable and transactionally aligned with order writes. It avoids the dual-write problem between PostgreSQL and an external broker while still allowing downstream services to process order lifecycle events asynchronously.

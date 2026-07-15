CREATE TABLE IF NOT EXISTS outbox_events (
  id TEXT PRIMARY KEY,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  locked_at TIMESTAMPTZ,
  locked_by VARCHAR(128),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ,
  CONSTRAINT outbox_events_status_check
    CHECK (status IN ('pending', 'processing', 'published', 'dead_letter')),
  CONSTRAINT outbox_events_attempts_check
    CHECK (attempts >= 0)
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_status_next_attempt_at
  ON outbox_events (status, next_attempt_at)
  WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_outbox_events_processing_locked_at
  ON outbox_events (locked_at)
  WHERE status = 'processing';

CREATE INDEX IF NOT EXISTS idx_outbox_aggregate
  ON outbox_events (aggregate_type, aggregate_id);

CREATE INDEX IF NOT EXISTS idx_outbox_events_event_type
  ON outbox_events (event_type);

CREATE INDEX IF NOT EXISTS idx_outbox_events_created_at
  ON outbox_events (created_at);

CREATE INDEX IF NOT EXISTS idx_outbox_events_published_at
  ON outbox_events (published_at)
  WHERE published_at IS NOT NULL;

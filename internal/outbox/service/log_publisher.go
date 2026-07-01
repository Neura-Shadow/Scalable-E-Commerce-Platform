package service

import (
	"context"

	"github.com/quangdangfit/gocommon/logger"

	"goshop/internal/outbox/model"
)

type LogPublisher struct{}

func NewLogPublisher() *LogPublisher {
	return &LogPublisher{}
}

func (p *LogPublisher) Publish(_ context.Context, event *model.OutboxEvent) error {
	logger.Info("outbox_publish_noop event_id=", event.ID, " event_type=", event.EventType, " aggregate_id=", event.AggregateID)
	return nil
}

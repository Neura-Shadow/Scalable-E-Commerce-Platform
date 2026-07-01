package consumer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/quangdangfit/gocommon/logger"

	"goshop/pkg/redis"
)

type StreamEvent struct {
	MessageID     string
	EventID       string
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       json.RawMessage
	CreatedAt     time.Time
}

type EventHandler interface {
	Handle(ctx context.Context, event StreamEvent) error
}

type LogEventHandler struct{}

func NewLogEventHandler() *LogEventHandler {
	return &LogEventHandler{}
}

func (h *LogEventHandler) Handle(_ context.Context, event StreamEvent) error {
	logger.Info(
		"outbox_consumer_handled event_id=", event.EventID,
		" event_type=", event.EventType,
		" aggregate_id=", event.AggregateID,
		" stream_message_id=", event.MessageID,
	)
	return nil
}

func ParseStreamMessage(message redis.RedisStreamMessage) (StreamEvent, error) {
	eventID, err := requiredString(message.Values, "event_id")
	if err != nil {
		return StreamEvent{}, err
	}
	aggregateType, err := requiredString(message.Values, "aggregate_type")
	if err != nil {
		return StreamEvent{}, err
	}
	aggregateID, err := requiredString(message.Values, "aggregate_id")
	if err != nil {
		return StreamEvent{}, err
	}
	eventType, err := requiredString(message.Values, "event_type")
	if err != nil {
		return StreamEvent{}, err
	}
	payloadValue, err := requiredString(message.Values, "payload")
	if err != nil {
		return StreamEvent{}, err
	}
	if !json.Valid([]byte(payloadValue)) {
		return StreamEvent{}, ErrInvalidPayload
	}
	createdAtValue, err := requiredString(message.Values, "created_at")
	if err != nil {
		return StreamEvent{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtValue)
	if err != nil {
		return StreamEvent{}, err
	}

	return StreamEvent{
		MessageID:     message.ID,
		EventID:       eventID,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Payload:       json.RawMessage(payloadValue),
		CreatedAt:     createdAt,
	}, nil
}

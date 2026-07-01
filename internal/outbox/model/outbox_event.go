package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type OutboxEventStatus string

const (
	OutboxEventStatusPending    OutboxEventStatus = "pending"
	OutboxEventStatusPublished  OutboxEventStatus = "published"
	OutboxEventStatusDeadLetter OutboxEventStatus = "dead_letter"
)

type OutboxEvent struct {
	ID            string            `json:"id" gorm:"unique;not null;index;primary_key"`
	AggregateType string            `json:"aggregate_type" gorm:"not null;index:idx_outbox_aggregate,priority:1"`
	AggregateID   string            `json:"aggregate_id" gorm:"not null;index:idx_outbox_aggregate,priority:2"`
	EventType     string            `json:"event_type" gorm:"not null;index"`
	Payload       datatypes.JSON    `json:"payload" gorm:"type:jsonb;not null"`
	Status        OutboxEventStatus `json:"status" gorm:"type:varchar(32);not null;index"`
	Attempts      int               `json:"attempts" gorm:"not null;default:0"`
	NextAttemptAt time.Time         `json:"next_attempt_at" gorm:"not null;index"`
	CreatedAt     time.Time         `json:"created_at"`
	PublishedAt   *time.Time        `json:"published_at" gorm:"index"`
}

func (event *OutboxEvent) BeforeCreate(tx *gorm.DB) error {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Status == "" {
		event.Status = OutboxEventStatusPending
	}
	if len(event.Payload) == 0 {
		event.Payload = datatypes.JSON([]byte("null"))
	}
	if event.NextAttemptAt.IsZero() {
		event.NextAttemptAt = time.Now().UTC()
	}

	return nil
}

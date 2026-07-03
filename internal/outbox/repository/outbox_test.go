package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"goshop/internal/outbox/model"
	dbMocks "goshop/pkg/dbs/mocks"
)

func TestCreatePendingUsesDatabaseCreate(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	repo := NewOutboxRepository(mockDB)
	event := &model.OutboxEvent{
		AggregateType: "order",
		AggregateID:   "order-1",
		EventType:     "order.created",
		Status:        model.OutboxEventStatusPending,
	}
	mockDB.On("Create", mock.Anything, event).Return(nil).Once()

	err := repo.CreatePending(context.Background(), event)

	require.NoError(t, err)
}

func TestListPendingReadyUsesPendingReadyQuery(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	repo := NewOutboxRepository(mockDB)
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	expected := []*model.OutboxEvent{{ID: "evt-1", Status: model.OutboxEventStatusPending}}

	mockDB.On(
		"Find",
		mock.Anything,
		mock.AnythingOfType("*[]*model.OutboxEvent"),
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).Run(func(args mock.Arguments) {
		result := args.Get(1).(*[]*model.OutboxEvent)
		*result = expected
	}).Return(nil).Once()

	events, err := repo.ListPendingReady(context.Background(), now, 10)

	require.NoError(t, err)
	assert.Equal(t, expected, events)
}

func TestPendingReadyLockedQueryUsesForUpdateSkipLocked(t *testing.T) {
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=test dbname=test",
		PreferSimpleProtocol: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)

	var events []*model.OutboxEvent
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	stmt := pendingReadyQuery(gormDB, context.Background(), now, 25, true).
		Find(&events).
		Statement

	sql := strings.ToUpper(stmt.SQL.String())
	assert.Contains(t, sql, "FOR UPDATE SKIP LOCKED")
	assert.Contains(t, sql, "ORDER BY CREATED_AT")
	assert.Contains(t, sql, "LIMIT 25")
	assert.Len(t, stmt.Vars, 2)
	assert.Equal(t, model.OutboxEventStatusPending, stmt.Vars[0])
	assert.Equal(t, now, stmt.Vars[1])
}

func TestClaimableReadyQueryUsesForUpdateSkipLockedAndStaleProcessingRecovery(t *testing.T) {
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=test dbname=test",
		PreferSimpleProtocol: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)

	var events []*model.OutboxEvent
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-15 * time.Minute)
	stmt := claimableReadyQuery(gormDB, context.Background(), now, staleBefore, 25, true).
		Find(&events).
		Statement

	sql := strings.ToUpper(stmt.SQL.String())
	assert.Contains(t, sql, "FOR UPDATE SKIP LOCKED")
	assert.Contains(t, sql, "STATUS =")
	assert.Contains(t, sql, "LOCKED_AT")
	assert.Contains(t, sql, "ORDER BY CREATED_AT")
	assert.Contains(t, sql, "LIMIT 25")
	assert.Len(t, stmt.Vars, 4)
	assert.Equal(t, model.OutboxEventStatusPending, stmt.Vars[0])
	assert.Equal(t, now, stmt.Vars[1])
	assert.Equal(t, model.OutboxEventStatusProcessing, stmt.Vars[2])
	assert.Equal(t, staleBefore, stmt.Vars[3])
}

func TestMarkPublishedLoadsAndUpdatesEvent(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	repo := NewOutboxRepository(mockDB)
	publishedAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

	mockDB.On("FindById", mock.Anything, "evt-1", &model.OutboxEvent{}).
		Run(func(args mock.Arguments) {
			event := args.Get(2).(*model.OutboxEvent)
			event.ID = "evt-1"
			event.Status = model.OutboxEventStatusPending
			lockedAt := time.Date(2026, 7, 1, 9, 59, 0, 0, time.UTC)
			event.LockedAt = &lockedAt
			event.LockedBy = "worker-1"
		}).
		Return(nil).Once()
	mockDB.On("Update", mock.Anything, mock.MatchedBy(func(event *model.OutboxEvent) bool {
		return event.ID == "evt-1" &&
			event.Status == model.OutboxEventStatusPublished &&
			event.PublishedAt != nil &&
			event.PublishedAt.Equal(publishedAt) &&
			event.LockedAt == nil &&
			event.LockedBy == ""
	})).Return(nil).Once()

	err := repo.MarkPublished(context.Background(), "evt-1", publishedAt)

	require.NoError(t, err)
}

func TestMarkPublishFailedIncrementsAttemptsAndSchedulesRetry(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	repo := NewOutboxRepository(mockDB)
	nextAttemptAt := time.Date(2026, 7, 1, 10, 5, 0, 0, time.UTC)

	mockDB.On("FindById", mock.Anything, "evt-1", &model.OutboxEvent{}).
		Run(func(args mock.Arguments) {
			event := args.Get(2).(*model.OutboxEvent)
			event.ID = "evt-1"
			event.Status = model.OutboxEventStatusProcessing
			event.Attempts = 2
			lockedAt := time.Date(2026, 7, 1, 9, 59, 0, 0, time.UTC)
			event.LockedAt = &lockedAt
			event.LockedBy = "worker-1"
		}).
		Return(nil).Once()
	mockDB.On("Update", mock.Anything, mock.MatchedBy(func(event *model.OutboxEvent) bool {
		return event.ID == "evt-1" &&
			event.Status == model.OutboxEventStatusPending &&
			event.Attempts == 3 &&
			event.NextAttemptAt.Equal(nextAttemptAt) &&
			event.LockedAt == nil &&
			event.LockedBy == ""
	})).Return(nil).Once()

	err := repo.MarkPublishFailed(context.Background(), "evt-1", nextAttemptAt)

	require.NoError(t, err)
}

func TestMarkDeadLetterLoadsAndUpdatesEvent(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	repo := NewOutboxRepository(mockDB)

	mockDB.On("FindById", mock.Anything, "evt-1", &model.OutboxEvent{}).
		Run(func(args mock.Arguments) {
			event := args.Get(2).(*model.OutboxEvent)
			event.ID = "evt-1"
			event.Status = model.OutboxEventStatusProcessing
			lockedAt := time.Date(2026, 7, 1, 9, 59, 0, 0, time.UTC)
			event.LockedAt = &lockedAt
			event.LockedBy = "worker-1"
		}).
		Return(nil).Once()
	mockDB.On("Update", mock.Anything, mock.MatchedBy(func(event *model.OutboxEvent) bool {
		return event.ID == "evt-1" &&
			event.Status == model.OutboxEventStatusDeadLetter &&
			event.LockedAt == nil &&
			event.LockedBy == ""
	})).Return(nil).Once()

	err := repo.MarkDeadLetter(context.Background(), "evt-1")

	require.NoError(t, err)
}

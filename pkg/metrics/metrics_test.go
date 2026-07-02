package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricLabelNamesStayBounded(t *testing.T) {
	ResetForTest()

	RecordHTTP("GET", "/api/v1/orders/:id", "200", time.Millisecond)
	RecordOrderPlace("failure", "insufficient_stock", time.Millisecond)
	RecordOrderIdempotencyDuplicate("replay")
	RecordOutboxEventCreated("order.created")
	RecordOutboxPublishFailure("order.created", "publish_error", time.Millisecond)
	RecordOutboxConsumerDeadLetter("order.created", "handler_error", "success")

	snapshot, err := SnapshotText()

	require.NoError(t, err)
	forbiddenLabels := []string{
		"user_id=",
		"order_id=",
		"event_id=",
		"idempotency_key=",
		"redis_key=",
	}
	for _, label := range forbiddenLabels {
		assert.NotContains(t, snapshot, label)
	}
}

func TestUnknownEventTypeLabelsCollapseToUnknown(t *testing.T) {
	ResetForTest()

	RecordOutboxConsumerFailure("order.created.user-123", "handler_error")

	snapshot, err := SnapshotText()

	require.NoError(t, err)
	assert.Contains(t, snapshot, `event_type="unknown"`)
	assert.NotContains(t, snapshot, "order.created.user-123")
}

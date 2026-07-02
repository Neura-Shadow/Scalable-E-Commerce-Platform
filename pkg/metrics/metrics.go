package metrics

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const unknownLabel = "unknown"

type registryState struct {
	registry *prometheus.Registry

	httpRequests        *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec

	orderPlace                 *prometheus.CounterVec
	orderPlaceFailed           *prometheus.CounterVec
	orderPlaceDuration         *prometheus.HistogramVec
	orderIdempotencyDuplicate  *prometheus.CounterVec
	orderRateLimited           *prometheus.CounterVec
	inventoryInsufficientStock prometheus.Counter

	outboxEventsCreated  *prometheus.CounterVec
	outboxPublishAttempt *prometheus.CounterVec
	outboxPublishSuccess *prometheus.CounterVec
	outboxPublishFailure *prometheus.CounterVec
	outboxPublishLatency *prometheus.HistogramVec
	outboxDeadLetter     *prometheus.CounterVec

	outboxConsumerRead             *prometheus.CounterVec
	outboxConsumerHandlerSuccess   *prometheus.CounterVec
	outboxConsumerAck              *prometheus.CounterVec
	outboxConsumerFailure          *prometheus.CounterVec
	outboxConsumerDuplicateSkipped *prometheus.CounterVec
	outboxConsumerStaleClaim       prometheus.Counter
	outboxConsumerDeadLetter       *prometheus.CounterVec
}

var (
	mu    sync.RWMutex
	state = newRegistryState()
)

func newRegistryState() *registryState {
	registry := prometheus.NewRegistry()
	s := &registryState{
		registry: registry,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests received by method, normalized path, and status.",
		}, []string{"method", "path", "status"}),
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration by method, normalized path, and status.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path", "status"}),
		orderPlace: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "order_place_total",
			Help: "Total order placement attempts by result and reason.",
		}, []string{"result", "reason"}),
		orderPlaceFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "order_place_failed_total",
			Help: "Total failed order placement attempts by reason.",
		}, []string{"reason"}),
		orderPlaceDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "order_place_duration_seconds",
			Help:    "Order placement duration by result and reason.",
			Buckets: prometheus.DefBuckets,
		}, []string{"result", "reason"}),
		orderIdempotencyDuplicate: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "order_idempotency_duplicate_total",
			Help: "Total idempotent order duplicate paths by reason.",
		}, []string{"reason"}),
		orderRateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "order_rate_limited_total",
			Help: "Total order placement requests rejected by rate limiting.",
		}, []string{"reason"}),
		inventoryInsufficientStock: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "inventory_insufficient_stock_total",
			Help: "Total order placement attempts rejected because inventory was insufficient.",
		}),
		outboxEventsCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_events_created_total",
			Help: "Total durable outbox events created by event type.",
		}, []string{"event_type"}),
		outboxPublishAttempt: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_publish_attempt_total",
			Help: "Total outbox publish attempts by event type.",
		}, []string{"event_type"}),
		outboxPublishSuccess: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_publish_success_total",
			Help: "Total successful outbox publishes by event type.",
		}, []string{"event_type"}),
		outboxPublishFailure: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_publish_failure_total",
			Help: "Total failed outbox publishes by event type and reason.",
		}, []string{"event_type", "reason"}),
		outboxPublishLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "outbox_publish_duration_seconds",
			Help:    "Outbox publish duration by event type and result.",
			Buckets: prometheus.DefBuckets,
		}, []string{"event_type", "result"}),
		outboxDeadLetter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_dead_letter_total",
			Help: "Total DB outbox events moved to dead-letter status by event type and reason.",
		}, []string{"event_type", "reason"}),
		outboxConsumerRead: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_consumer_read_total",
			Help: "Total Redis Streams consumer messages read by event type.",
		}, []string{"event_type"}),
		outboxConsumerHandlerSuccess: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_consumer_handler_success_total",
			Help: "Total Redis Streams consumer handler successes by event type.",
		}, []string{"event_type"}),
		outboxConsumerAck: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_consumer_ack_total",
			Help: "Total Redis Streams consumer acknowledgements by event type and result.",
		}, []string{"event_type", "result"}),
		outboxConsumerFailure: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_consumer_failure_total",
			Help: "Total Redis Streams consumer failures by event type and reason.",
		}, []string{"event_type", "reason"}),
		outboxConsumerDuplicateSkipped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_consumer_duplicate_skipped_total",
			Help: "Total Redis Streams consumer duplicate processed events skipped by event type.",
		}, []string{"event_type"}),
		outboxConsumerStaleClaim: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "outbox_consumer_stale_claim_total",
			Help: "Total stale Redis Streams messages claimed with XAUTOCLAIM.",
		}),
		outboxConsumerDeadLetter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_consumer_dead_letter_total",
			Help: "Total Redis Streams consumer dead-letter writes by event type, reason, and result.",
		}, []string{"event_type", "reason", "result"}),
	}

	registry.MustRegister(
		s.httpRequests,
		s.httpRequestDuration,
		s.orderPlace,
		s.orderPlaceFailed,
		s.orderPlaceDuration,
		s.orderIdempotencyDuplicate,
		s.orderRateLimited,
		s.inventoryInsufficientStock,
		s.outboxEventsCreated,
		s.outboxPublishAttempt,
		s.outboxPublishSuccess,
		s.outboxPublishFailure,
		s.outboxPublishLatency,
		s.outboxDeadLetter,
		s.outboxConsumerRead,
		s.outboxConsumerHandlerSuccess,
		s.outboxConsumerAck,
		s.outboxConsumerFailure,
		s.outboxConsumerDuplicateSkipped,
		s.outboxConsumerStaleClaim,
		s.outboxConsumerDeadLetter,
	)

	return s
}

func Handler() http.Handler {
	return promhttp.HandlerFor(current().registry, promhttp.HandlerOpts{})
}

func SnapshotText() (string, error) {
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	writer := httptest.NewRecorder()
	Handler().ServeHTTP(writer, request)
	return writer.Body.String(), nil
}

func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	state = newRegistryState()
}

func RecordHTTP(method, path, status string, duration time.Duration) {
	labels := []string{label(method), label(path), label(status)}
	s := current()
	s.httpRequests.WithLabelValues(labels...).Inc()
	s.httpRequestDuration.WithLabelValues(labels...).Observe(seconds(duration))
}

func RecordHTTPStatus(method, path string, status int, duration time.Duration) {
	RecordHTTP(method, path, strconv.Itoa(status), duration)
}

func RecordOrderPlace(result, reason string, duration time.Duration) {
	result = label(result)
	reason = label(reason)
	s := current()
	s.orderPlace.WithLabelValues(result, reason).Inc()
	s.orderPlaceDuration.WithLabelValues(result, reason).Observe(seconds(duration))
	if result == "failure" {
		s.orderPlaceFailed.WithLabelValues(reason).Inc()
	}
}

func RecordOrderIdempotencyDuplicate(reason string) {
	current().orderIdempotencyDuplicate.WithLabelValues(label(reason)).Inc()
}

func RecordOrderRateLimited(reason string) {
	current().orderRateLimited.WithLabelValues(label(reason)).Inc()
}

func RecordInventoryInsufficientStock() {
	current().inventoryInsufficientStock.Inc()
}

func RecordOutboxEventCreated(eventType string) {
	current().outboxEventsCreated.WithLabelValues(label(eventType)).Inc()
}

func RecordOutboxPublishAttempt(eventType string) {
	current().outboxPublishAttempt.WithLabelValues(label(eventType)).Inc()
}

func RecordOutboxPublishSuccess(eventType string, duration time.Duration) {
	s := current()
	eventType = label(eventType)
	s.outboxPublishSuccess.WithLabelValues(eventType).Inc()
	s.outboxPublishLatency.WithLabelValues(eventType, "success").Observe(seconds(duration))
}

func RecordOutboxPublishFailure(eventType, reason string, duration time.Duration) {
	s := current()
	eventType = label(eventType)
	s.outboxPublishFailure.WithLabelValues(eventType, label(reason)).Inc()
	s.outboxPublishLatency.WithLabelValues(eventType, "failure").Observe(seconds(duration))
}

func RecordOutboxDeadLetter(eventType, reason string) {
	current().outboxDeadLetter.WithLabelValues(label(eventType), label(reason)).Inc()
}

func RecordOutboxConsumerRead(eventType string, count int) {
	if count <= 0 {
		return
	}
	current().outboxConsumerRead.WithLabelValues(label(eventType)).Add(float64(count))
}

func RecordOutboxConsumerHandlerSuccess(eventType string) {
	current().outboxConsumerHandlerSuccess.WithLabelValues(label(eventType)).Inc()
}

func RecordOutboxConsumerAck(eventType, result string) {
	current().outboxConsumerAck.WithLabelValues(label(eventType), label(result)).Inc()
}

func RecordOutboxConsumerFailure(eventType, reason string) {
	current().outboxConsumerFailure.WithLabelValues(label(eventType), label(reason)).Inc()
}

func RecordOutboxConsumerDuplicateSkipped(eventType string) {
	current().outboxConsumerDuplicateSkipped.WithLabelValues(label(eventType)).Inc()
}

func RecordOutboxConsumerStaleClaim(count int) {
	if count <= 0 {
		return
	}
	current().outboxConsumerStaleClaim.Add(float64(count))
}

func RecordOutboxConsumerDeadLetter(eventType, reason, result string) {
	current().outboxConsumerDeadLetter.WithLabelValues(label(eventType), label(reason), label(result)).Inc()
}

func current() *registryState {
	mu.RLock()
	defer mu.RUnlock()
	return state
}

func label(value string) string {
	if value == "" {
		return unknownLabel
	}
	return value
}

func seconds(duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	return duration.Seconds()
}

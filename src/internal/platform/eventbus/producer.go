// Package eventbus wraps Redpanda (Kafka-API-compatible) for the ingress
// producer and the worker's partitioned consumer.
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/twmb/franz-go/pkg/kgo"
)

// ErrProducerSaturated is returned by Produce when the number of in-flight,
// not-yet-acknowledged records has reached maxInFlight. The ingress webhook
// translates this into a retryable HTTP status (429/503) instead of
// accepting unbounded backlog in memory (design.md decision: backpressure).
var ErrProducerSaturated = errors.New("eventbus: producer pending-delivery buffer is full")

// Producer publishes events to a single topic, partitioned by record key.
type Producer struct {
	client *kgo.Client
	topic  string
	sem    chan struct{}
	logger *slog.Logger
}

// NewProducer connects to the given Redpanda/Kafka brokers and returns a
// producer for topic. maxInFlight bounds how many records may be awaiting
// broker acknowledgment at once; Produce returns ErrProducerSaturated once
// that bound is reached, rather than blocking or buffering unboundedly.
func NewProducer(brokers []string, topic string, maxInFlight int, logger *slog.Logger) (*Producer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.DefaultProduceTopic(topic),
	)
	if err != nil {
		return nil, fmt.Errorf("eventbus: create producer client: %w", err)
	}
	return &Producer{
		client: client,
		topic:  topic,
		sem:    make(chan struct{}, maxInFlight),
		logger: logger,
	}, nil
}

// Produce enqueues value under key (the tenant_id, so all of a tenant's
// events land in the same partition and stay ordered) and returns as soon
// as the record is accepted for delivery — it does not wait for the
// broker's acknowledgment, keeping the ingress webhook's response latency
// decoupled from Redpanda round-trip time. Delivery failures are logged
// asynchronously from the callback.
//
// The actual send is detached from ctx's cancellation (via
// context.WithoutCancel) rather than tied to it directly: ctx is typically
// an HTTP request context that gets canceled the moment the handler
// returns, which is exactly when Produce is meant to still be in flight.
// Any values on ctx (e.g. trace propagation) are preserved.
func (p *Producer) Produce(ctx context.Context, key, value []byte) error {
	select {
	case p.sem <- struct{}{}:
	default:
		return ErrProducerSaturated
	}

	record := &kgo.Record{Topic: p.topic, Key: key, Value: value}
	sendCtx := context.WithoutCancel(ctx)
	p.client.Produce(sendCtx, record, func(r *kgo.Record, err error) {
		<-p.sem
		if err != nil && p.logger != nil {
			p.logger.Error("eventbus: delivery failed", "error", err, "topic", r.Topic)
		}
	})
	return nil
}

// Close flushes any in-flight produces and closes the underlying client.
func (p *Producer) Close() {
	p.client.Close()
}

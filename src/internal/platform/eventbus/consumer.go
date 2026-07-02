package eventbus

import (
	"context"
	"log/slog"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Handler processes a single event's raw value. The tenant_id needed for
// any downstream locking/state lookups is embedded in the decoded event
// itself (see contracts/events/v1), so the handler does not need to know
// about Kafka partitions or records at all.
type Handler func(ctx context.Context, value []byte) error

// Consumer consumes a topic as part of a consumer group, processing each
// assigned partition sequentially on its own goroutine so that a tenant's
// events (all routed to one partition by key) are always handled in order,
// while different partitions make progress concurrently.
type Consumer struct {
	client  *kgo.Client
	handler Handler
	logger  *slog.Logger

	mu         sync.Mutex
	partitions map[int32]*partitionWorker
}

type partitionWorker struct {
	records chan *kgo.Record
	done    chan struct{}
}

// NewConsumer joins group and subscribes to topic on the given brokers.
func NewConsumer(brokers []string, topic, group string, handler Handler, logger *slog.Logger) (*Consumer, error) {
	c := &Consumer{
		handler:    handler,
		logger:     logger,
		partitions: make(map[int32]*partitionWorker),
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumeTopics(topic),
		kgo.ConsumerGroup(group),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(c.onAssigned),
		kgo.OnPartitionsRevoked(c.onRevokedOrLost),
		kgo.OnPartitionsLost(c.onRevokedOrLost),
	)
	if err != nil {
		return nil, err
	}
	c.client = client
	return c, nil
}

// onAssigned starts one goroutine per newly assigned partition. Each
// goroutine drains its own channel sequentially, so ordering within a
// partition (and therefore within a tenant) is preserved.
func (c *Consumer) onAssigned(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, parts := range assigned {
		for _, p := range parts {
			w := &partitionWorker{records: make(chan *kgo.Record, 64), done: make(chan struct{})}
			c.partitions[p] = w
			go c.runPartition(p, w)
		}
	}
}

// onRevokedOrLost stops accepting new records for the revoked partitions
// and blocks until each partition's in-flight message finishes processing,
// so a rebalance can never overlap with a still-running handler for that
// partition (design.md, decision 6 — this is the consumer-side half of the
// rebalance safety net; the Redis lock is the cross-instance half).
func (c *Consumer) onRevokedOrLost(ctx context.Context, _ *kgo.Client, revoked map[string][]int32) {
	c.mu.Lock()
	var draining []*partitionWorker
	for _, parts := range revoked {
		for _, p := range parts {
			if w, ok := c.partitions[p]; ok {
				close(w.records)
				draining = append(draining, w)
				delete(c.partitions, p)
			}
		}
	}
	c.mu.Unlock()

	for _, w := range draining {
		select {
		case <-w.done:
		case <-ctx.Done():
			return
		}
	}
}

func (c *Consumer) runPartition(partition int32, w *partitionWorker) {
	defer close(w.done)
	for record := range w.records {
		ctx := context.Background()
		if err := c.handler(ctx, record.Value); err != nil {
			c.logger.Error("eventbus: handler failed, offset will not be committed", "partition", partition, "error", err)
			continue
		}
		if err := c.client.CommitRecords(ctx, record); err != nil {
			c.logger.Error("eventbus: commit failed", "partition", partition, "error", err)
		}
	}
}

// Run polls for records and dispatches them to their partition's goroutine
// until ctx is canceled.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		fetches := c.client.PollFetches(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		for _, e := range fetches.Errors() {
			c.logger.Error("eventbus: fetch error", "topic", e.Topic, "partition", e.Partition, "error", e.Err)
		}

		fetches.EachPartition(func(p kgo.FetchTopicPartition) {
			c.mu.Lock()
			w, ok := c.partitions[p.Partition]
			c.mu.Unlock()
			if !ok {
				return // revoked mid-fetch; drop, will be redelivered after rebalance
			}
			for _, record := range p.Records {
				select {
				case w.records <- record:
				case <-ctx.Done():
					return
				}
			}
		})
	}
}

// Close leaves the consumer group (triggering a final revoke/drain of all
// owned partitions) and closes the underlying client.
func (c *Consumer) Close() {
	c.client.Close()
}

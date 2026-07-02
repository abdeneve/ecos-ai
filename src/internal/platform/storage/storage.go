// Package storage wraps ScyllaDB for the agent worker's immutable history:
// processed messages and session state transitions. Redis (see
// internal/platform/cache) remains the source of truth for the *current*
// session state; this package is the append-only record of how a session
// got there.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/gocql/gocql"

	"github.com/abdeneve/ecos-ai/src/internal/statemachine"
)

// Client wraps a ScyllaDB (CQL) session.
type Client struct {
	session *gocql.Session
}

// New connects to the ScyllaDB cluster at hosts and returns a client
// scoped to keyspace (see migrations/0001_init.cql).
func New(hosts []string, keyspace string) (*Client, error) {
	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = keyspace
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = 5 * time.Second

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("storage: connect to scylla: %w", err)
	}
	return &Client{session: session}, nil
}

// Close releases the underlying ScyllaDB session.
func (c *Client) Close() {
	c.session.Close()
}

// timeBucket buckets a timestamp by UTC day, keeping any single tenant's
// partition bounded (migrations/0001_init.cql).
func timeBucket(t time.Time) string {
	return t.UTC().Format("20060102")
}

// InsertMessage appends a processed message to the immutable history log.
func (c *Client) InsertMessage(ctx context.Context, tenantID, messageID, traceID string, occurredAt time.Time, payload []byte) error {
	q := c.session.Query(
		`INSERT INTO message_history (tenant_id, time_bucket, occurred_at, message_id, trace_id, payload)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		tenantID, timeBucket(occurredAt), occurredAt, messageID, traceID, string(payload),
	).WithContext(ctx)
	if err := q.Exec(); err != nil {
		return fmt.Errorf("storage: insert message history for tenant %q: %w", tenantID, err)
	}
	return nil
}

// InsertTransition appends a state machine transition to the audit trail.
func (c *Client) InsertTransition(ctx context.Context, tenantID string, tr statemachine.Transition) error {
	q := c.session.Query(
		`INSERT INTO session_transitions (tenant_id, time_bucket, at, from_state, to_state, transitioned_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		tenantID, timeBucket(tr.At), tr.At, string(tr.From), string(tr.To), string(tr.By),
	).WithContext(ctx)
	if err := q.Exec(); err != nil {
		return fmt.Errorf("storage: insert transition for tenant %q: %w", tenantID, err)
	}
	return nil
}

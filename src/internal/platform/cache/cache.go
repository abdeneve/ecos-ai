// Package cache wraps Redis for the three things the rest of the system
// needs from it: idempotency, session state, and the short-lived
// distributed lock used around per-tenant state mutation during consumer
// group rebalances. See design.md decisions 3 and 6.
package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps a Redis connection.
type Client struct {
	rdb *redis.Client
}

// New connects to Redis at addr (e.g. "localhost:6379").
func New(addr string) *Client {
	return &Client{rdb: redis.NewClient(&redis.Options{Addr: addr})}
}

// Close releases the underlying Redis connection pool.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping verifies connectivity to Redis.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

const idempotencyKeyPrefix = "idempotency:"

// SeenBefore records messageID as seen for ttl (24h in production, per
// design.md) and reports whether it had already been recorded. The ingress
// webhook uses this to drop duplicate deliveries from Meta before ever
// publishing to the event stream.
func (c *Client) SeenBefore(ctx context.Context, messageID string, ttl time.Duration) (bool, error) {
	key := idempotencyKeyPrefix + messageID
	ok, err := c.rdb.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("cache: idempotency check for %q: %w", messageID, err)
	}
	// SetNX returns true when the key was newly set (first time seen).
	return !ok, nil
}

const sessionStateKeyPrefix = "session:"

// GetSessionState returns the current state string for tenantID, and
// whether a session exists at all (a tenant with no session yet is
// implicitly NEW).
func (c *Client) GetSessionState(ctx context.Context, tenantID string) (state string, exists bool, err error) {
	val, err := c.rdb.Get(ctx, sessionStateKeyPrefix+tenantID).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("cache: get session state for %q: %w", tenantID, err)
	}
	return val, true, nil
}

// SetSessionState persists the new state string for tenantID.
func (c *Client) SetSessionState(ctx context.Context, tenantID, state string) error {
	if err := c.rdb.Set(ctx, sessionStateKeyPrefix+tenantID, state, 0).Err(); err != nil {
		return fmt.Errorf("cache: set session state for %q: %w", tenantID, err)
	}
	return nil
}

const lockKeyPrefix = "lock:tenant:"

// ErrLockNotHeld is returned by ReleaseLock when the caller's token does
// not match the current lock holder (e.g. the lock already expired and was
// re-acquired by someone else).
var ErrLockNotHeld = errors.New("cache: lock not held by this token")

// AcquireLock attempts to take the per-tenant lock used to guard the
// consumer-group rebalance window (design.md decision 6). It returns a
// random token that must be presented to ReleaseLock, and ok=false if the
// lock is currently held by someone else.
func (c *Client) AcquireLock(ctx context.Context, tenantID string, ttl time.Duration) (token string, ok bool, err error) {
	token, err = randomToken()
	if err != nil {
		return "", false, fmt.Errorf("cache: generate lock token: %w", err)
	}
	acquired, err := c.rdb.SetNX(ctx, lockKeyPrefix+tenantID, token, ttl).Result()
	if err != nil {
		return "", false, fmt.Errorf("cache: acquire lock for tenant %q: %w", tenantID, err)
	}
	if !acquired {
		return "", false, nil
	}
	return token, true, nil
}

// releaseScript deletes the lock key only if it still holds the caller's
// token, so a caller can never release a lock it no longer owns (e.g.
// because it expired and was re-acquired by another worker).
var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
else
	return 0
end
`)

// ReleaseLock releases the per-tenant lock, but only if token still matches
// the current holder.
func (c *Client) ReleaseLock(ctx context.Context, tenantID, token string) error {
	res, err := releaseScript.Run(ctx, c.rdb, []string{lockKeyPrefix + tenantID}, token).Int64()
	if err != nil {
		return fmt.Errorf("cache: release lock for tenant %q: %w", tenantID, err)
	}
	if res == 0 {
		return ErrLockNotHeld
	}
	return nil
}

// Publish publishes message on channel, used to notify the Next.js handoff
// panel of state changes via Redis pub/sub -> WebSocket.
func (c *Client) Publish(ctx context.Context, channel, message string) error {
	if err := c.rdb.Publish(ctx, channel, message).Err(); err != nil {
		return fmt.Errorf("cache: publish to %q: %w", channel, err)
	}
	return nil
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

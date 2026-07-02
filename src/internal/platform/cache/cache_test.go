package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return New(mr.Addr())
}

func TestSeenBefore_FirstTimeThenDuplicate(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	seen, err := c.SeenBefore(ctx, "msg-1", 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen {
		t.Fatal("first occurrence reported as already seen")
	}

	seen, err = c.SeenBefore(ctx, "msg-1", 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !seen {
		t.Fatal("duplicate occurrence not detected")
	}
}

func TestSessionState_RoundTrip(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	_, exists, err := c.GetSessionState(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected no session state for unknown tenant")
	}

	if err := c.SetSessionState(ctx, "tenant-1", "AI_ENGAGED"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, exists, err := c.GetSessionState(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists || state != "AI_ENGAGED" {
		t.Fatalf("got state=%q exists=%v, want AI_ENGAGED true", state, exists)
	}
}

func TestLock_ExclusiveAcquisitionAndRelease(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	token, ok, err := c.AcquireLock(ctx, "tenant-1", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected to acquire uncontended lock")
	}

	_, ok, err = c.AcquireLock(ctx, "tenant-1", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected second acquisition to fail while lock is held")
	}

	if err := c.ReleaseLock(ctx, "tenant-1", token); err != nil {
		t.Fatalf("unexpected error releasing lock: %v", err)
	}

	_, ok, err = c.AcquireLock(ctx, "tenant-1", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected to reacquire lock after release")
	}
}

func TestLock_ReleaseWithWrongTokenFails(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	_, ok, err := c.AcquireLock(ctx, "tenant-1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("setup: unexpected AcquireLock result ok=%v err=%v", ok, err)
	}

	if err := c.ReleaseLock(ctx, "tenant-1", "not-the-real-token"); err != ErrLockNotHeld {
		t.Fatalf("got %v, want ErrLockNotHeld", err)
	}
}

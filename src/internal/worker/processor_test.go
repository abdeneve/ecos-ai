package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	v1 "github.com/abdeneve/ecos-ai/src/contracts/events/v1"
	"github.com/abdeneve/ecos-ai/src/internal/platform/cache"
	"github.com/abdeneve/ecos-ai/src/internal/statemachine"
)

type historyRecord struct {
	tenantID, messageID string
}

type fakeHistory struct {
	mu          sync.Mutex
	messages    []historyRecord
	transitions []statemachine.Transition
}

func (f *fakeHistory) InsertMessage(_ context.Context, tenantID, messageID, _ string, _ time.Time, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, historyRecord{tenantID: tenantID, messageID: messageID})
	return nil
}

func (f *fakeHistory) InsertTransition(_ context.Context, _ string, tr statemachine.Transition) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transitions = append(f.transitions, tr)
	return nil
}

func (f *fakeHistory) messageCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

func (f *fakeHistory) transitionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.transitions)
}

func newTestProcessor(t *testing.T) (*Processor, *cache.Client, *fakeHistory) {
	t.Helper()
	mr := miniredis.RunT(t)
	sessions := cache.New(mr.Addr())
	history := &fakeHistory{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := NewProcessor(sessions, history, 8, time.Minute, logger)
	return p, sessions, history
}

func encodedEvent(t *testing.T, tenantID, messageID string) []byte {
	t.Helper()
	e := v1.ConversationEvent{
		TenantID:   tenantID,
		MessageID:  messageID,
		OccurredAt: time.Now().UTC(),
		Payload:    json.RawMessage(`{"body":"hi"}`),
		TraceID:    "trace-1",
	}
	raw, err := e.Encode()
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	return raw
}

func TestHandleMessage_FirstMessageTransitionsToAIEngaged(t *testing.T) {
	p, sessions, history := newTestProcessor(t)
	ctx := context.Background()

	if err := p.HandleMessage(ctx, encodedEvent(t, "tenant-1", "msg-1")); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	state, exists, err := sessions.GetSessionState(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("GetSessionState: %v", err)
	}
	if !exists || state != string(statemachine.AIEngaged) {
		t.Fatalf("state = %q exists=%v, want AI_ENGAGED", state, exists)
	}
	if history.messageCount() != 1 {
		t.Fatalf("messageCount = %d, want 1", history.messageCount())
	}
	if history.transitionCount() != 1 {
		t.Fatalf("transitionCount = %d, want 1 (NEW -> AI_ENGAGED)", history.transitionCount())
	}
}

func TestHandleMessage_SecondMessageDoesNotRetransition(t *testing.T) {
	p, _, history := newTestProcessor(t)
	ctx := context.Background()

	if err := p.HandleMessage(ctx, encodedEvent(t, "tenant-1", "msg-1")); err != nil {
		t.Fatalf("first HandleMessage: %v", err)
	}
	if err := p.HandleMessage(ctx, encodedEvent(t, "tenant-1", "msg-2")); err != nil {
		t.Fatalf("second HandleMessage: %v", err)
	}

	if history.messageCount() != 2 {
		t.Fatalf("messageCount = %d, want 2", history.messageCount())
	}
	if history.transitionCount() != 1 {
		t.Fatalf("transitionCount = %d, want 1 (only the initial NEW -> AI_ENGAGED)", history.transitionCount())
	}
}

func TestHandleMessage_BypassesWhileInHandoff(t *testing.T) {
	p, sessions, history := newTestProcessor(t)
	ctx := context.Background()

	if err := sessions.SetSessionState(ctx, "tenant-1", string(statemachine.Handoff)); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	if err := p.HandleMessage(ctx, encodedEvent(t, "tenant-1", "msg-1")); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	state, _, err := sessions.GetSessionState(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("GetSessionState: %v", err)
	}
	if state != string(statemachine.Handoff) {
		t.Fatalf("state = %q, want unchanged HANDOFF (worker must not call the LLM or transition on its own)", state)
	}
	if history.messageCount() != 1 {
		t.Fatalf("messageCount = %d, want 1 (still routed/persisted)", history.messageCount())
	}
	if history.transitionCount() != 0 {
		t.Fatalf("transitionCount = %d, want 0 (no transition while bypassing)", history.transitionCount())
	}
}

func TestTransition_HumanReturnsHandoffToAIEngaged(t *testing.T) {
	p, sessions, history := newTestProcessor(t)
	ctx := context.Background()

	if err := sessions.SetSessionState(ctx, "tenant-1", string(statemachine.Handoff)); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	tr, err := p.Transition(ctx, "tenant-1", statemachine.AIEngaged, statemachine.HumanInitiator("op-1"))
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if tr.By != statemachine.Initiator("human:op-1") {
		t.Fatalf("By = %q, want human:op-1", tr.By)
	}

	state, _, err := sessions.GetSessionState(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("GetSessionState: %v", err)
	}
	if state != string(statemachine.AIEngaged) {
		t.Fatalf("state = %q, want AI_ENGAGED", state)
	}
	if history.transitionCount() != 1 {
		t.Fatalf("transitionCount = %d, want 1", history.transitionCount())
	}
}

func TestTransition_InvalidTransitionLeavesStateUnchanged(t *testing.T) {
	p, sessions, history := newTestProcessor(t)
	ctx := context.Background()

	if err := sessions.SetSessionState(ctx, "tenant-1", string(statemachine.Closed)); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	var invalidErr *statemachine.InvalidTransitionError
	_, err := p.Transition(ctx, "tenant-1", statemachine.AIEngaged, statemachine.SystemInitiator)
	if !errors.As(err, &invalidErr) {
		t.Fatalf("Transition from CLOSED: got %v, want *statemachine.InvalidTransitionError", err)
	}

	state, _, err := sessions.GetSessionState(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("GetSessionState: %v", err)
	}
	if state != string(statemachine.Closed) {
		t.Fatalf("state = %q, want unchanged CLOSED after rejected transition", state)
	}
	if history.transitionCount() != 0 {
		t.Fatalf("transitionCount = %d, want 0 for a rejected transition", history.transitionCount())
	}
}

// TestHandleMessage_ConcurrentAccessSameTenantIsSerialized simulates the
// rebalance-overlap scenario from tasks.md 5.10: two goroutines racing to
// process events for the same tenant at once (as could happen briefly
// across two worker instances during a consumer group rebalance). The
// per-tenant Redis lock must ensure exactly one of them proceeds at a time
// so the session state is never corrupted by an interleaved read-modify-write.
func TestHandleMessage_ConcurrentAccessSameTenantIsSerialized(t *testing.T) {
	p, sessions, history := newTestProcessor(t)
	ctx := context.Background()

	const n = 20
	events := make([][]byte, n)
	for i := 0; i < n; i++ {
		events[i] = encodedEvent(t, "tenant-1", "msg-"+strconv.Itoa(i))
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = p.HandleMessage(ctx, events[i])
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrTenantLocked) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded == 0 {
		t.Fatal("expected at least one goroutine to succeed")
	}

	// Regardless of how many individual acquisitions were contended, the
	// state machine must never have been corrupted: exactly one NEW ->
	// AI_ENGAGED transition should be recorded, and the final state must
	// be AI_ENGAGED.
	if history.transitionCount() != 1 {
		t.Fatalf("transitionCount = %d, want exactly 1 despite concurrent access", history.transitionCount())
	}
	state, _, err := sessions.GetSessionState(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("GetSessionState: %v", err)
	}
	if state != string(statemachine.AIEngaged) {
		t.Fatalf("state = %q, want AI_ENGAGED", state)
	}
	t.Logf("%d/%d concurrent HandleMessage calls acquired the lock and proceeded", succeeded, n)
}

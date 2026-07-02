// Package worker applies incoming conversation events to the session state
// machine and persists the result — the "headless" core described in
// docs/flows/routing-engine.md, minus the LLM call itself (out of scope
// for this change; see design.md Non-Goals).
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	v1 "github.com/abdeneve/ecos-ai/src/contracts/events/v1"
	"github.com/abdeneve/ecos-ai/src/internal/statemachine"
)

// SessionStore is the subset of cache.Client the processor depends on for
// live session state, the rebalance-safety lock, and UI pub/sub.
type SessionStore interface {
	GetSessionState(ctx context.Context, tenantID string) (state string, exists bool, err error)
	SetSessionState(ctx context.Context, tenantID, state string) error
	AcquireLock(ctx context.Context, tenantID string, ttl time.Duration) (token string, ok bool, err error)
	ReleaseLock(ctx context.Context, tenantID, token string) error
	Publish(ctx context.Context, channel, message string) error
}

// HistoryStore is the subset of storage.Client the processor depends on for
// the immutable message and transition history.
type HistoryStore interface {
	InsertMessage(ctx context.Context, tenantID, messageID, traceID string, occurredAt time.Time, payload []byte) error
	InsertTransition(ctx context.Context, tenantID string, tr statemachine.Transition) error
}

// ErrTenantLocked is returned when a tenant's session is currently locked by
// another worker instance (mid-rebalance overlap window).
var ErrTenantLocked = fmt.Errorf("worker: tenant session is locked by another worker")

// Processor wires consumed events to state machine transitions and
// persistence.
type Processor struct {
	sessions SessionStore
	history  HistoryStore
	sem      chan struct{}
	lockTTL  time.Duration
	logger   *slog.Logger
}

// NewProcessor builds a Processor. maxConcurrentDownstream bounds how many
// messages may be concurrently hitting Redis/Scylla at once, across all
// partitions this worker instance owns (design.md, decision 7).
func NewProcessor(sessions SessionStore, history HistoryStore, maxConcurrentDownstream int, lockTTL time.Duration, logger *slog.Logger) *Processor {
	return &Processor{
		sessions: sessions,
		history:  history,
		sem:      make(chan struct{}, maxConcurrentDownstream),
		lockTTL:  lockTTL,
		logger:   logger,
	}
}

// HandleMessage decodes and processes one event: it starts a session on
// first contact, bypasses any further logic while a session is in HANDOFF
// (the worker only persists/routes — it never calls the LLM for a
// handed-off session), and records the message in history. Its signature
// matches eventbus.Handler so it can be passed directly to
// eventbus.NewConsumer.
func (p *Processor) HandleMessage(ctx context.Context, value []byte) error {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.sem }()

	event, err := v1.Decode(value)
	if err != nil {
		return fmt.Errorf("worker: decode event: %w", err)
	}

	return p.withTenantLock(ctx, event.TenantID, func(ctx context.Context) error {
		current, err := p.currentState(ctx, event.TenantID)
		if err != nil {
			return err
		}

		if current == statemachine.New {
			tr, err := statemachine.Apply(statemachine.New, statemachine.AIEngaged, statemachine.SystemInitiator)
			if err != nil {
				return fmt.Errorf("worker: apply initial transition: %w", err)
			}
			if err := p.persistTransition(ctx, event.TenantID, tr); err != nil {
				return err
			}
			current = statemachine.AIEngaged
		}

		if current == statemachine.Handoff {
			p.logger.Info("worker: handoff active, routing without invoking the LLM",
				"tenant_id", event.TenantID, "message_id", event.MessageID)
		}

		if err := p.history.InsertMessage(ctx, event.TenantID, event.MessageID, event.TraceID, event.OccurredAt, event.Payload); err != nil {
			return fmt.Errorf("worker: persist message: %w", err)
		}
		return nil
	})
}

// Transition applies an explicit, externally-requested transition — e.g. a
// human operator returning a conversation from HANDOFF to AI_ENGAGED via
// the (future) handoff panel. It is the seam later changes call into
// rather than mutating session state directly.
func (p *Processor) Transition(ctx context.Context, tenantID string, target statemachine.State, by statemachine.Initiator) (statemachine.Transition, error) {
	var tr statemachine.Transition
	err := p.withTenantLock(ctx, tenantID, func(ctx context.Context) error {
		current, err := p.currentState(ctx, tenantID)
		if err != nil {
			return err
		}
		tr, err = statemachine.Apply(current, target, by)
		if err != nil {
			return err
		}
		return p.persistTransition(ctx, tenantID, tr)
	})
	return tr, err
}

func (p *Processor) currentState(ctx context.Context, tenantID string) (statemachine.State, error) {
	state, exists, err := p.sessions.GetSessionState(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("worker: get session state for tenant %q: %w", tenantID, err)
	}
	if !exists {
		return statemachine.New, nil
	}
	return statemachine.State(state), nil
}

func (p *Processor) persistTransition(ctx context.Context, tenantID string, tr statemachine.Transition) error {
	if err := p.sessions.SetSessionState(ctx, tenantID, string(tr.To)); err != nil {
		return fmt.Errorf("worker: set session state for tenant %q: %w", tenantID, err)
	}
	if err := p.history.InsertTransition(ctx, tenantID, tr); err != nil {
		return fmt.Errorf("worker: persist transition for tenant %q: %w", tenantID, err)
	}
	// Best-effort notification for the (future) handoff panel; a missed
	// pub/sub message does not affect correctness of session state.
	channel := "tenant:" + tenantID
	msg := fmt.Sprintf(`{"type":"state_changed","from":%q,"to":%q,"by":%q}`, tr.From, tr.To, tr.By)
	if err := p.sessions.Publish(ctx, channel, msg); err != nil {
		p.logger.Error("worker: publish state change failed", "tenant_id", tenantID, "error", err)
	}
	return nil
}

func (p *Processor) withTenantLock(ctx context.Context, tenantID string, fn func(ctx context.Context) error) error {
	token, ok, err := p.sessions.AcquireLock(ctx, tenantID, p.lockTTL)
	if err != nil {
		return fmt.Errorf("worker: acquire lock for tenant %q: %w", tenantID, err)
	}
	if !ok {
		return ErrTenantLocked
	}
	defer func() {
		if err := p.sessions.ReleaseLock(ctx, tenantID, token); err != nil {
			p.logger.Error("worker: release lock failed", "tenant_id", tenantID, "error", err)
		}
	}()
	return fn(ctx)
}

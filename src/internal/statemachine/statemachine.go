// Package statemachine models the conversation session lifecycle as a pure,
// I/O-free function of current state and requested transition. It has no
// dependency on Redis, Kafka, or any other transport or storage mechanism,
// so it is testable in isolation and is the single place transition rules
// are enforced.
package statemachine

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// State is one of the four states a conversation session can be in.
type State string

const (
	New        State = "NEW"
	AIEngaged  State = "AI_ENGAGED"
	Handoff    State = "HANDOFF"
	Closed     State = "CLOSED"
)

func (s State) valid() bool {
	switch s {
	case New, AIEngaged, Handoff, Closed:
		return true
	default:
		return false
	}
}

// Initiator identifies who or what requested a transition. Every transition
// must carry one, so a human returning a conversation from HANDOFF back to
// AI_ENGAGED is always attributable and auditable.
type Initiator string

// SystemInitiator is used for transitions triggered by the agent worker
// itself (e.g. NEW -> AI_ENGAGED on first message, or an automatic handoff
// on repeated LLM failure).
const SystemInitiator Initiator = "system"

const humanPrefix = "human:"

// HumanInitiator builds the initiator value for a transition requested by a
// human operator identified by operatorID.
func HumanInitiator(operatorID string) Initiator {
	return Initiator(humanPrefix + operatorID)
}

func (i Initiator) valid() bool {
	if i == SystemInitiator {
		return true
	}
	return strings.HasPrefix(string(i), humanPrefix) && len(i) > len(humanPrefix)
}

// ErrMissingInitiator is returned when a transition is requested without an
// initiator.
var ErrMissingInitiator = errors.New("statemachine: transition requires an initiator")

// ErrInvalidInitiator is returned when the initiator is neither
// SystemInitiator nor a well-formed HumanInitiator.
var ErrInvalidInitiator = errors.New("statemachine: initiator must be \"system\" or \"human:<operator_id>\"")

// InvalidTransitionError is returned when the requested (from, to) pair is
// not in the allowed transition table.
type InvalidTransitionError struct {
	From State
	To   State
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("statemachine: transition %s -> %s is not allowed", e.From, e.To)
}

// allowed enumerates every valid (from, to) pair. NEW -> AI_ENGAGED starts a
// session; AI_ENGAGED and HANDOFF both resolve to CLOSED; HANDOFF is
// reversible back to AI_ENGAGED via an explicit (human) initiator. CLOSED is
// terminal.
var allowed = map[State]map[State]bool{
	New:       {AIEngaged: true},
	AIEngaged: {Handoff: true, Closed: true},
	Handoff:   {AIEngaged: true, Closed: true},
	Closed:    {},
}

// Transition is the record of a single, applied state change.
type Transition struct {
	From State
	To   State
	By   Initiator
	At   time.Time
}

// Apply validates and applies a transition from the current state to
// target, attributed to by. It is a pure function: given the same inputs it
// always returns the same result, with no side effects.
func Apply(current, target State, by Initiator) (Transition, error) {
	if !current.valid() {
		return Transition{}, fmt.Errorf("statemachine: invalid current state %q", current)
	}
	if !target.valid() {
		return Transition{}, fmt.Errorf("statemachine: invalid target state %q", target)
	}
	if by == "" {
		return Transition{}, ErrMissingInitiator
	}
	if !by.valid() {
		return Transition{}, ErrInvalidInitiator
	}
	if !allowed[current][target] {
		return Transition{}, &InvalidTransitionError{From: current, To: target}
	}
	return Transition{From: current, To: target, By: by, At: time.Now().UTC()}, nil
}

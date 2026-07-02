package statemachine

import (
	"errors"
	"testing"
)

var allStates = []State{New, AIEngaged, Handoff, Closed}

func TestApply_AllValidTransitions(t *testing.T) {
	cases := []struct {
		from, to State
	}{
		{New, AIEngaged},
		{AIEngaged, Handoff},
		{AIEngaged, Closed},
		{Handoff, AIEngaged},
		{Handoff, Closed},
	}

	for _, c := range cases {
		tr, err := Apply(c.from, c.to, SystemInitiator)
		if err != nil {
			t.Errorf("Apply(%s, %s, system) unexpected error: %v", c.from, c.to, err)
			continue
		}
		if tr.From != c.from || tr.To != c.to || tr.By != SystemInitiator {
			t.Errorf("Apply(%s, %s) = %+v, want From=%s To=%s By=system", c.from, c.to, tr, c.from, c.to)
		}
		if tr.At.IsZero() {
			t.Errorf("Apply(%s, %s) returned zero timestamp", c.from, c.to)
		}
	}
}

func TestApply_AllInvalidTransitionsRejected(t *testing.T) {
	valid := map[State]map[State]bool{
		New:       {AIEngaged: true},
		AIEngaged: {Handoff: true, Closed: true},
		Handoff:   {AIEngaged: true, Closed: true},
		Closed:    {},
	}

	for _, from := range allStates {
		for _, to := range allStates {
			if valid[from][to] {
				continue // covered by TestApply_AllValidTransitions
			}
			_, err := Apply(from, to, SystemInitiator)
			if err == nil {
				t.Errorf("Apply(%s, %s, system) expected error, got nil", from, to)
				continue
			}
			var invalidErr *InvalidTransitionError
			if !errors.As(err, &invalidErr) {
				t.Errorf("Apply(%s, %s) expected *InvalidTransitionError, got %T: %v", from, to, err, err)
			}
		}
	}
}

func TestApply_HumanReturnsHandoffToAIEngaged(t *testing.T) {
	tr, err := Apply(Handoff, AIEngaged, HumanInitiator("op-42"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.By != Initiator("human:op-42") {
		t.Errorf("By = %q, want %q", tr.By, "human:op-42")
	}
}

func TestApply_MissingInitiatorRejected(t *testing.T) {
	_, err := Apply(New, AIEngaged, "")
	if !errors.Is(err, ErrMissingInitiator) {
		t.Errorf("Apply with empty initiator: got %v, want ErrMissingInitiator", err)
	}
}

func TestApply_MalformedInitiatorRejected(t *testing.T) {
	cases := []Initiator{"bot", "human:", "SYSTEM", " system"}
	for _, by := range cases {
		_, err := Apply(New, AIEngaged, by)
		if !errors.Is(err, ErrInvalidInitiator) {
			t.Errorf("Apply with initiator %q: got %v, want ErrInvalidInitiator", by, err)
		}
	}
}

func TestApply_ClosedIsTerminal(t *testing.T) {
	for _, to := range allStates {
		_, err := Apply(Closed, to, SystemInitiator)
		if err == nil {
			t.Errorf("Apply(CLOSED, %s) expected error, got nil", to)
		}
	}
}

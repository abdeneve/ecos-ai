# session-state-machine Specification

## Purpose

Defines the finite state machine that governs the lifecycle of a conversation session — including valid states, allowed transitions, attribution, and the rule that the LLM is bypassed while a session is in human handoff.

## Requirements

### Requirement: Defined Session States
The system SHALL model every conversation session as being in exactly one of the following states at any time: `NEW`, `AI_ENGAGED`, `HANDOFF`, or `CLOSED`.

#### Scenario: New session starts in NEW
- **WHEN** a session is created for a tenant's conversation for the first time
- **THEN** its state is `NEW`

### Requirement: Valid Transition Enforcement
The state machine SHALL only allow the following transitions, and SHALL reject any transition attempt not in this list: `NEW → AI_ENGAGED`, `AI_ENGAGED → HANDOFF`, `AI_ENGAGED → CLOSED`, `HANDOFF → AI_ENGAGED`, `HANDOFF → CLOSED`.

#### Scenario: Valid transition accepted
- **WHEN** a session in state `AI_ENGAGED` receives a transition event to `HANDOFF`
- **THEN** the state machine applies the transition and the session's new state is `HANDOFF`

#### Scenario: Invalid transition rejected
- **WHEN** a session in state `CLOSED` receives any transition event
- **THEN** the state machine rejects the transition and returns an error without changing the session's state

#### Scenario: Invalid transition rejected from NEW
- **WHEN** a session in state `NEW` receives a transition event directly to `HANDOFF` or `CLOSED`
- **THEN** the state machine rejects the transition and returns an error without changing the session's state

### Requirement: Reversible Handoff with Attribution
The state machine SHALL allow a session in `HANDOFF` to transition back to `AI_ENGAGED`, and SHALL require every transition to record who or what initiated it (`system` or a human operator identifier).

#### Scenario: Human returns conversation to AI
- **WHEN** a human operator explicitly initiates a transition from `HANDOFF` to `AI_ENGAGED` for a session
- **THEN** the state machine applies the transition and records the operator's identifier as the initiator of that transition

#### Scenario: Transition without attribution rejected
- **WHEN** a transition is requested without an initiator (`system` or operator identifier)
- **THEN** the state machine rejects the transition and returns an error

### Requirement: LLM Bypass While in Handoff
The agent worker SHALL NOT invoke the LLM provider for any session currently in the `HANDOFF` state, and SHALL act only as a message router while a session remains in that state.

#### Scenario: Message received during handoff
- **WHEN** the agent worker processes an inbound message for a session in state `HANDOFF`
- **THEN** the worker persists and routes the message without calling the LLM provider

### Requirement: Pure, I/O-Free Transition Logic
The state machine's transition logic SHALL be implemented as a pure function of current state and incoming event, with no direct dependency on Redis, Kafka, or any other I/O, so that it is testable in isolation.

#### Scenario: Transition evaluated without external dependencies
- **WHEN** the transition function is called with a current state and an event in a unit test with no network or storage access
- **THEN** it returns the resulting state (or an error) deterministically

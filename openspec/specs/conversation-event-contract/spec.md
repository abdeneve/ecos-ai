# conversation-event-contract Specification

## Purpose

Defines the versioned event schema used to carry conversation data between the webhook ingress, the event stream, and downstream consumers such as the agent worker, ensuring producers and consumers stay compatible as the contract evolves.

## Requirements

### Requirement: Versioned Event Schema
The conversation event contract SHALL be defined as a versioned schema (starting at `v1`) stored in `src/contracts/events/v1`, independent of any single service's internal data structures.

#### Scenario: Producer and consumer reference the same schema version
- **WHEN** the ingress publishes an event and the worker consumes it
- **THEN** both validate the event against the same `src/contracts/events/v1` schema definition

### Requirement: Required Event Fields
Every conversation event SHALL include at minimum: `tenant_id`, `message_id`, `occurred_at`, `payload`, and `trace_id`.

#### Scenario: Event missing a required field
- **WHEN** an event is constructed without one of the required fields
- **THEN** schema validation fails and the event is not published

### Requirement: Additive Schema Evolution
Changes to the event contract SHALL be introduced as new additive versions (e.g., `v2`) rather than breaking changes to an existing version, so that consumers running an older contract version continue to function.

#### Scenario: New optional field added
- **WHEN** a new field is needed by a future capability
- **THEN** it is added as an optional field in a new schema version rather than modifying `v1` in place

### Requirement: Trace Context Propagation
Every conversation event SHALL carry a `trace_id` that propagates distributed tracing context from the ingress webhook through to the agent worker and any downstream persistence.

#### Scenario: Trace continuity across the event stream
- **WHEN** the ingress publishes an event with a given `trace_id`
- **THEN** the worker's processing of that event is recorded under the same `trace_id`

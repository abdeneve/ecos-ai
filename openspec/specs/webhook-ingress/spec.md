# webhook-ingress Specification

## Purpose

Defines the behavior of the inbound Meta WhatsApp webhook ingress: signature verification, idempotent ingestion, low-latency responses, backpressure handling, and tenant-partitioned publishing to the event stream.

## Requirements

### Requirement: Signature Verification
The ingress webhook SHALL verify the HMAC signature of every inbound Meta WhatsApp payload before further processing, and SHALL reject requests with a missing or invalid signature.

#### Scenario: Valid signature accepted
- **WHEN** a webhook request arrives with a valid HMAC signature matching the configured app secret
- **THEN** the ingress proceeds to idempotency checking and event publishing

#### Scenario: Invalid signature rejected
- **WHEN** a webhook request arrives with a missing or invalid HMAC signature
- **THEN** the ingress responds with an error status and does not publish any event

### Requirement: Idempotent Ingestion
The ingress webhook SHALL deduplicate inbound messages by `message_id` using a 24-hour idempotency window, and SHALL NOT publish duplicate events for a message already seen within that window.

#### Scenario: First occurrence of a message
- **WHEN** a message with a given `message_id` is received for the first time within the idempotency window
- **THEN** the ingress marks the `message_id` as seen and publishes the corresponding event

#### Scenario: Duplicate message within window
- **WHEN** a message with a `message_id` already marked as seen is received again within 24 hours
- **THEN** the ingress drops the event without republishing and still responds 200 OK to the caller

### Requirement: Low-Latency Response
The ingress webhook SHALL respond to the calling party (Meta) without waiting for downstream event-stream broker acknowledgment beyond what is required to guarantee the event was accepted for delivery.

#### Scenario: Normal load
- **WHEN** the event stream broker and idempotency store are healthy
- **THEN** the ingress responds to the webhook call within its configured latency budget

### Requirement: Backpressure on Publish Failure
The ingress webhook SHALL reject inbound requests with a retryable error status when the event stream producer's outstanding delivery buffer exceeds its configured limit, rather than accepting unbounded in-memory backlog.

#### Scenario: Producer buffer saturated
- **WHEN** the event stream broker is slow or unreachable and the producer's pending-delivery buffer reaches its configured limit
- **THEN** the ingress responds with a retryable error status (e.g. 429/503) instead of accepting and buffering the request indefinitely

### Requirement: Tenant-Partitioned Publishing
The ingress webhook SHALL publish each event to the event stream using the `tenant_id` as the partition key, so that all events for a given tenant are strictly ordered relative to one another.

#### Scenario: Multiple messages from the same tenant
- **WHEN** two or more messages for the same `tenant_id` are ingested in sequence
- **THEN** both are published to the same partition, preserving their relative order for downstream consumers

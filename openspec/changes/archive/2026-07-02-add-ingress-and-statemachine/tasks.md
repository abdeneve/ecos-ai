## 1. Monorepo Scaffolding

- [x] 1.1 Initialize Go module at `src/go.mod` with base layout under `src/` (`cmd/ingress`, `cmd/worker`, `internal/`, `contracts/`, `migrations/`)
- [x] 1.2 Add `src/internal/telemetry` with structured logging (slog) and OTel tracing/metrics setup shared by both binaries

## 2. Conversation Event Contract

- [x] 2.1 Define `src/contracts/events/v1` schema with required fields: `tenant_id`, `message_id`, `occurred_at`, `payload`, `trace_id`
- [x] 2.2 Generate/wire Go types (or validators) from the schema for use by both `ingress` and `worker`
- [x] 2.3 Document the additive-versioning rule (how a future `v2` would be introduced) alongside the schema

## 3. Session State Machine

- [x] 3.1 Implement `src/internal/statemachine` with states `NEW`, `AI_ENGAGED`, `HANDOFF`, `CLOSED` and the transition table from `design.md`
- [x] 3.2 Enforce transition validation (reject any pair not in the allowed list) with typed errors
- [x] 3.3 Add `transitioned_by` (`system` | `human:<operator_id>`) as a required field on every transition call
- [x] 3.4 Unit tests covering every valid transition, every rejected invalid transition, and the missing-attribution rejection case

## 4. Webhook Ingress

- [x] 4.1 Implement `src/internal/ingress` HTTP handler with Meta HMAC signature verification
- [x] 4.2 Implement idempotency check via Redis `SETNX <message_id>` with 24h TTL (`src/internal/platform/cache`)
- [x] 4.3 Implement Redpanda producer wrapper partitioned by `tenant_id` (`src/internal/platform/eventbus`), publishing events conforming to `src/contracts/events/v1`
- [x] 4.4 Implement producer backpressure: reject with retryable error status when the pending-delivery buffer exceeds its configured limit
- [x] 4.5 Wire `src/cmd/ingress/main.go`: handler → idempotency → publish → response, propagating `trace_id`
- [x] 4.6 Integration test: duplicate `message_id` within 24h is dropped and not republished (hermetic, via miniredis + fake publisher — see `src/internal/ingress/handler_test.go`; full-infra e2e covered by task 6.1)
- [x] 4.7 Latency test: measure response time under normal load against the sub-5ms target (hermetic component test against in-memory dependencies; real Redpanda/Redis latency validated in task 6.1)

## 5. Agent Worker (state transitions only, no LLM)

- [x] 5.1 Implement `src/internal/platform/storage` (ScyllaDB client) with session/history tables per `src/migrations/`
- [x] 5.2 Implement partitioned consumer loop in `src/internal/worker`: one goroutine per assigned partition, sequential processing within a partition (implemented in `src/internal/platform/eventbus/consumer.go`, driven by franz-go's partition assignment callbacks)
- [x] 5.3 Implement the Redis-based distributed lock (`SET NX`, short TTL) around per-tenant read-modify-write during state transitions
- [x] 5.4 Wire consumed events to `src/internal/statemachine` transitions and persist resulting state to Redis + history to ScyllaDB
- [x] 5.5 Implement LLM bypass check: while a session is `HANDOFF`, the worker only routes/persists messages, no LLM call (LLM call itself is out of scope; this task only wires the bypass condition)
- [x] 5.6 Implement bounded semaphore limiting concurrent downstream (Redis/Scylla) calls across partitions
- [x] 5.7 Implement graceful shutdown: stop consuming, drain in-flight partition goroutines, commit offsets, exit (implemented via context cancellation + franz-go's partition-revoke drain in `onRevokedOrLost`, rather than a separate `errgroup` — see note below)
- [x] 5.8 Wire `src/cmd/worker/main.go`
- [x] 5.9 Integration test: two events for the same tenant are processed in order and produce the expected state sequence (hermetic, via miniredis + fake history store — see `src/internal/worker/processor_test.go`)
- [x] 5.10 Integration test: simulated rebalance overlap does not corrupt session state (lock prevents double-processing) (hermetic concurrency test against a real Redis-protocol lock via miniredis, run with `-race`)

## 6. Verification

- [x] 6.1 End-to-end test: webhook POST → ingress → Redpanda → worker → state transition persisted, using `docker-compose.yml` infra (manually run against live containers; found and fixed a real bug — see note below)
- [x] 6.2 Verify `trace_id` continuity from ingress through worker in traces/logs (confirmed: ingress span TraceID matched the `trace_id` column persisted in `message_history`)
- [x] 6.3 Update `docs/flows/routing-engine.md` and `docs/flows/whatsapp-handoff.md` if implementation details diverge from the original sequence diagrams (reviewed both; no divergence found — both still accurately describe the target architecture, of which this change implements the ingestion/state-machine subset only, LLM/handoff-panel steps intentionally not yet implemented per design.md Non-Goals)

**Bug found and fixed during 6.1**: `eventbus.Producer.Produce` passed the caller's `context.Context` straight into `kgo.Client.Produce`. Since the caller is the HTTP handler and its request context is canceled the instant `ServeHTTP` returns, the async delivery — which by design happens *after* the response is sent — was being canceled before it ever reached Redpanda (observed as `"context canceled"` delivery errors, with events never arriving at the worker). Fixed by detaching the send from the caller's cancellation via `context.WithoutCancel` in `src/internal/platform/eventbus/producer.go`, which preserves any context values (e.g. trace propagation) without inheriting the caller's lifecycle. Verified the fix by re-running the full webhook → Redpanda → worker → Redis/ScyllaDB path against live `docker-compose.yml` infra.

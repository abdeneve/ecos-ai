## Context

ecos-ai is documented (C4 model, mermaid sequence flows, business strategy) but has no code yet (`src/` is empty). All physical code for this change lives under `src/` — the repository root stays reserved for docs, infra config, and `openspec/`. This is the first implementation slice: the webhook ingress and the session state machine that everything else — the agentic LLM loop, MCP skill calls, the Next.js handoff panel — will be built on top of. The existing `docker-compose.yml` already provisions Redpanda (Kafka-compatible), Redis, and ScyllaDB, so this design targets that infra as given.

## Goals / Non-Goals

**Goals:**
- Prove the ingestion path end-to-end: Meta webhook → idempotency check → event stream → ordered per-tenant consumption → state transition → persistence.
- Establish a versioned event contract between producer and consumer so they can evolve independently.
- Model the conversation session lifecycle as a pure, testable state machine, decoupled from Redis/Kafka/LLM concerns.
- Establish the monorepo layout under `src/` (`src/cmd/`, `src/internal/`, `src/contracts/`) that later slices extend rather than restructure.

**Non-Goals:**
- No LLM integration (`src/internal/llm`) — the worker in this change applies state transitions but does not call any LLM provider.
- No MCP skill registry (`src/internal/mcp`) — deferred to a follow-up change.
- No Next.js handoff panel (`src/apps/web`) — the worker publishes state-change events to Redis pub/sub, but no UI consumes them yet.
- No multi-region or multi-cluster concerns — single Redpanda/Redis/Scylla cluster as in `docker-compose.yml`.

## Decisions

### 1. Monorepo layout: single Go module rooted at `src/`, `cmd/` + `internal/` beneath it
Rejected a `go.work` multi-module layout for now: `ingress` and `worker` share nearly all of `internal/` (state machine, platform clients, telemetry), and there's no current need to version or release those independently. All physical code lives under `src/`, keeping the repository root reserved for `docs/`, `openspec/`, and infra config. Structure:

```
src/
  go.mod
  cmd/{ingress,worker}/main.go
  internal/
    ingress/            # HTTP handler, HMAC verification, idempotency
    worker/              # partitioned consumer loop, applies FSM transitions
    statemachine/         # pure FSM, no I/O
    platform/{eventbus,cache,storage}/   # Redpanda, Redis, Scylla clients
    telemetry/
  contracts/events/v1/
  migrations/
```
Revisit `go.work` only if a package needs independent versioning (e.g., open-sourcing `contracts/`).

### 2. Event contract: versioned JSON Schema in `src/contracts/events/v1`, not a shared Go struct
A shared Go struct between `ingress` and `worker` would let the two drift silently since the compiler enforces nothing across process boundaries. A versioned schema file is the source of truth; both services generate/validate against it. Minimum fields: `tenant_id`, `message_id`, `occurred_at`, `payload`, `trace_id` (for OTel propagation across the Kafka boundary). Future schema changes are additive (`v1` → `v2`) rather than breaking in place.

### 3. Idempotency: Redis `SETNX message_id` with 24h TTL, checked in ingress before publish
Matches the existing `routing-engine.md` flow doc. Rejected checking idempotency in the worker instead: the ingress must be able to drop duplicates without ever touching Kafka, keeping the sub-5ms response budget intact and avoiding duplicate events in the stream entirely (cheaper than deduplicating downstream).

### 4. Partitioning: Redpanda partition key = `tenant_id`
Guarantees ordering of a tenant's messages within a partition, which the state machine depends on (transitions must apply in order). Trade-off: a very high-volume tenant is bounded by single-partition throughput; acceptable for this slice, revisit with sub-partitioning (e.g., `tenant_id + conversation_id`) if a tenant's volume becomes a bottleneck.

### 5. State machine: `NEW → AI_ENGAGED ⇄ HANDOFF → CLOSED`, `HANDOFF` is reversible
Resolved the open question from the proposal: `HANDOFF` can transition back to `AI_ENGAGED` via an explicit human action (not automatically, and not triggered by AI). This requires:
- A transition table enforced entirely inside `src/internal/statemachine` (pure function: `(currentState, event) → (newState, error)`), so invalid transitions (e.g., `CLOSED → AI_ENGAGED`) are rejected before touching Redis.
- A `transitioned_by` field (`system` | `human:<operator_id>`) recorded with every transition, so returning a conversation from `HANDOFF` to `AI_ENGAGED` is always attributable and auditable — this is what prevents accidental "ping-pong" from being silent.
- The worker still bypasses the LLM call whenever it observes `HANDOFF` state, per the existing `whatsapp-handoff.md` flow; it re-engages the LLM only after observing the transition back to `AI_ENGAGED`.

```
        ┌─────┐  first message   ┌─────────────┐
        │ NEW │──────────────────▶ AI_ENGAGED  │
        └─────┘                  └──────┬──────┘
                     qualification /     │      ▲
                     out-of-scope /      │      │ human returns
                     LLM failure (N)     ▼      │ conversation
                              ┌───────────────┐  │
                              │    HANDOFF    │──┘
                              └───────┬───────┘
                     AI_ENGAGED ──────┤ resolved by human/bot
                                      ▼
                                 ┌────────┐
                                 │ CLOSED │
                                 └────────┘
```

### 6. Distributed lock during rebalance: Redis `SET NX` short TTL, keyed by `tenant_id`
Kafka consumer-group rebalances create a brief window where two workers may believe they own the same partition. A short-TTL lock around "read state → apply transition → write state" for a given `tenant_id` prevents two workers from racing on the same session. This is a safety net, not the primary ordering mechanism — partitioning by `tenant_id` is.

### 7. Worker concurrency: one goroutine per assigned partition, bounded semaphore for downstream calls
Each partition is processed sequentially by its own goroutine (preserves per-tenant ordering). A buffered-channel semaphore bounds concurrent Redis/Scylla calls across all partitions to avoid overwhelming either store under load.

Goroutine lifecycle is driven by the Kafka client's own partition-assignment callbacks (`OnPartitionsAssigned` spawns a goroutine per new partition; `OnPartitionsRevoked`/`OnPartitionsLost` close that partition's channel and block until its goroutine drains) rather than `errgroup.Group`. This was a deliberate change from the original plan: `errgroup.Wait()` blocks until every tracked goroutine returns, which fits a fixed batch of work, not a long-running consumer whose partition set grows and shrinks across rebalances for the life of the process. The revoke callback gives per-partition drain-on-loss for free, which is exactly the rebalance-safety property this decision needs.

## Risks / Trade-offs

- **[Risk]** Reversible `HANDOFF` enables accidental or malicious ping-pong (human toggles back and forth) → **[Mitigation]** Log every transition with `transitioned_by`; rate-limit human-initiated transitions per conversation in a later change if abuse is observed.
- **[Risk]** Rebalance lock adds latency to every state mutation → **[Mitigation]** Short TTL (low single-digit seconds) and lock only around the read-modify-write, not the full message processing.
- **[Risk]** Single-partition-per-tenant caps throughput for very high-volume tenants → **[Mitigation]** Explicitly deferred; revisit sub-partitioning if/when a real tenant hits the ceiling.
- **[Risk]** Event schema in `src/contracts/events/v1` still requires discipline (nothing stops someone from hand-editing structs to drift from it) → **[Mitigation]** Add schema validation in CI as a follow-up task; out of scope for this change's initial implementation but flagged in tasks.md.

## Migration Plan

Greenfield — no existing data or running services to migrate. Deployment order:
1. Apply ScyllaDB migrations (session/history tables).
2. Deploy `worker` (safe to deploy first; idle until events arrive).
3. Deploy `ingress` last, pointed at the existing Redpanda/Redis from `docker-compose.yml`.
No rollback complexity beyond stopping the two new binaries — no schema changes to existing systems since none exist yet.

## Open Questions

- Should the human-initiated `HANDOFF → AI_ENGAGED` transition require the operator to leave a note/reason (for audit quality), or is the `transitioned_by` attribution alone sufficient? Deferred to the handoff-panel change since it's a UI concern.
- Exact LLM-failure retry count (`N`) that triggers automatic `AI_ENGAGED → HANDOFF` is left as a configurable value, default TBD when `src/internal/llm` is designed.

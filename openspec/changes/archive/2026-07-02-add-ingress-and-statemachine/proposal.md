## Why

ecos-ai has a fully documented architecture (C4 model, sequence flows, business strategy) but zero code. The riskiest, most foundational pieces — the webhook ingress that must respond to Meta in milliseconds, and the session state machine that decides whether a conversation is AI-handled or handed off to a human — don't exist yet and everything else (agentic loop, MCP skills, human handoff UI) depends on them. Building this vertical slice first proves the architecture end-to-end (idempotent ingestion → event stream → ordered per-tenant state transitions) before any LLM or MCP complexity is added.

## What Changes

- Add a Go ingress webhook service: HMAC signature verification for Meta WhatsApp payloads, idempotency check via Redis (`SETNX` with 24h TTL), async publish to Redpanda partitioned by `tenant_id`, sub-5ms response target.
- Add a versioned event contract (`src/contracts/events/v1`) shared between producer (ingress) and consumer (worker) — decouples the two from a shared Go struct that could drift silently.
- Add the core session state machine (`src/internal/statemachine`) as a pure, I/O-free package: states `NEW`, `AI_ENGAGED`, `HANDOFF`, `CLOSED`, with explicit transition rules and validation.
- Add a minimal Go agent worker that consumes events partitioned by `tenant_id`, applies state machine transitions, and persists session state to Redis + history to ScyllaDB — without LLM or MCP integration (that's future work).
- Add a distributed lock around per-tenant state mutation (Redis `SET NX` with short TTL) to guard the consumer-group rebalance window where two workers could momentarily believe they own the same partition.
- Establish the monorepo layout under `src/` (`src/cmd/`, `src/internal/`, `src/contracts/`, `src/migrations/`) that later slices (LLM loop, MCP skills, Next.js handoff panel) will build on. All physical code lives under `src/`; the repository root stays reserved for `docs/`, `openspec/`, and infra config.

## Capabilities

### New Capabilities
- `webhook-ingress`: Receives and validates inbound WhatsApp webhooks, enforces idempotency, and publishes events to the event stream within a strict latency budget.
- `session-state-machine`: Defines and enforces the conversation session lifecycle (states, valid transitions, guards) independent of any transport or storage mechanism.
- `conversation-event-contract`: Defines the versioned event schema exchanged between ingress and worker over Redpanda, including compatibility rules for future changes.

### Modified Capabilities
(none — this is the first code in the repository; no existing specs to modify)

## Impact

- **New code**: `src/cmd/ingress`, `src/cmd/worker`, `src/internal/statemachine`, `src/internal/platform/{eventbus,cache,storage}`, `src/contracts/events/v1`.
- **Infra**: relies on the existing `docker-compose.yml` (Redpanda, Redis, ScyllaDB) — no new infra services introduced by this change.
- **Out of scope for this change**: LLM integration (`src/internal/llm`), MCP skill registry (`src/internal/mcp`), and the Next.js handoff panel (`src/apps/web`) — these depend on the state machine and event contract established here but are separate future changes.
- **Open question carried into design**: whether `HANDOFF` is a terminal state or can transition back to `AI_ENGAGED` (e.g., a human operator returns a conversation to the bot). This affects the state machine's transition table and needs a decision before `design.md` is finalized.

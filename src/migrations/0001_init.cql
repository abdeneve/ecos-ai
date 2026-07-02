-- Initial schema for the agent worker's persistence layer.
--
-- Both tables use a (tenant_id, time_bucket) composite partition key, per
-- the anti-hot-partition pattern described in
-- docs/business-strategy/o-segredo-do-contrato.md: bucketing by day keeps
-- any single tenant's partition bounded regardless of overall message
-- volume, instead of accumulating unbounded rows under one tenant_id key.

CREATE KEYSPACE IF NOT EXISTS ecos_ai
WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

-- Immutable log of every inbound message the worker has processed.
CREATE TABLE IF NOT EXISTS ecos_ai.message_history (
    tenant_id   text,
    time_bucket text,   -- UTC day bucket, format YYYYMMDD
    occurred_at timestamp,
    message_id  text,
    trace_id    text,
    payload     text,
    PRIMARY KEY ((tenant_id, time_bucket), occurred_at, message_id)
) WITH CLUSTERING ORDER BY (occurred_at DESC);

-- Audit trail of every session_state_machine transition, including who or
-- what initiated it (system vs. a specific human operator) — this is what
-- makes a reversible HANDOFF -> AI_ENGAGED transition auditable rather than
-- silent (design.md, decision 5).
CREATE TABLE IF NOT EXISTS ecos_ai.session_transitions (
    tenant_id       text,
    time_bucket     text,   -- UTC day bucket, format YYYYMMDD
    at              timestamp,
    from_state      text,
    to_state        text,
    transitioned_by text,
    PRIMARY KEY ((tenant_id, time_bucket), at)
) WITH CLUSTERING ORDER BY (at DESC);

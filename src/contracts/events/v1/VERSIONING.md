# Event Contract Versioning

`contracts/events/v1` is the source of truth for the shape of events exchanged
between the ingress webhook (producer) and the agent worker (consumer) over
Redpanda. Both services validate against `schema.json` in this package rather
than relying on a shared Go struct staying in sync by convention.

## Rule: changes are additive, not breaking

- A new field needed by a future capability is added as **optional** in a new
  version directory (`contracts/events/v2`), never added as required to `v1`
  in place.
- `v1` keeps working, unmodified, for as long as any producer or consumer
  still emits or expects it. Removing a version is a separate, explicit
  decision — not a side effect of adding a new one.
- Renaming or changing the type of an existing required field is a breaking
  change and requires a new version, not an edit to the existing one.
- Consumers select which version(s) they can decode by importing the
  corresponding package (e.g. `contracts/events/v1` vs `contracts/events/v2`);
  there is no implicit upgrade.

## Adding v2 (when the time comes)

1. Copy `v1/schema.json` into a new `v2/schema.go`, add the new field(s).
2. Copy `v1/event.go`'s Go type and validation wiring into `v2`, extended
   for the new field(s).
3. Update producers to emit `v2` once all consumers can decode it; until
   then, producers may need to emit both versions or consumers must accept
   both.

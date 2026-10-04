# ADR 0002 — Four components composed into one adaptive loop

**Status:** Accepted

## Context
The project could have been a grab-bag of unrelated components. It's stronger as a
single, coherent story: raw telemetry that governs, watches, and explains itself.

## Decision
Build four components that chain into one loop, using the Collector's own composition
primitives rather than one monolithic component:

```
traces ─► redmetrics (RED) ─► metrics ─► anomaly (EWMA) ─► filter ─► llmtriage ─► logs
                             resourcecost enriches every signal along the way
```

- `redmetrics` (traces→metrics connector) produces RED metrics **and** a per-interval
  average-latency gauge shaped for streaming detection.
- `anomaly` (metrics processor) z-scores those gauges and tags anomalies.
- A stock `filter` processor keeps only flagged points — composition over a bespoke
  router.
- `llmtriage` (metrics→logs connector) turns anomalies into incident logs.

## Consequences
- Each component stays single-purpose and independently testable; the interesting
  behaviour is emergent from composition, which is idiomatic Collector design.
- Using two connectors with **different signal pairs** (traces→metrics, metrics→logs)
  exercises the connector model across more than one signal boundary.
- The framing is intentionally generic-infrastructure/FinOps and AIOps, not GenAI,
  so it is broadly applicable.

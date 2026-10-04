# ADR 0003 — Honest `MutatesData` declarations

**Status:** Accepted

## Context
Every Collector component declares `consumer.Capabilities{MutatesData: bool}`. If any
component in a pipeline mutates data, the pipeline runs in exclusive-ownership mode and
data is cloned at fan-out; otherwise pipelines share a single read-only copy. Declaring
this wrong causes either data races/corruption (claiming read-only while mutating) or
needless clones (claiming mutation when read-only).

## Decision
Declare capabilities to match reality, and document why on each component:

| Component | MutatesData | Why |
|---|---|---|
| `resourcecost` | **true** | writes new resource attributes in place |
| `anomaly` | **true** | writes `anomaly.score` / `anomaly.is_anomaly` onto data points |
| `redmetrics` | **false** | reads spans, produces brand-new metrics |
| `llmtriage` | **false** | reads metrics, produces brand-new logs |

The two connectors build fresh pdata, so they never mutate their input. The two
processors do, so they pay for a clone at fan-out — an accepted cost of enrichment.

## Consequences
- Correctness under fan-out is guaranteed by construction, not by luck.
- Where state is also shared across concurrent `Consume*` calls (`anomaly`'s series
  map, `redmetrics`'s aggregation), it is guarded by a mutex / lock-striped shards and
  proven race-clean with `go test -race`.
- A performance lever is explicit: enrichment that could be done read-only would avoid
  the clone. Here it genuinely can't, and we say so.

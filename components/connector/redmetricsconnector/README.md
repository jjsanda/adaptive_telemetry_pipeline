# redmetrics

A **connector** that turns trace spans into cumulative RED (Rate, Errors, Duration) metrics, plus a per-interval average-latency gauge shaped for streaming anomaly detection.

| Status        |                                   |
| ------------- | --------------------------------- |
| Stability     | `beta`: traces_to_metrics         |
| Signals       | traces → metrics                  |
| Distributions | `adaptive-otelcol`                |
| Mutates data  | no — reads spans, builds new metrics |
| Code owners   | @jjsanda                          |

Emitted metrics (all keyed by the configured `dimensions`):

| Metric | Type | Notes |
| --- | --- | --- |
| `red.calls` | Sum (cumulative, monotonic) | span count |
| `red.errors` | Sum (cumulative, monotonic) | spans with `StatusCode == Error` |
| `red.duration.ms` | Histogram (cumulative) | uses the `histogram_buckets_ms` bounds |
| `red.latency.avg.ms` | Gauge (per-interval) | mean latency since the previous flush — one scalar per series, purpose-shaped for the `anomaly` detector |

## Configuration

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `dimensions` | `[]string` | `[service.name, span.kind, http.route, status.code]` | Attribute names used to key each series. Resource attrs, span attrs, or the specials `span.kind` / `status.code` / `span.name`. |
| `histogram_buckets_ms` | `[]float64` | `[5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000]` | Explicit upper bounds (ms) for the duration histogram. Must be strictly increasing. |
| `flush_interval` | `duration` | `15s` | How often accumulated metrics are emitted. `0` flushes synchronously on every batch (used in tests). |
| `max_series` | `int` | `10000` | Caps distinct dimension combinations. Series beyond the cap are dropped and counted. `0` = unbounded (not recommended). |
| `namespace` | `string` | `red` | Prefix for every emitted metric name. |

## Example

```yaml
connectors:
  redmetrics:
    dimensions: [service.name, span.kind, http.route, status.code]
    histogram_buckets_ms: [5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000]
    flush_interval: 5s
```

## How it works

`ConsumeTraces` folds spans into per-series counters under a mutex; a background ticker flushes every `flush_interval`. The counters are **cumulative** (fixed start timestamp, ever-growing counts) — the temporality Prometheus expects — while the gauge is a **delta**: `(sumMs − lastSumMs) / (calls − lastCalls)` since the previous flush, because a streaming detector needs one fresh scalar per interval, not a monotonic curve. `max_series` bounds cardinality against pathological dimensions (e.g. an un-normalized URL). Because it builds brand-new `pdata`, it never mutates its input (`MutatesData: false`). See [ADR-0002](../../../docs/adr/0002-four-components-and-the-adaptive-loop.md).

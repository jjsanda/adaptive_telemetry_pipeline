# anomaly

A streaming **processor** that scores every scalar metric data point with an EWMA z-score and tags outliers — O(1) memory per series, concurrency-safe, self-cleaning.

| Status        |                                   |
| ------------- | --------------------------------- |
| Stability     | `alpha`: metrics                  |
| Signals       | metrics → metrics (annotated)     |
| Distributions | `adaptive-otelcol`                |
| Mutates data  | **yes** — writes score attributes onto data points |
| Code owners   | @jjsanda                          |

Attributes written onto each scored data point:

- `anomaly.score` — the z-score (double, rounded to 3 dp).
- `anomaly.is_anomaly` — `bool`, true when `|z| > threshold` after warmup.

Only **Gauge** and **non-monotonic Sum** points are scored; histograms, summaries, and monotonic counters pass through untouched (a z-score on an ever-climbing value is meaningless).

## Configuration

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `alpha` | `float64` | `0.05` | EWMA smoothing factor in `(0,1]`. Larger reacts faster but absorbs sustained shifts sooner. |
| `threshold` | `float64` | `3.0` | The `\|z-score\|` above which a point is flagged. |
| `warmup` | `int` | `20` | Samples observed **per series** before any flag. |
| `shards` | `int` | `16` | Lock shards for the per-series state map. More shards reduce contention under concurrency. |
| `stale_after` | `duration` | `5m` | Evict a series whose last sample is older than this. `0` disables. |
| `sweep_interval` | `duration` | `1m` | How often the stale-series sweeper runs. `0` disables. |

## Example

```yaml
processors:
  anomaly:
    alpha: 0.05
    threshold: 3.0
    warmup: 8          # ~40s to warm a series at a 5s upstream flush_interval
    stale_after: 5m
    sweep_interval: 1m
```

`warmup` counts **samples per series**, not seconds: with `redmetrics` flushing every 5 s, `warmup: 8` means a series needs ~40 s of traffic before it can be flagged. A production deployment would use a larger warmup.

## How it works

Per series the detector maintains an exponentially-weighted mean and variance (the West/Finch incremental EWMV recurrence) and scores each sample `z = (x − mean) / √var`:

```
diff = x - mean;  mean += alpha*diff
var  = (1 - alpha) * (var + diff*(alpha*diff))
```

This is a direct port of a validated Python oracle (z-scores match within 1e-3) and runs at **~9 ns/op with 0 allocations**. **Catch-up is a deliberate feature:** a fast EWMA flags the *onset* of a spike and then adapts to the new level — a 100→200→205→198 shift flags the first two points, not the third. Series state lives in a lock-striped store keyed by resource attrs + metric name + data-point attrs; a background sweeper evicts series unseen for `stale_after`, and an observable gauge `anomaly.series.tracked` proves memory stays flat. See [ADR-0005](../../../docs/adr/0005-ewma-detector-and-its-limits.md).

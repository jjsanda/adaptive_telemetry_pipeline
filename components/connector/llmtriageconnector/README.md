# llmtriage

A **connector** that consumes anomaly-flagged metrics and asynchronously asks an LLM to explain them, emitting each summary as a new, trace-correlated log record.

| Status        |                                   |
| ------------- | --------------------------------- |
| Stability     | `alpha`: metrics_to_logs          |
| Signals       | metrics → logs                    |
| Distributions | `adaptive-otelcol`                |
| Mutates data  | no — reads metrics, builds new logs |
| Code owners   | @jjsanda                          |

## Configuration

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `backend` | `string` | `deterministic` | Triage engine: `deterministic` (offline, no key) or `anthropic` (Claude Messages API). |
| `model` | `string` | `claude-sonnet-5` | Model id passed to the `anthropic` backend. |
| `api_key` | `string` | `""` | API key for the `anthropic` backend. Redacted from logged config (`configopaque`). |
| `timeout` | `duration` | `10s` | Bounds each LLM call. |
| `queue_size` | `int` | `1000` | Bounded channel capacity. When full, anomalies are dropped and counted — the backpressure boundary. |
| `workers` | `int` | `2` | Goroutines draining the queue. |
| `max_batch` | `int` | `10` | Most anomalies triaged in a single LLM call. |
| `max_wait` | `duration` | `3s` | How long a worker waits to fill a batch before flushing. |
| `debounce_window` | `duration` | `60s` | Suppress re-triaging the same `(service, metric)` within this window. |
| `webhook_url` | `string` | `""` | If set, POSTs the triage summary as JSON (e.g. Slack). |

`Validate` rejects unknown backends and requires `api_key` when `backend: anthropic`.

## Example

```yaml
connectors:
  llmtriage:
    backend: ${env:ADAPTIVE_LLM_BACKEND:-deterministic}
    api_key: ${env:ANTHROPIC_API_KEY:-}
    max_batch: 10
    max_wait: 5s
    debounce_window: 60s
```

## How it works

**Async by design.** `ConsumeMetrics` is on the hot path, so it only extracts flagged points and does a **non-blocking** enqueue onto a bounded channel — if the queue is full it sheds load (incrementing `llmtriage.queue.dropped`) rather than block. A worker pool drains the queue, batches related anomalies (up to `max_batch` / `max_wait`), debounces duplicate `(service, metric)` pairs, calls the LLM with a timeout, and emits a trace-correlated log on the component-lifetime context. A test asserts `ConsumeMetrics` returns in <50 ms even against a 300 ms backend.

**Two backends behind one interface.** The default `deterministic` engine is offline and reproducible (zero keys): it foregrounds the strongest signal in the batch and points on-call at the next step — and doubles as the test fake. The optional `anthropic` engine calls the Claude Messages API. LLM output is treated as **untrusted**: the JSON is extracted from any surrounding prose and the severity normalized before it becomes a log record. The component instruments itself — queue depth/drops, LLM latency/errors, tokens, and summaries emitted. See [ADR-0004](../../../docs/adr/0004-async-llm-triage-boundary.md).

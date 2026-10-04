# ADR 0004 — The LLM call is asynchronous and bounded

**Status:** Accepted

## Context
`llmtriage` calls an LLM, which is slow (hundreds of ms to seconds), rate-limited, and
costs money. A Collector's `Consume*` methods are on the hot path: blocking one applies
backpressure all the way back to the instrumented app.

## Decision
Split the component at a bounded queue:

- `ConsumeMetrics` only extracts anomalies and does a **non-blocking** enqueue onto a
  bounded channel, then returns immediately. If the queue is full, it **sheds load**
  (drops + increments a metric) rather than block.
- A worker pool drains the queue, **batches** related anomalies (up to `max_batch` /
  `max_wait`), **debounces** duplicate (service, metric) pairs, and calls the LLM with a
  timeout. Workers emit the resulting log on the component-lifetime context — the same
  pattern `spanmetrics` uses to flush on a ticker.
- The LLM backend is an interface with a deterministic offline default and an optional
  Anthropic implementation. LLM output is treated as untrusted: parsed and validated
  before it becomes a log record.

## Consequences
- Pipeline throughput is independent of LLM latency (there's a test that asserts
  `ConsumeMetrics` returns in <50 ms even with a 300 ms backend).
- Cost is bounded by batching + debouncing; correctness is bounded by validation.
- Under a flood the system degrades gracefully (sheds, with a drop metric) instead of
  stalling ingestion. The component instruments itself: queue depth, drops, LLM
  latency/errors, tokens, summaries emitted.

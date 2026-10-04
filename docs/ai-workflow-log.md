# AI-in-the-workflow log

This project was built with an LLM coding agent in the loop throughout. This log
is an honest account of where that helped and — more usefully — where it produced
subtly wrong OpenTelemetry code that a human who understands the Collector had to
catch.

## Where it clearly helped
- **Scaffolding boilerplate.** Factory + typed `Config` + `processorhelper`
  wiring, the `metadata.yaml` files, and the multi-stage Dockerfiles are
  repetitive and well-suited to generation.
- **Table-driven tests.** Especially the anomaly oracle test — restating a
  validated Python recurrence as a Go fixture and asserting parity.
- **The polyglot services.** The Node and Python services (correct OTLP export,
  W3C baggage extraction, chaos injection) were generated from a precise contract
  and only needed light review.
- **Glue.** docker-compose, Prometheus/Loki/Grafana provisioning, CI YAML, and the
  Mermaid diagrams — tedious to write by hand, easy to review.

## Where it was confidently wrong (the important part)
- **Version drift.** The guiding design doc referenced Collector **v0.115** (Dec
  2024). By mid-2026 the line is **v0.155 / v1.61**. Every version number had to be
  re-verified against `pkg.go.dev` and a known-good build — *not* taken from the
  model's memory, which is anchored to its training cutoff. The two-week release
  cadence guarantees stale suggestions.
- **A removed API.** `component.ConfigValidator` no longer exists. The model
  reached for it repeatedly; the correct pattern today is simply a `Validate()
  error` method on the config (the framework calls it by convention).
- **The stable/beta split.** `pdata`, `component`, `consumer`, and `processor` are
  stable `v1.61.0`, but `connector` is still `v0.155.0`. An LLM will happily
  "align" them to one number — which fails `ocb`'s strict version check.
- **pdata ownership.** Two traps a model won't respect by default: declaring
  `MutatesData` correctly, and the fact that `BucketCounts().FromRaw(slice)`
  *retains* the slice — so histogram bucket counts had to be copied before the
  metric was handed downstream, or later mutation would corrupt in-flight data.
- **The async instinct.** The naive suggestion is to call the LLM inside
  `ConsumeMetrics`. The senior design enqueues and returns, draining on a worker
  pool — the whole point of the `llmtriage` component.

## Takeaway
LLM tooling is excellent for *scaffolding and explanation* and a real
accelerator. But Collector **correctness** — versioning, data ownership,
concurrency, connector semantics — needs a human who understands the internals.
That is exactly why the four components here were designed, reviewed, and
`-race`-tested by hand rather than accepted as generated.

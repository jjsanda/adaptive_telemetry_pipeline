# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — 2026-07-04

### Added
- Custom **adaptive-otelcol** OpenTelemetry Collector distribution built with `ocb`
  (core v1.61.0 / v0.155.0, Go 1.25).
- **`resourcecost`** processor — FinOps/governance attributes (`telemetry.owner.team`,
  `telemetry.cost.estimate`) across traces, metrics, and logs.
- **`redmetrics`** connector — trace spans → RED metrics (rate, errors, duration
  histogram) plus a per-interval average-latency gauge shaped for anomaly detection.
- **`anomaly`** processor — streaming EWMA z-score anomaly detection with a sharded,
  concurrent per-series store and a stale-series sweeper.
- **`llmtriage`** connector — asynchronous LLM triage of anomalies into correlated
  incident log records; deterministic offline engine by default, optional Anthropic
  backend; self-observability metrics.
- Polyglot demo "order pipeline" app (Go · Node · Python) with cross-language W3C
  context + baggage propagation and chaos-based fault injection.
- Docker Compose stack (Jaeger, Prometheus, Grafana, Loki) and Kubernetes deployment
  via the OpenTelemetry Operator (gateway + agent + auto-instrumentation).
- Rendered architecture diagrams, ADRs, and a multi-job CI pipeline.

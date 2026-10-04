# Troubleshooting

Common issues running the `adaptive-otelcol` stack and its build, with the fastest fix for each. Most problems are one of: the detector is still warming up, a config typo, or an `ocb` version mismatch.

## No triage log appears

This is almost always **warmup, not a bug**. The `anomaly` detector will not flag a series until it has seen more than `warmup` samples of it, and `warmup` counts *samples*, not seconds. With `redmetrics` flushing every `flush_interval: 5s` and `anomaly` set to `warmup: 8`, a series needs roughly **40 s of steady traffic** before it can be flagged at all.

- Drive more baseline traffic first: `make demo` warms the detector for ~60 s before injecting the anomaly. If you skip the warmup, the spike lands during warmup and is (correctly) not flagged.
- Confirm metrics are actually flowing: check Prometheus at http://localhost:9090 for `red_latency_avg_ms`. No series there means no traces are reaching `redmetrics`.
- Remember the **catch-up** behaviour: a *sustained* shift is flagged only at its onset, then the EWMA adapts. A slow ramp may never trip the threshold — induce a sharp step (the demo uses `?chaos=pricing&mode=slow&ms=900`).
- Look in the right place: triage logs carry `service.name = adaptive-otelcol`. In Grafana → Explore → Loki, query `{service_name="adaptive-otelcol"}`, or `make logs | grep -i triage`.
- Check the drop counter: if `llmtriage.queue.dropped` is climbing, the queue is saturated and summaries are being shed — raise `queue_size` or `workers`.

## Collector won't start

Almost always config validation. Build and validate the full pipeline offline before starting the stack:

```bash
make collector-validate   # builds adaptive-otelcol, then runs `validate --config ...`
```

The error names the offending component and field. Common causes: a `duration` written without a unit (`5` instead of `5s`), `histogram_buckets_ms` not strictly increasing, `alpha` outside `(0,1]`, or `backend: anthropic` without an `api_key`.

## `ocb` build fails

The Collector ships two version lines and **every module must sit on one consistent pair**: stable modules (`pdata`, `component`, `consumer`, `processor`, confmap providers) at `v1.61.0`, and beta/contrib modules (`connector`, receivers, contrib components) at `v0.155.0`. A single mismatched `gomod` line in [`collector/builder-config.yaml`](../collector/builder-config.yaml) breaks the build. If you bump one, bump them all to the same line, and keep the builder itself pinned (`BUILDER_VERSION := v0.155.0` in the Makefile).

## Memory pressure on the compose stack

The stack is tuned for a laptop with per-service `mem_limit`s (~2 GiB total; the collector itself is capped at 320 MiB). If containers are OOM-killed:

- Free host memory or raise the limits in [`deploy/compose/docker-compose.yaml`](../deploy/compose/docker-compose.yaml).
- The in-pipeline `memory_limiter` processor (75% soft / 20% spike) will refuse data before the container dies — expect `data refused` warnings under load rather than a crash.
- Verify the `anomaly` sweeper is keeping series bounded: the `anomaly.series.tracked` gauge should plateau, not climb.

## Ports already in use

The stack binds `4317`/`4318` (OTLP), `3000` (Grafana), `16686` (Jaeger), `9090` (Prometheus), `8080` (order-api), `8889`/`8888` (collector metrics), and `13133` (health check). If a bind fails, stop the conflicting process or remap the host side of the port in the compose file. `make down` frees everything this stack owns.

## Enabling the real Anthropic backend

The default backend is the deterministic offline engine (no keys, reproducible). To use Claude instead, set two environment variables before `make up`:

```bash
export ADAPTIVE_LLM_BACKEND=anthropic
export ANTHROPIC_API_KEY=sk-ant-...
# optional: export ADAPTIVE_LLM_MODEL=<model-id>
```

`config validate` will reject `backend: anthropic` with an empty `api_key`, so a missing key fails fast at startup rather than silently. The key is held in a `configopaque.String` and redacted from logged config.

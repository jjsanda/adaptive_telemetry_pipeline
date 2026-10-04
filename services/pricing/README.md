# pricing

A Python 3.12 + FastAPI microservice that prices one order line. In the
`adaptive_telemetry_pipeline` demo it is called by the Go `fulfillment-worker`
(`POST /price`) inside a single distributed trace (Go -> pricing) propagated with
W3C trace context + baggage. It is the **primary anomaly source**: chaos baggage
targeting `pricing` makes it slow (or error), producing the latency spike the
Collector's anomaly detector catches.

## API
- `POST /price` -> `{ "sku": str, "quantity": number }` returns `{ "price_usd": number, "currency": "USD" }` (`round(base * quantity, 2)`; known SKUs use a fixed catalogue, unknown ones a deterministic SHA-256 price).
- `GET /healthz` -> `{ "status": "ok" }`.

## OpenTelemetry
Traces, metrics, and logs via the OTel SDK, wired in `app/otel.py` and applied with
`FastAPIInstrumentor`. Env-driven: `OTEL_EXPORTER_OTLP_ENDPOINT` (default
`http://localhost:4318`), `OTEL_SERVICE_NAME` (default `pricing`),
`OTEL_EXPORTER_OTLP_PROTOCOL` (default `http/protobuf`). Custom `pricing.requests`
counter and `price.usd` histogram; one trace-correlated log per request. Exporters
fail quietly with no Collector present.

## Chaos contract
Reads W3C baggage `chaos.target` / `chaos.mode` / `chaos.value` (ms). When the
target is `pricing` or `all`: `slow` sleeps `chaos.value` ms (default 800),
`error` returns HTTP 500. Matches `services/shared/chaos.go`.

## Develop
```bash
python3.12 -m venv .venv && . .venv/bin/activate
pip install -r requirements-dev.txt
ruff check . && pytest
PORT=8083 python -m app.main   # serves on :8083 (Docker: docker build -t pricing . && docker run -p 8083:8083 pricing)
```

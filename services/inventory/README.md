# inventory

Node.js + TypeScript microservice for the `adaptive_telemetry_pipeline` polyglot
OpenTelemetry demo. It is called by the Go `fulfillment-worker` and checks stock
for an order line, participating in one distributed trace (Go → inventory →
Python `pricing`) via W3C trace context + baggage.

## Endpoints

- `POST /check` — body `{ "sku": string, "quantity": number }` →
  `{ "in_stock": boolean, "available": number }`. `available` is a stable,
  deterministic per-SKU number (`hash(sku) % 50 + 10`); `in_stock` is
  `available >= quantity`.
- `GET /healthz` → `200 {"status":"ok"}`.

## OpenTelemetry

Traces + metrics are exported via OTLP HTTP/protobuf. Everything is env-driven:
`OTEL_EXPORTER_OTLP_ENDPOINT` (default `http://localhost:4318`), `OTEL_SERVICE_NAME`
(default `inventory`), `OTEL_EXPORTER_OTLP_PROTOCOL` (default `http/protobuf`). A
custom `inventory.checks` counter is emitted per check, tagged with `in_stock`.
The SDK (`src/instrumentation.ts`) is loaded before the HTTP framework. With no
Collector reachable, exports fail quietly and the server keeps serving.

## Chaos injection

If W3C baggage carries `chaos.target` = `inventory` (or `all`), `/check` honors
`chaos.mode`: `slow` sleeps `chaos.value` ms (default 800) before responding;
`error` returns HTTP 500. Matches the Go `shared.ApplyChaos` contract.

## Run

```bash
npm install
npm run build
npm start        # node --import ./dist/instrumentation.js dist/index.js, port 8082 (PORT env)
npm run dev      # watch mode via tsx
npm run lint
npm test         # builds, then runs the node:test suite

# smoke test
curl localhost:8082/healthz
curl -X POST localhost:8082/check -H 'content-type: application/json' \
  -d '{"sku":"SKU-COFFEE","quantity":2}'

# Docker
docker build -t inventory . && docker run -p 8082:8082 inventory
```

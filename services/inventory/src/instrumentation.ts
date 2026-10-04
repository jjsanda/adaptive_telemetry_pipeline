// OpenTelemetry bootstrap for the inventory service.
//
// This module MUST be evaluated before the HTTP framework (express/http) is
// loaded, so that `require-in-the-middle` can patch those modules. That is
// guaranteed two ways: it is preloaded via `node --import ./dist/instrumentation.js`
// (see the `start` script / Dockerfile CMD), and `src/index.ts` also imports it
// statically before dynamically importing the server.
//
// Configuration is entirely environment-driven (12-factor). We only set process
// defaults when the operator has not provided a value, so nothing about the
// collector endpoint is hard-coded — OTEL_EXPORTER_OTLP_ENDPOINT and friends win.
import { NodeSDK } from '@opentelemetry/sdk-node';
import { getNodeAutoInstrumentations } from '@opentelemetry/auto-instrumentations-node';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-proto';
import { OTLPMetricExporter } from '@opentelemetry/exporter-metrics-otlp-proto';
import { PeriodicExportingMetricReader } from '@opentelemetry/sdk-metrics';
import { resourceFromAttributes } from '@opentelemetry/resources';
import {
  ATTR_SERVICE_NAME,
  ATTR_SERVICE_VERSION,
} from '@opentelemetry/semantic-conventions';

// Env-driven defaults. `??=` only assigns when unset, so any real environment
// value (e.g. a Collector URL) takes precedence and the endpoint is never
// hard-coded in the exporters below.
process.env.OTEL_SERVICE_NAME ??= 'inventory';
process.env.OTEL_EXPORTER_OTLP_ENDPOINT ??= 'http://localhost:4318';
process.env.OTEL_EXPORTER_OTLP_PROTOCOL ??= 'http/protobuf';

const sdk = new NodeSDK({
  resource: resourceFromAttributes({
    [ATTR_SERVICE_NAME]: process.env.OTEL_SERVICE_NAME,
    [ATTR_SERVICE_VERSION]: process.env.OTEL_SERVICE_VERSION ?? '0.1.0',
  }),
  // Exporters read OTEL_EXPORTER_OTLP_* from the environment; no args = no
  // hard-coded endpoint. If no Collector is reachable, exports fail silently in
  // the background (batch processors swallow the error) — the server keeps
  // serving requests regardless.
  traceExporter: new OTLPTraceExporter(),
  metricReader: new PeriodicExportingMetricReader({
    exporter: new OTLPMetricExporter(),
    exportIntervalMillis: Number(process.env.OTEL_METRIC_EXPORT_INTERVAL ?? 10_000),
  }),
  // Default propagators are W3C tracecontext + baggage, exactly what the Go and
  // Python services use, so one trace_id (and the chaos baggage) survives every hop.
  instrumentations: [getNodeAutoInstrumentations()],
});

sdk.start();

const shutdown = (): void => {
  sdk
    .shutdown()
    .catch((err: unknown) => {
      // Best-effort flush; never block process exit on a missing Collector.
      console.error('otel shutdown error', err);
    })
    .finally(() => process.exit(0));
};

process.once('SIGTERM', shutdown);
process.once('SIGINT', shutdown);

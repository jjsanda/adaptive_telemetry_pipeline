"""OpenTelemetry SDK bootstrap for the pricing service.

Wires all three signals -- traces, metrics, and logs -- to an OTLP endpoint and
configures W3C trace-context + baggage propagation, mirroring the Go ``shared``
package the other services in this demo use. Configuration is entirely
environment-driven (12-factor); the collector endpoint is never hard-coded.

The exporter defaults to **HTTP/protobuf** (``:4318``) so it lines up with the
Collector's OTLP/HTTP receiver, but ``OTEL_EXPORTER_OTLP_PROTOCOL=grpc`` is
honoured too. Exporters connect lazily: if no Collector is reachable the SDK
logs and drops telemetry from a background thread -- the app keeps serving.
"""

import logging
import os

from opentelemetry import metrics, trace
from opentelemetry._logs import set_logger_provider
from opentelemetry.baggage.propagation import W3CBaggagePropagator
from opentelemetry.instrumentation.logging.handler import LoggingHandler
from opentelemetry.propagate import set_global_textmap
from opentelemetry.propagators.composite import CompositePropagator
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

DEFAULT_ENDPOINT = "http://localhost:4318"
DEFAULT_SERVICE_NAME = "pricing"
SERVICE_VERSION = "0.1.0"

# Resolved once at import so the request handler / chaos logic can compare the
# incoming ``chaos.target`` baggage against this service's own name.
SERVICE_NAME = os.environ.get("OTEL_SERVICE_NAME", DEFAULT_SERVICE_NAME)

_configured = False


def _endpoint() -> str:
    return os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT", DEFAULT_ENDPOINT).rstrip("/")


def _use_http() -> bool:
    proto = os.environ.get("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf").lower()
    return proto != "grpc"


def _build_resource() -> Resource:
    # Resource.create() also merges OTEL_RESOURCE_ATTRIBUTES and process defaults.
    return Resource.create(
        {
            "service.name": SERVICE_NAME,
            "service.version": SERVICE_VERSION,
            "deployment.environment.name": os.environ.get("DEPLOYMENT_ENVIRONMENT", "local"),
        }
    )


def _build_exporters(endpoint: str, use_http: bool):
    """Return ``(span_exporter, metric_exporter, log_exporter)`` for the protocol.

    Imported lazily so the process boots regardless of which OTLP transport is
    selected. For HTTP each signal gets its standard ``/v1/<signal>`` path.
    """
    if use_http:
        from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

        return (
            OTLPSpanExporter(endpoint=f"{endpoint}/v1/traces"),
            OTLPMetricExporter(endpoint=f"{endpoint}/v1/metrics"),
            OTLPLogExporter(endpoint=f"{endpoint}/v1/logs"),
        )

    from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
    from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
    from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter

    return (
        OTLPSpanExporter(endpoint=endpoint),
        OTLPMetricExporter(endpoint=endpoint),
        OTLPLogExporter(endpoint=endpoint),
    )


def setup_otel() -> logging.Logger:
    """Initialise global Tracer/Meter/Logger providers; return a trace-correlated logger.

    Idempotent: safe to call from both the uvicorn process and the test suite.
    """
    global _configured

    # Composite W3C propagation (trace context + baggage) so one trace_id -- and
    # the chaos baggage -- survives the Go -> pricing hop. This is already the
    # Python default; we set it explicitly to match the Go ``shared`` bootstrap.
    set_global_textmap(
        CompositePropagator([TraceContextTextMapPropagator(), W3CBaggagePropagator()])
    )

    app_logger = logging.getLogger(SERVICE_NAME)
    if _configured:
        return app_logger

    resource = _build_resource()
    span_exp, metric_exp, log_exp = _build_exporters(_endpoint(), _use_http())

    # --- Traces ---
    tracer_provider = TracerProvider(resource=resource)
    tracer_provider.add_span_processor(BatchSpanProcessor(span_exp))
    trace.set_tracer_provider(tracer_provider)

    # --- Metrics ---
    reader = PeriodicExportingMetricReader(metric_exp)
    meter_provider = MeterProvider(resource=resource, metric_readers=[reader])
    metrics.set_meter_provider(meter_provider)

    # --- Logs (SDK still beta: opentelemetry.sdk._logs) ---
    logger_provider = LoggerProvider(resource=resource)
    logger_provider.add_log_record_processor(BatchLogRecordProcessor(log_exp))
    set_logger_provider(logger_provider)

    # Bridge stdlib logging -> OTel logs (opentelemetry-instrumentation-logging
    # is the non-deprecated home for this handler). Attached to THIS service's
    # logger only, not the root logger, so an exporter-failure log can't feed
    # back into the log pipeline. Records emitted while a span is active are
    # auto-stamped with trace_id / span_id, giving trace-correlated logs.
    app_logger.addHandler(LoggingHandler(level=logging.INFO, logger_provider=logger_provider))
    app_logger.addHandler(logging.StreamHandler())  # human-readable console for local dev
    app_logger.setLevel(logging.INFO)
    app_logger.propagate = False

    _configured = True
    return app_logger

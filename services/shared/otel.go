// Package shared provides a small, opinionated OpenTelemetry SDK bootstrap used
// by the Go services in the adaptive_telemetry_pipeline demo. It wires all
// three signals — traces, metrics, and logs — to an OTLP/gRPC endpoint and
// returns a single composite shutdown func that flushes each provider on exit.
//
// Configuration is entirely environment-driven (12-factor): the OTLP exporters
// honour the standard OTEL_EXPORTER_OTLP_* variables, so the collector endpoint
// is selected with OTEL_EXPORTER_OTLP_ENDPOINT and never hard-coded. Sending to
// a Collector (rather than a backend directly) is deliberate: it decouples the
// app from retry/backpressure and lets us enrich telemetry (k8sattributes,
// resourcecost) without touching service code.
package shared

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otellog "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// SetupOTel initialises global Tracer/Meter/Logger providers for the given
// service and returns (shutdown, logger). The returned *slog.Logger emits log
// records through the OpenTelemetry logs bridge, so every log written with it is
// automatically correlated to the active span (trace_id / span_id).
//
// The logs SDK is still beta (go.opentelemetry.io/otel/log v0.x); we pin it and
// accept additive interface churn, which is exactly the kind of version
// discipline a custom-Collector shop needs.
func SetupOTel(ctx context.Context, serviceName, serviceVersion string) (shutdown func(context.Context) error, logger *slog.Logger, err error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithProcessRuntimeName(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
			semconv.DeploymentEnvironmentName(env("DEPLOYMENT_ENVIRONMENT", "local")),
		),
	)
	if err != nil {
		return nil, nil, err
	}

	// Composite propagation: W3C trace context + baggage, so a single request
	// keeps one trace_id across Go, Node, and Python services.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	var shutdowns []func(context.Context) error

	// --- Traces ---
	traceExp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	shutdowns = append(shutdowns, tp.Shutdown)

	// --- Metrics ---
	metricExp, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(10*time.Second))),
	)
	otel.SetMeterProvider(mp)
	shutdowns = append(shutdowns, mp.Shutdown)

	// --- Logs (beta) ---
	logExp, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
	)
	otellog.SetLoggerProvider(lp)
	shutdowns = append(shutdowns, lp.Shutdown)

	// slog.Logger backed by the OTel logs bridge → trace-correlated logs.
	logger = otelslog.NewLogger(serviceName)

	shutdown = func(ctx context.Context) error {
		var errs error
		for i := len(shutdowns) - 1; i >= 0; i-- {
			errs = errors.Join(errs, shutdowns[i](ctx))
		}
		return errs
	}
	return shutdown, logger, nil
}

// env returns the value of key, or def when unset/empty.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

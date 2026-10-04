package redmetricsconnector

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/consumer"
)

const typeStr = "redmetrics"

// NewFactory returns a factory for the redmetrics traces-to-metrics connector.
func NewFactory() connector.Factory {
	return connector.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		connector.WithTracesToMetrics(createTracesToMetrics, component.StabilityLevelBeta),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		Dimensions:         []string{"service.name", "span.kind", "http.route", "status.code"},
		HistogramBucketsMs: []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
		FlushInterval:      15 * time.Second,
		MaxSeries:          10000,
		Namespace:          "red",
	}
}

func createTracesToMetrics(_ context.Context, set connector.Settings, cfg component.Config, next consumer.Metrics) (connector.Traces, error) {
	return newConnector(cfg.(*Config), set, next), nil
}

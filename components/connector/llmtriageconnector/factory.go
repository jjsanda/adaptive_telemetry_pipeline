package llmtriageconnector

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/consumer"
)

const typeStr = "llmtriage"

// NewFactory returns a factory for the llmtriage metrics-to-logs connector.
func NewFactory() connector.Factory {
	return connector.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		connector.WithMetricsToLogs(createMetricsToLogs, component.StabilityLevelAlpha),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		Backend:        "deterministic",
		Model:          "claude-sonnet-5",
		Timeout:        10 * time.Second,
		QueueSize:      1000,
		Workers:        2,
		MaxBatch:       10,
		MaxWait:        3 * time.Second,
		DebounceWindow: 60 * time.Second,
	}
}

func createMetricsToLogs(_ context.Context, set connector.Settings, cfg component.Config, next consumer.Logs) (connector.Metrics, error) {
	return newConnector(cfg.(*Config), set, next)
}

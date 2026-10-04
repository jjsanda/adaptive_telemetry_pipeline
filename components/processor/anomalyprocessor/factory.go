package anomalyprocessor

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
)

const typeStr = "anomaly"

// NewFactory returns a factory for the anomaly processor (metrics only).
func NewFactory() processor.Factory {
	return processor.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		processor.WithMetrics(createMetrics, component.StabilityLevelAlpha),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		Alpha:         0.05,
		Threshold:     3.0,
		Warmup:        20,
		Shards:        16,
		StaleAfter:    5 * time.Minute,
		SweepInterval: 1 * time.Minute,
	}
}

func createMetrics(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Metrics) (processor.Metrics, error) {
	p, err := newProcessor(cfg.(*Config), set)
	if err != nil {
		return nil, err
	}
	return processorhelper.NewMetrics(ctx, set, cfg, next, p.processMetrics,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
		processorhelper.WithStart(p.start),
		processorhelper.WithShutdown(p.shutdown))
}

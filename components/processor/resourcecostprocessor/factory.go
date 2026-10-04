package resourcecostprocessor

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
)

const typeStr = "resourcecost"

// NewFactory returns a factory for the resourcecost processor, wired for all
// three signal types.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		processor.WithTraces(createTraces, component.StabilityLevelBeta),
		processor.WithMetrics(createMetrics, component.StabilityLevelBeta),
		processor.WithLogs(createLogs, component.StabilityLevelBeta),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		CostPerKB: 0.0004,
	}
}

func createTraces(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Traces) (processor.Traces, error) {
	e := newEnricher(cfg.(*Config))
	return processorhelper.NewTraces(ctx, set, cfg, next, e.processTraces,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}))
}

func createMetrics(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Metrics) (processor.Metrics, error) {
	e := newEnricher(cfg.(*Config))
	return processorhelper.NewMetrics(ctx, set, cfg, next, e.processMetrics,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}))
}

func createLogs(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Logs) (processor.Logs, error) {
	e := newEnricher(cfg.(*Config))
	return processorhelper.NewLogs(ctx, set, cfg, next, e.processLogs,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}))
}

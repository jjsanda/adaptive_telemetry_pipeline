package anomalyprocessor

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processortest"
)

func procSettings() processor.Settings {
	return processortest.NewNopSettings(component.MustNewType(typeStr))
}

func gaugeMetric(svc, name string, val float64) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", svc)
	m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName(name)
	m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(val)
	return md
}

func firstGaugeDP(md pmetric.Metrics) pmetric.NumberDataPoint {
	return md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Gauge().DataPoints().At(0)
}

func TestFlagsSpike(t *testing.T) {
	cfg := &Config{Alpha: 0.05, Threshold: 3, Warmup: 5, Shards: 4, StaleAfter: time.Minute}
	sink := new(consumertest.MetricsSink)
	p, err := createMetrics(context.Background(), procSettings(), cfg, sink)
	require.NoError(t, err)
	require.True(t, p.Capabilities().MutatesData)
	require.NoError(t, p.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	// Steady baseline through warmup, then a spike.
	for i := 0; i < 10; i++ {
		require.NoError(t, p.ConsumeMetrics(context.Background(), gaugeMetric("pricing", "red.latency.avg.ms", 100)))
	}
	require.NoError(t, p.ConsumeMetrics(context.Background(), gaugeMetric("pricing", "red.latency.avg.ms", 800)))

	all := sink.AllMetrics()
	dp := firstGaugeDP(all[len(all)-1])
	isAnom, ok := dp.Attributes().Get(attrIsAnomaly)
	require.True(t, ok)
	assert.True(t, isAnom.Bool(), "the spike must be flagged")
	score, ok := dp.Attributes().Get(attrScore)
	require.True(t, ok)
	assert.Greater(t, math.Abs(score.Double()), 3.0)

	// A steady point before the spike is scored but not flagged.
	steady := firstGaugeDP(all[3])
	notAnom, _ := steady.Attributes().Get(attrIsAnomaly)
	assert.False(t, notAnom.Bool())
}

func TestMonotonicSumSkipped(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	sink := new(consumertest.MetricsSink)
	p, err := createMetrics(context.Background(), procSettings(), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	md := pmetric.NewMetrics()
	m := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName("red.calls")
	s := m.SetEmptySum()
	s.SetIsMonotonic(true)
	s.DataPoints().AppendEmpty().SetIntValue(42)

	require.NoError(t, p.ConsumeMetrics(context.Background(), md))
	dp := sink.AllMetrics()[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Sum().DataPoints().At(0)
	_, ok := dp.Attributes().Get(attrScore)
	assert.False(t, ok, "monotonic counters must not be z-scored")
}

func TestConcurrentProcess(t *testing.T) {
	cfg := &Config{Alpha: 0.05, Threshold: 3, Warmup: 20, Shards: 8, StaleAfter: time.Minute, SweepInterval: 5 * time.Millisecond}
	sink := new(consumertest.MetricsSink)
	p, err := createMetrics(context.Background(), procSettings(), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = p.ConsumeMetrics(context.Background(), gaugeMetric("svc", "m", float64(i)))
			}
		}(g)
	}
	wg.Wait()
}

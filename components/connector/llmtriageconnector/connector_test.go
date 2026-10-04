package llmtriageconnector

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/connector/connectortest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/jjsanda/adaptive_telemetry_pipeline/components/internal/llm"
)

// fakeLLM is the injected test backend — deterministic, instrumented, and able
// to simulate latency and failure. It is why the connector's tests are fast and
// hermetic (no network, no key).
type fakeLLM struct {
	mu    sync.Mutex
	calls int
	delay time.Duration
	sum   llm.Summary
	err   error
}

func (f *fakeLLM) Name() string { return "fake" }
func (f *fakeLLM) Triage(ctx context.Context, _ llm.Request) (llm.Summary, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return llm.Summary{}, ctx.Err()
		}
	}
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.sum, f.err
}
func (f *fakeLLM) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

func anomalyMetric(svc string, score float64) pmetric.Metrics {
	md := pmetric.NewMetrics()
	m := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName("red.latency.avg.ms")
	dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.SetDoubleValue(800)
	dp.Attributes().PutStr("service.name", svc)
	dp.Attributes().PutBool("anomaly.is_anomaly", true)
	dp.Attributes().PutDouble("anomaly.score", score)
	return md
}

func newTestConn(t *testing.T, cfg *Config, sink *consumertest.LogsSink, fake *fakeLLM) *triageConn {
	t.Helper()
	c, err := newConnector(cfg, connectortest.NewNopSettings(component.MustNewType(typeStr)), sink)
	require.NoError(t, err)
	if fake != nil {
		c.client = fake
	}
	return c
}

func baseCfg() *Config {
	c := createDefaultConfig().(*Config)
	c.MaxWait = 20 * time.Millisecond
	c.MaxBatch = 5
	c.DebounceWindow = 0
	return c
}

func TestEmitsCorrelatedSummary(t *testing.T) {
	sink := new(consumertest.LogsSink)
	fake := &fakeLLM{sum: llm.Summary{Title: "pricing latency anomaly", Severity: "critical",
		AffectedService: "pricing", ProbableCause: "cause", SuggestedNextStep: "look here", Tokens: 42}}
	c := newTestConn(t, baseCfg(), sink, fake)
	require.NoError(t, c.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = c.Shutdown(context.Background()) })

	require.NoError(t, c.ConsumeMetrics(context.Background(), anomalyMetric("pricing", 6.2)))
	require.Eventually(t, func() bool { return len(sink.AllLogs()) > 0 }, time.Second, 5*time.Millisecond)

	lr := sink.AllLogs()[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, "CRITICAL", lr.SeverityText())
	title, ok := lr.Attributes().Get("triage.title")
	require.True(t, ok)
	assert.Equal(t, "pricing latency anomaly", title.Str())
	eng, _ := lr.Attributes().Get("triage.engine")
	assert.Equal(t, "fake", eng.Str())
}

// TestConsumeNeverBlocks proves the backpressure boundary: even with a slow LLM,
// ConsumeMetrics returns effectively immediately.
func TestConsumeNeverBlocks(t *testing.T) {
	sink := new(consumertest.LogsSink)
	fake := &fakeLLM{delay: 300 * time.Millisecond, sum: llm.Summary{Title: "x"}}
	c := newTestConn(t, baseCfg(), sink, fake)
	require.NoError(t, c.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = c.Shutdown(context.Background()) })

	start := time.Now()
	require.NoError(t, c.ConsumeMetrics(context.Background(), anomalyMetric("pricing", 6)))
	assert.Less(t, time.Since(start), 50*time.Millisecond, "consume path must not wait on the LLM")
}

// TestLoadSheds proves the queue drops rather than blocks when full.
func TestLoadSheds(t *testing.T) {
	cfg := baseCfg()
	cfg.QueueSize = 1
	sink := new(consumertest.LogsSink)
	c := newTestConn(t, cfg, sink, &fakeLLM{}) // NOT started: nothing drains the queue
	for i := 0; i < 20; i++ {
		require.NoError(t, c.ConsumeMetrics(context.Background(), anomalyMetric("s", 6)))
	}
	assert.Equal(t, 1, len(c.queue), "only queue capacity is retained; the rest are shed")
}

func TestDebounceSuppressesDuplicates(t *testing.T) {
	cfg := baseCfg()
	cfg.MaxBatch = 1
	cfg.DebounceWindow = time.Hour
	sink := new(consumertest.LogsSink)
	fake := &fakeLLM{sum: llm.Summary{Title: "t", Severity: "warning"}}
	c := newTestConn(t, cfg, sink, fake)
	require.NoError(t, c.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = c.Shutdown(context.Background()) })

	require.NoError(t, c.ConsumeMetrics(context.Background(), anomalyMetric("pricing", 6)))
	require.NoError(t, c.ConsumeMetrics(context.Background(), anomalyMetric("pricing", 6)))
	require.Eventually(t, func() bool { return len(sink.AllLogs()) >= 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, 1, fake.count(), "the duplicate (service, metric) is debounced")
	assert.Equal(t, 1, len(sink.AllLogs()))
}

func TestExtractIgnoresNonAnomalies(t *testing.T) {
	md := pmetric.NewMetrics()
	m := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName("red.latency.avg.ms")
	m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(100) // no anomaly flag
	assert.Empty(t, extractAnomalies(md))
}

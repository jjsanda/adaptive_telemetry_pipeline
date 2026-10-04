package resourcecostprocessor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processortest"
)

func testCfg() *Config {
	return &Config{
		TeamByService: map[string]string{"orders": "payments-team"},
		DefaultTeam:   "platform",
		CostPerKB:     0.5, // high rate so the proxy is clearly > 0 on tiny fixtures
	}
}

func settings() processor.Settings {
	return processortest.NewNopSettings(component.MustNewType(typeStr))
}

func tracesWith(svc string) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	if svc != "" {
		rs.Resource().Attributes().PutStr(attrServiceName, svc)
	}
	rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName("op")
	return td
}

func metricsWith(svc string) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	if svc != "" {
		rm.Resource().Attributes().PutStr(attrServiceName, svc)
	}
	m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName("requests")
	m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(1)
	return md
}

func logsWith(svc string) plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	if svc != "" {
		rl.Resource().Attributes().PutStr(attrServiceName, svc)
	}
	rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("hello")
	return ld
}

func assertEnriched(t *testing.T, attrs pcommon.Map, wantTeam string, wantCost bool) {
	t.Helper()
	team, ok := attrs.Get(attrOwnerTeam)
	require.True(t, ok, "expected %s", attrOwnerTeam)
	assert.Equal(t, wantTeam, team.Str())

	cost, ok := attrs.Get(attrCostEstimate)
	if wantCost {
		require.True(t, ok, "expected %s", attrCostEstimate)
		assert.Greater(t, cost.Double(), 0.0)
	} else {
		assert.False(t, ok, "did not expect %s", attrCostEstimate)
	}
}

func TestTracesEnriched(t *testing.T) {
	sink := new(consumertest.TracesSink)
	p, err := createTraces(context.Background(), settings(), testCfg(), sink)
	require.NoError(t, err)
	assert.True(t, p.Capabilities().MutatesData)

	require.NoError(t, p.ConsumeTraces(context.Background(), tracesWith("orders")))
	got := sink.AllTraces()
	require.Len(t, got, 1)
	assertEnriched(t, got[0].ResourceSpans().At(0).Resource().Attributes(), "payments-team", true)
}

func TestMetricsEnriched(t *testing.T) {
	sink := new(consumertest.MetricsSink)
	p, err := createMetrics(context.Background(), settings(), testCfg(), sink)
	require.NoError(t, err)
	assert.True(t, p.Capabilities().MutatesData)

	require.NoError(t, p.ConsumeMetrics(context.Background(), metricsWith("orders")))
	got := sink.AllMetrics()
	require.Len(t, got, 1)
	assertEnriched(t, got[0].ResourceMetrics().At(0).Resource().Attributes(), "payments-team", true)
}

func TestLogsEnriched(t *testing.T) {
	sink := new(consumertest.LogsSink)
	p, err := createLogs(context.Background(), settings(), testCfg(), sink)
	require.NoError(t, err)
	assert.True(t, p.Capabilities().MutatesData)

	require.NoError(t, p.ConsumeLogs(context.Background(), logsWith("orders")))
	got := sink.AllLogs()
	require.Len(t, got, 1)
	assertEnriched(t, got[0].ResourceLogs().At(0).Resource().Attributes(), "payments-team", true)
}

func TestDefaultTeamFallback(t *testing.T) {
	sink := new(consumertest.TracesSink)
	p, err := createTraces(context.Background(), settings(), testCfg(), sink)
	require.NoError(t, err)
	require.NoError(t, p.ConsumeTraces(context.Background(), tracesWith("unmapped-service")))
	attrs := sink.AllTraces()[0].ResourceSpans().At(0).Resource().Attributes()
	assertEnriched(t, attrs, "platform", true) // falls back to DefaultTeam
}

func TestCostDisabled(t *testing.T) {
	cfg := testCfg()
	cfg.CostPerKB = 0
	sink := new(consumertest.TracesSink)
	p, err := createTraces(context.Background(), settings(), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, p.ConsumeTraces(context.Background(), tracesWith("orders")))
	attrs := sink.AllTraces()[0].ResourceSpans().At(0).Resource().Attributes()
	assertEnriched(t, attrs, "payments-team", false) // no cost attribute when disabled
}

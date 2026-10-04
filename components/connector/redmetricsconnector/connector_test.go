package redmetricsconnector

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/connector/connectortest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type spanSpec struct {
	durMs int
	isErr bool
}

func tracesFor(svc string, spans ...spanSpec) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", svc)
	ss := rs.ScopeSpans().AppendEmpty().Spans()
	base := time.Now()
	for _, sp := range spans {
		s := ss.AppendEmpty()
		s.SetName("op")
		s.SetKind(ptrace.SpanKindServer)
		s.SetStartTimestamp(pcommon.NewTimestampFromTime(base))
		s.SetEndTimestamp(pcommon.NewTimestampFromTime(base.Add(time.Duration(sp.durMs) * time.Millisecond)))
		if sp.isErr {
			s.Status().SetCode(ptrace.StatusCodeError)
		}
	}
	return td
}

func findMetric(md pmetric.Metrics, name string) (pmetric.Metric, bool) {
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		sms := rms.At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				if ms.At(k).Name() == name {
					return ms.At(k), true
				}
			}
		}
	}
	return pmetric.Metric{}, false
}

func newTestConn(t *testing.T, cfg *Config, sink *consumertest.MetricsSink) connector.Traces {
	t.Helper()
	conn, err := createTracesToMetrics(context.Background(), connectortest.NewNopSettings(component.MustNewType(typeStr)), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, conn.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = conn.Shutdown(context.Background()) })
	return conn
}

func syncCfg() *Config {
	return &Config{
		Dimensions:         []string{"service.name"},
		HistogramBucketsMs: []float64{10, 50, 100, 500},
		FlushInterval:      0, // flush synchronously on each ConsumeTraces
		MaxSeries:          100,
		Namespace:          "red",
	}
}

func TestCountsAndErrors(t *testing.T) {
	sink := new(consumertest.MetricsSink)
	conn := newTestConn(t, syncCfg(), sink)

	require.NoError(t, conn.ConsumeTraces(context.Background(),
		tracesFor("orders", spanSpec{20, false}, spanSpec{20, false}, spanSpec{20, false}, spanSpec{20, true}, spanSpec{20, true})))

	mds := sink.AllMetrics()
	require.NotEmpty(t, mds)
	last := mds[len(mds)-1]

	calls, ok := findMetric(last, "red.calls")
	require.True(t, ok)
	assert.Equal(t, int64(5), calls.Sum().DataPoints().At(0).IntValue())
	assert.Equal(t, pmetric.AggregationTemporalityCumulative, calls.Sum().AggregationTemporality())

	errs, ok := findMetric(last, "red.errors")
	require.True(t, ok)
	assert.Equal(t, int64(2), errs.Sum().DataPoints().At(0).IntValue())
}

func TestHistogramBucketPlacement(t *testing.T) {
	sink := new(consumertest.MetricsSink)
	conn := newTestConn(t, syncCfg(), sink)

	// bounds [10,50,100,500] -> 5 buckets. Durations land in buckets 0,1,2,4.
	require.NoError(t, conn.ConsumeTraces(context.Background(),
		tracesFor("orders", spanSpec{5, false}, spanSpec{30, false}, spanSpec{75, false}, spanSpec{600, false})))

	hist, ok := findMetric(sink.AllMetrics()[0], "red.duration.ms")
	require.True(t, ok)
	dp := hist.Histogram().DataPoints().At(0)
	assert.Equal(t, uint64(4), dp.Count())
	assert.InDelta(t, 710.0, dp.Sum(), 1e-6)
	got := dp.BucketCounts().AsRaw()
	assert.Equal(t, []uint64{1, 1, 1, 0, 1}, got, "one value in each of buckets 0,1,2 and one above the top bound")
}

func TestLatencyGauge(t *testing.T) {
	sink := new(consumertest.MetricsSink)
	conn := newTestConn(t, syncCfg(), sink)

	require.NoError(t, conn.ConsumeTraces(context.Background(),
		tracesFor("orders", spanSpec{20, false}, spanSpec{40, false}))) // avg = 30ms

	g, ok := findMetric(sink.AllMetrics()[0], "red.latency.avg.ms")
	require.True(t, ok)
	assert.InDelta(t, 30.0, g.Gauge().DataPoints().At(0).DoubleValue(), 1e-6)
}

func TestMaxSeriesCap(t *testing.T) {
	cfg := syncCfg()
	cfg.MaxSeries = 1
	sink := new(consumertest.MetricsSink)
	conn := newTestConn(t, cfg, sink)

	td := tracesFor("orders", spanSpec{20, false})
	// second resource -> a distinct series that must be dropped by the cap.
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "checkout")
	s := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	s.SetKind(ptrace.SpanKindServer)

	require.NoError(t, conn.ConsumeTraces(context.Background(), td))
	calls, ok := findMetric(sink.AllMetrics()[0], "red.calls")
	require.True(t, ok)
	assert.Equal(t, 1, calls.Sum().DataPoints().Len(), "cardinality cap keeps only one series")
}

// TestConcurrent exercises the mutex-guarded state under the race detector with
// a live ticker flushing while many goroutines record spans.
func TestConcurrent(t *testing.T) {
	cfg := syncCfg()
	cfg.FlushInterval = 5 * time.Millisecond
	sink := new(consumertest.MetricsSink)
	conn := newTestConn(t, cfg, sink)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = conn.ConsumeTraces(context.Background(), tracesFor("orders", spanSpec{15, false}))
			}
		}()
	}
	wg.Wait()
	time.Sleep(15 * time.Millisecond) // let a flush land
	require.NotEmpty(t, sink.AllMetrics())
}

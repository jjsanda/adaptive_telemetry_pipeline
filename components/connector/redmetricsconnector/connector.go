package redmetricsconnector

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

// seriesState is the accumulated cumulative state for one dimension combination.
type seriesState struct {
	attrs        pcommon.Map
	calls        uint64
	errors       uint64
	sumMs        float64
	bucketCounts []uint64
	// snapshots taken at the previous flush, used to derive the per-interval
	// average-latency gauge.
	lastCalls uint64
	lastSumMs float64
}

// redConnector aggregates spans into RED metrics. It is the exporter side of a
// traces pipeline and the receiver side of a metrics pipeline. State is guarded
// by a mutex because ConsumeTraces is called concurrently while a background
// ticker flushes.
type redConnector struct {
	cfg    *Config
	next   consumer.Metrics
	logger *zap.Logger
	bounds []float64

	mu      sync.Mutex
	series  map[string]*seriesState
	dropped uint64
	start   pcommon.Timestamp

	ticker *time.Ticker
	stop   chan struct{}
	wg     sync.WaitGroup
}

func newConnector(cfg *Config, set connector.Settings, next consumer.Metrics) *redConnector {
	return &redConnector{
		cfg:    cfg,
		next:   next,
		logger: set.Logger,
		bounds: cfg.HistogramBucketsMs,
		series: make(map[string]*seriesState),
	}
}

// Capabilities: the connector reads spans and produces brand-new metrics, so it
// does not mutate its input.
func (c *redConnector) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (c *redConnector) Start(_ context.Context, _ component.Host) error {
	c.start = pcommon.NewTimestampFromTime(time.Now())
	if c.cfg.FlushInterval > 0 {
		c.ticker = time.NewTicker(c.cfg.FlushInterval)
		c.stop = make(chan struct{})
		c.wg.Add(1)
		go c.flushLoop()
	}
	return nil
}

func (c *redConnector) Shutdown(ctx context.Context) error {
	if c.ticker != nil {
		c.ticker.Stop()
		close(c.stop)
		c.wg.Wait()
	}
	return c.flush(ctx) // final flush
}

func (c *redConnector) flushLoop() {
	defer c.wg.Done()
	for {
		select {
		case <-c.stop:
			return
		case <-c.ticker.C:
			if err := c.flush(context.Background()); err != nil {
				c.logger.Warn("redmetrics flush failed", zap.Error(err))
			}
		}
	}
}

func (c *redConnector) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	c.mu.Lock()
	c.aggregate(td)
	c.mu.Unlock()
	// When flushing is disabled we emit synchronously (handy for tests).
	if c.cfg.FlushInterval <= 0 {
		return c.flush(ctx)
	}
	return nil
}

func (c *redConnector) aggregate(td ptrace.Traces) {
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		res := rss.At(i).Resource()
		sss := rss.At(i).ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			spans := sss.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				c.record(res, spans.At(k))
			}
		}
	}
}

func (c *redConnector) record(res pcommon.Resource, span ptrace.Span) {
	key, attrs := c.dimensions(res, span)
	s := c.series[key]
	if s == nil {
		if c.cfg.MaxSeries > 0 && len(c.series) >= c.cfg.MaxSeries {
			c.dropped++
			return
		}
		s = &seriesState{attrs: attrs, bucketCounts: make([]uint64, len(c.bounds)+1)}
		c.series[key] = s
	}
	s.calls++
	if span.Status().Code() == ptrace.StatusCodeError {
		s.errors++
	}
	durMs := float64(span.EndTimestamp().AsTime().Sub(span.StartTimestamp().AsTime()).Nanoseconds()) / 1e6
	if durMs < 0 {
		durMs = 0
	}
	s.sumMs += durMs
	// bucket i covers (bounds[i-1], bounds[i]]; SearchFloat64s returns the first
	// index whose bound is >= durMs, which is exactly that bucket.
	s.bucketCounts[sort.SearchFloat64s(c.bounds, durMs)]++
}

// dimensions builds the series key and the owned attribute map from the
// configured dimension names.
func (c *redConnector) dimensions(res pcommon.Resource, span ptrace.Span) (string, pcommon.Map) {
	attrs := pcommon.NewMap()
	var b strings.Builder
	for _, dim := range c.cfg.Dimensions {
		v := resolveDimension(res, span, dim)
		attrs.PutStr(dim, v)
		b.WriteString(dim)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('|')
	}
	return b.String(), attrs
}

func resolveDimension(res pcommon.Resource, span ptrace.Span, name string) string {
	switch name {
	case "span.kind":
		return span.Kind().String()
	case "status.code":
		return span.Status().Code().String()
	case "span.name":
		return span.Name()
	}
	if v, ok := span.Attributes().Get(name); ok {
		return v.AsString()
	}
	if v, ok := res.Attributes().Get(name); ok {
		return v.AsString()
	}
	return ""
}

func (c *redConnector) flush(ctx context.Context) error {
	c.mu.Lock()
	if len(c.series) == 0 {
		c.mu.Unlock()
		return nil
	}
	now := pcommon.NewTimestampFromTime(time.Now())
	md := c.buildMetrics(now)
	for _, s := range c.series {
		s.lastCalls = s.calls
		s.lastSumMs = s.sumMs
	}
	dropped := c.dropped
	c.mu.Unlock()

	if dropped > 0 {
		c.logger.Warn("redmetrics dropped series beyond max_series",
			zap.Uint64("dropped", dropped), zap.Int("max_series", c.cfg.MaxSeries))
	}
	return c.next.ConsumeMetrics(ctx, md)
}

// buildMetrics must be called with c.mu held. It emits the cumulative RED
// metrics plus a per-interval average-latency gauge that is specifically shaped
// for streaming anomaly detection downstream (a single scalar per series per
// interval, rather than a cumulative histogram).
func (c *redConnector) buildMetrics(now pcommon.Timestamp) pmetric.Metrics {
	md := pmetric.NewMetrics()
	sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()
	sm.Scope().SetName("github.com/jjsanda/adaptive_telemetry_pipeline/components/connector/redmetricsconnector")

	ns := c.cfg.Namespace
	calls := newCumulativeSum(sm, ns+".calls", true)
	errs := newCumulativeSum(sm, ns+".errors", true)

	durM := sm.Metrics().AppendEmpty()
	durM.SetName(ns + ".duration.ms")
	durM.SetUnit("ms")
	hist := durM.SetEmptyHistogram()
	hist.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)

	latM := sm.Metrics().AppendEmpty()
	latM.SetName(ns + ".latency.avg.ms")
	latM.SetUnit("ms")
	gauge := latM.SetEmptyGauge()

	for _, s := range c.series {
		cdp := calls.DataPoints().AppendEmpty()
		cdp.SetStartTimestamp(c.start)
		cdp.SetTimestamp(now)
		cdp.SetIntValue(int64(s.calls))
		s.attrs.CopyTo(cdp.Attributes())

		edp := errs.DataPoints().AppendEmpty()
		edp.SetStartTimestamp(c.start)
		edp.SetTimestamp(now)
		edp.SetIntValue(int64(s.errors))
		s.attrs.CopyTo(edp.Attributes())

		hdp := hist.DataPoints().AppendEmpty()
		hdp.SetStartTimestamp(c.start)
		hdp.SetTimestamp(now)
		hdp.ExplicitBounds().FromRaw(c.bounds)
		// Copy the bucket counts before handing them off: this series' state
		// keeps mutating on later spans, so we must not share the backing array
		// with a metric that has already been emitted downstream.
		hdp.BucketCounts().FromRaw(append([]uint64(nil), s.bucketCounts...))
		hdp.SetCount(s.calls)
		hdp.SetSum(s.sumMs)
		s.attrs.CopyTo(hdp.Attributes())

		intervalCalls := s.calls - s.lastCalls
		var avg float64
		if intervalCalls > 0 {
			avg = (s.sumMs - s.lastSumMs) / float64(intervalCalls)
		}
		gdp := gauge.DataPoints().AppendEmpty()
		gdp.SetTimestamp(now)
		gdp.SetDoubleValue(avg)
		s.attrs.CopyTo(gdp.Attributes())
	}
	return md
}

func newCumulativeSum(sm pmetric.ScopeMetrics, name string, monotonic bool) pmetric.Sum {
	m := sm.Metrics().AppendEmpty()
	m.SetName(name)
	sum := m.SetEmptySum()
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	sum.SetIsMonotonic(monotonic)
	return sum
}

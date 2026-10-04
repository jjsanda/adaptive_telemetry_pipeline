package anomalyprocessor

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/jjsanda/adaptive_telemetry_pipeline/components/internal/anomaly"
)

const (
	attrScore     = "anomaly.score"
	attrIsAnomaly = "anomaly.is_anomaly"
	scopeName     = "github.com/jjsanda/adaptive_telemetry_pipeline/components/processor/anomalyprocessor"
)

type anomalyProc struct {
	cfg    *Config
	logger *zap.Logger
	store  *store

	anomalies metric.Int64Counter // self-observability

	stop chan struct{}
	wg   sync.WaitGroup
}

func newProcessor(cfg *Config, set processor.Settings) (*anomalyProc, error) {
	newDet := func() *anomaly.Detector {
		return anomaly.NewDetector(cfg.Alpha, cfg.Threshold, cfg.Warmup)
	}
	p := &anomalyProc{
		cfg:    cfg,
		logger: set.Logger,
		store:  newStore(cfg.Shards, newDet, cfg.StaleAfter),
	}

	meter := set.MeterProvider.Meter(scopeName)
	var err error
	if p.anomalies, err = meter.Int64Counter("anomaly.flagged",
		metric.WithDescription("Data points flagged anomalous")); err != nil {
		return nil, err
	}
	// An observable gauge of live series proves the sweeper keeps memory bounded.
	if _, err = meter.Int64ObservableGauge("anomaly.series.tracked",
		metric.WithDescription("Live series currently tracked by the detector"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(p.store.size()))
			return nil
		})); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *anomalyProc) start(_ context.Context, _ component.Host) error {
	if p.cfg.SweepInterval > 0 && p.cfg.StaleAfter > 0 {
		p.stop = make(chan struct{})
		p.wg.Add(1)
		go p.sweepLoop()
	}
	return nil
}

func (p *anomalyProc) sweepLoop() {
	defer p.wg.Done()
	t := time.NewTicker(p.cfg.SweepInterval)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			if n := p.store.sweep(); n > 0 {
				p.logger.Debug("anomaly swept stale series",
					zap.Int("evicted", n), zap.Int("remaining", p.store.size()))
			}
		}
	}
}

func (p *anomalyProc) shutdown(_ context.Context) error {
	if p.stop != nil {
		close(p.stop)
		p.wg.Wait()
	}
	return nil
}

// processMetrics scores every scalar (Gauge / non-monotonic Sum) data point.
// Histograms, summaries, and monotonic cumulative counters are passed through
// untouched — a z-score on a value that only ever climbs is meaningless.
func (p *anomalyProc) processMetrics(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		resKey := attrsKey(rms.At(i).Resource().Attributes())
		sms := rms.At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				m := ms.At(k)
				switch m.Type() {
				case pmetric.MetricTypeGauge:
					p.score(ctx, resKey, m.Name(), m.Gauge().DataPoints())
				case pmetric.MetricTypeSum:
					if !m.Sum().IsMonotonic() {
						p.score(ctx, resKey, m.Name(), m.Sum().DataPoints())
					}
				}
			}
		}
	}
	return md, nil
}

func (p *anomalyProc) score(ctx context.Context, resKey, name string, dps pmetric.NumberDataPointSlice) {
	for i := 0; i < dps.Len(); i++ {
		dp := dps.At(i)
		key := resKey + "\x1f" + name + "\x1f" + attrsKey(dp.Attributes())
		z, anom := p.store.detect(key, numberValue(dp))
		dp.Attributes().PutDouble(attrScore, round3(z))
		dp.Attributes().PutBool(attrIsAnomaly, anom)
		if anom {
			p.anomalies.Add(ctx, 1)
		}
	}
}

func numberValue(dp pmetric.NumberDataPoint) float64 {
	switch dp.ValueType() {
	case pmetric.NumberDataPointValueTypeDouble:
		return dp.DoubleValue()
	case pmetric.NumberDataPointValueTypeInt:
		return float64(dp.IntValue())
	default:
		return 0
	}
}

// attrsKey builds a stable, order-independent identity string from an attribute
// map — part of a series' identity along with resource attrs and metric name.
func attrsKey(m pcommon.Map) string {
	if m.Len() == 0 {
		return ""
	}
	pairs := make([]string, 0, m.Len())
	for k, v := range m.All() {
		pairs = append(pairs, k+"="+v.AsString())
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

func round3(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return math.Round(f*1000) / 1000
}

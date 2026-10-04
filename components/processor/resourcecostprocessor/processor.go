package resourcecostprocessor

import (
	"context"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

const (
	attrOwnerTeam    = "telemetry.owner.team"
	attrCostEstimate = "telemetry.cost.estimate"
	attrServiceName  = "service.name"
)

// enricher holds the config and the proto sizers used for the cost proxy. The
// sizers are cheap, reusable value types.
type enricher struct {
	cfg          *Config
	tracesSizer  ptrace.Sizer
	metricsSizer pmetric.Sizer
	logsSizer    plog.Sizer
}

func newEnricher(cfg *Config) *enricher {
	return &enricher{
		cfg:          cfg,
		tracesSizer:  &ptrace.ProtoMarshaler{},
		metricsSizer: &pmetric.ProtoMarshaler{},
		logsSizer:    &plog.ProtoMarshaler{},
	}
}

// We declare MutatesData: true (see factory) precisely because these methods
// write new resource attributes in place. That flag forces the pipeline to hand
// us an owned copy at fan-out; getting it wrong would risk a data race with any
// other consumer reading the same batch.

func (e *enricher) processTraces(_ context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		e.enrichResource(rs.Resource(), func() int {
			if e.cfg.CostPerKB <= 0 {
				return 0
			}
			tmp := ptrace.NewTraces()
			rs.CopyTo(tmp.ResourceSpans().AppendEmpty())
			return e.tracesSizer.TracesSize(tmp)
		})
	}
	return td, nil
}

func (e *enricher) processMetrics(_ context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		e.enrichResource(rm.Resource(), func() int {
			if e.cfg.CostPerKB <= 0 {
				return 0
			}
			tmp := pmetric.NewMetrics()
			rm.CopyTo(tmp.ResourceMetrics().AppendEmpty())
			return e.metricsSizer.MetricsSize(tmp)
		})
	}
	return md, nil
}

func (e *enricher) processLogs(_ context.Context, ld plog.Logs) (plog.Logs, error) {
	rls := ld.ResourceLogs()
	for i := 0; i < rls.Len(); i++ {
		rl := rls.At(i)
		e.enrichResource(rl.Resource(), func() int {
			if e.cfg.CostPerKB <= 0 {
				return 0
			}
			tmp := plog.NewLogs()
			rl.CopyTo(tmp.ResourceLogs().AppendEmpty())
			return e.logsSizer.LogsSize(tmp)
		})
	}
	return ld, nil
}

// enrichResource attaches the owner and cost attributes to a single resource.
// sizeOf is lazy so the (comparatively expensive) marshal only happens when a
// cost estimate is actually being produced.
func (e *enricher) enrichResource(res pcommon.Resource, sizeOf func() int) {
	attrs := res.Attributes()

	team := e.cfg.DefaultTeam
	if svc, ok := attrs.Get(attrServiceName); ok {
		if t, ok := e.cfg.TeamByService[svc.Str()]; ok {
			team = t
		}
	}
	if team != "" {
		attrs.PutStr(attrOwnerTeam, team)
	}

	if e.cfg.CostPerKB > 0 {
		cost := float64(sizeOf()) / 1024.0 * e.cfg.CostPerKB
		attrs.PutDouble(attrCostEstimate, cost)
	}
}

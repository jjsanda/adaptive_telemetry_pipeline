package llmtriageconnector

import (
	"errors"

	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/jjsanda/adaptive_telemetry_pipeline/components/internal/llm"
)

var errInvalidTraceID = errors.New("invalid trace id")

// extractAnomalies pulls flagged data points (anomaly.is_anomaly == true) out of
// a metrics batch. It tolerates service.name living on either the resource or,
// as the redmetrics connector emits it, on the data point itself.
func extractAnomalies(md pmetric.Metrics) []llm.Anomaly {
	var out []llm.Anomaly
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		resSvc := ""
		if v, ok := rms.At(i).Resource().Attributes().Get("service.name"); ok {
			resSvc = v.Str()
		}
		sms := rms.At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				m := ms.At(k)
				switch m.Type() {
				case pmetric.MetricTypeGauge:
					collect(&out, resSvc, m.Name(), m.Gauge().DataPoints())
				case pmetric.MetricTypeSum:
					collect(&out, resSvc, m.Name(), m.Sum().DataPoints())
				}
			}
		}
	}
	return out
}

func collect(out *[]llm.Anomaly, resSvc, name string, dps pmetric.NumberDataPointSlice) {
	for i := 0; i < dps.Len(); i++ {
		dp := dps.At(i)
		flag, ok := dp.Attributes().Get("anomaly.is_anomaly")
		if !ok || !flag.Bool() {
			continue
		}
		a := llm.Anomaly{
			Service:    resSvc,
			Metric:     name,
			Value:      numberValue(dp),
			Timestamp:  dp.Timestamp().AsTime(),
			Attributes: map[string]string{},
		}
		for k, v := range dp.Attributes().All() {
			a.Attributes[k] = v.AsString()
		}
		if a.Service == "" {
			a.Service = a.Attributes["service.name"]
		}
		if s, ok := dp.Attributes().Get("anomaly.score"); ok {
			a.Score = s.Double()
		}
		a.TraceID = a.Attributes["trace_id"]
		*out = append(*out, a)
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

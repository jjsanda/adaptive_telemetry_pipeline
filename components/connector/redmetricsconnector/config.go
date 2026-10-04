package redmetricsconnector

import (
	"errors"
	"time"
)

// Config controls the redmetrics connector, which turns trace spans into RED
// (Rate, Errors, Duration) metrics.
type Config struct {
	// Dimensions are the attribute names used to key each output series. They
	// may be resource attributes (e.g. service.name) or span-level values
	// (span.kind, status.code, or any span attribute such as http.route).
	Dimensions []string `mapstructure:"dimensions"`

	// HistogramBucketsMs are the explicit upper bounds (milliseconds) for the
	// duration histogram. Must be strictly increasing.
	HistogramBucketsMs []float64 `mapstructure:"histogram_buckets_ms"`

	// FlushInterval is how often accumulated metrics are emitted downstream.
	// Cumulative counters keep growing across flushes; the per-interval latency
	// gauge is computed from the delta since the previous flush.
	FlushInterval time.Duration `mapstructure:"flush_interval"`

	// MaxSeries caps distinct dimension combinations to bound memory and protect
	// the metrics store from cardinality explosion (e.g. an un-normalized URL
	// used as a dimension). New series beyond the cap are dropped and counted.
	// 0 means unbounded (not recommended in production).
	MaxSeries int `mapstructure:"max_series"`

	// Namespace prefixes every emitted metric name (default "red").
	Namespace string `mapstructure:"namespace"`
}

func (c *Config) Validate() error {
	if c.MaxSeries < 0 {
		return errors.New("max_series must be >= 0")
	}
	if c.FlushInterval < 0 {
		return errors.New("flush_interval must be >= 0")
	}
	for i := 1; i < len(c.HistogramBucketsMs); i++ {
		if c.HistogramBucketsMs[i] <= c.HistogramBucketsMs[i-1] {
			return errors.New("histogram_buckets_ms must be strictly increasing")
		}
	}
	return nil
}

// Package llm defines the small, pluggable triage-engine interface used by the
// llmtriage connector, plus two implementations: a deterministic offline engine
// (the default — zero API keys, reproducible) and an optional Anthropic (Claude)
// backend. Keeping the backend behind an interface is what makes the connector
// testable with a fake and safe to run in CI.
package llm

import (
	"context"
	"time"
)

// Anomaly is one flagged metric data point, extracted from the telemetry stream.
type Anomaly struct {
	Service    string
	Metric     string
	Value      float64
	Score      float64
	TraceID    string
	Attributes map[string]string
	Timestamp  time.Time
}

// Request is a batch of correlated anomalies to triage together.
type Request struct {
	Anomalies []Anomaly
}

// Summary is the structured triage result. The text fields are what an on-call
// engineer reads; Tokens is bookkeeping the connector turns into a metric.
type Summary struct {
	Title             string `json:"title"`
	Severity          string `json:"severity"` // info | warning | critical
	AffectedService   string `json:"affected_service"`
	ProbableCause     string `json:"probable_cause"`
	SuggestedNextStep string `json:"suggested_next_step"`
	Tokens            int    `json:"-"`
}

// Client is a triage backend.
type Client interface {
	Triage(ctx context.Context, req Request) (Summary, error)
	Name() string
}

// validSeverities is the closed set we accept from any backend (LLM output is
// untrusted and must be validated before it becomes a log record).
var validSeverities = map[string]bool{"info": true, "warning": true, "critical": true}

func normalizeSeverity(s string) string {
	if validSeverities[s] {
		return s
	}
	return "warning"
}

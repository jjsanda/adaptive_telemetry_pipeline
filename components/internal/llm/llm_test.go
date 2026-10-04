package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeterministicTriage(t *testing.T) {
	sum, err := Deterministic{}.Triage(context.Background(), Request{Anomalies: []Anomaly{
		{Service: "inventory", Metric: "red.latency.avg.ms", Value: 120, Score: 3.4},
		{Service: "pricing", Metric: "red.latency.avg.ms", Value: 850, Score: 6.1, TraceID: "abc123"},
	}})
	require.NoError(t, err)
	assert.Equal(t, "pricing", sum.AffectedService, "picks the strongest signal")
	assert.Equal(t, "critical", sum.Severity, "|z| >= 5 is critical")
	assert.Contains(t, sum.SuggestedNextStep, "abc123")
}

func TestDeterministicEmptyBatch(t *testing.T) {
	_, err := Deterministic{}.Triage(context.Background(), Request{})
	assert.Error(t, err)
}

func TestParseSummary(t *testing.T) {
	// Clean JSON.
	s, err := parseSummary(`{"title":"t","severity":"critical","affected_service":"pricing","probable_cause":"c","suggested_next_step":"s"}`)
	require.NoError(t, err)
	assert.Equal(t, "critical", s.Severity)

	// JSON wrapped in prose/markdown — must still be extracted.
	s, err = parseSummary("Here you go:\n```json\n{\"title\":\"t\",\"severity\":\"bogus\"}\n```")
	require.NoError(t, err)
	assert.Equal(t, "warning", s.Severity, "invalid severity is normalized")

	// Not JSON at all — rejected.
	_, err = parseSummary("I cannot help with that")
	assert.Error(t, err)

	// Missing title — rejected.
	_, err = parseSummary(`{"severity":"info"}`)
	assert.Error(t, err)
}

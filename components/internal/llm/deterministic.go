package llm

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// Deterministic is an offline triage engine. It is the DEFAULT backend so the
// entire pipeline runs with zero API keys and produces reproducible output —
// which is exactly what keeps the demo and the tests hermetic. It also stands
// in as the "fake" in unit tests.
//
// The heuristic is deliberately simple and honest: it foregrounds the
// strongest signal in the batch and points the on-call at the obvious next
// step. It is where an LLM adds value (natural-language summarization and
// correlation) without pretending an LLM is doing the detection — that is the
// deterministic anomaly processor's job.
type Deterministic struct{}

func (Deterministic) Name() string { return "deterministic" }

func (Deterministic) Triage(_ context.Context, req Request) (Summary, error) {
	if len(req.Anomalies) == 0 {
		return Summary{}, errors.New("triage: empty anomaly batch")
	}

	focal := req.Anomalies[0]
	for _, a := range req.Anomalies[1:] {
		if math.Abs(a.Score) > math.Abs(focal.Score) {
			focal = a
		}
	}

	severity := "warning"
	if math.Abs(focal.Score) >= 5 {
		severity = "critical"
	}

	step := fmt.Sprintf("Inspect recent changes to %q and its downstream dependencies.", focal.Service)
	if focal.TraceID != "" {
		step = fmt.Sprintf("Inspect recent changes to %q and its downstream dependencies; start from trace %s.", focal.Service, focal.TraceID)
	}

	return Summary{
		Title:           fmt.Sprintf("%s anomaly on %s", focal.Metric, focal.Service),
		Severity:        severity,
		AffectedService: focal.Service,
		ProbableCause: fmt.Sprintf(
			"%d correlated signal(s) breached the anomaly threshold; %s on %s is the strongest (z=%.1f, value=%.2f).",
			len(req.Anomalies), focal.Metric, focal.Service, focal.Score, focal.Value),
		SuggestedNextStep: step,
	}, nil
}

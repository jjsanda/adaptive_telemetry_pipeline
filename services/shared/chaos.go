package shared

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/baggage"
)

// Chaos baggage keys. Using W3C baggage means a fault instruction set at the
// edge (order-api) propagates across every downstream hop — Go, Node, or Python
// — without any service needing to know the topology. It also gives us a clean,
// reproducible way to induce the latency spike the anomaly detector is meant to
// catch.
const (
	chaosTargetKey = "chaos.target" // service.name that should misbehave, or "all"
	chaosModeKey   = "chaos.mode"   // "slow" | "error"
	chaosValueKey  = "chaos.value"  // slow delay in milliseconds
)

// SetChaosBaggage attaches chaos-injection instructions to ctx as baggage.
// A no-op when target or mode is empty.
func SetChaosBaggage(ctx context.Context, target, mode string, valueMS int) context.Context {
	if target == "" || mode == "" {
		return ctx
	}
	var members []baggage.Member
	for k, v := range map[string]string{
		chaosTargetKey: target,
		chaosModeKey:   mode,
		chaosValueKey:  strconv.Itoa(valueMS),
	} {
		if m, err := baggage.NewMember(k, v); err == nil {
			members = append(members, m)
		}
	}
	bag, err := baggage.New(members...)
	if err != nil {
		return ctx
	}
	return baggage.ContextWithBaggage(ctx, bag)
}

// ApplyChaos inspects propagated chaos baggage and, if this service is the
// target, injects the requested fault: sleeps (mode "slow") or returns an error
// (mode "error"). Returns nil when no fault applies. The Node and Python
// services implement the same contract against the raw `baggage` header.
func ApplyChaos(ctx context.Context, serviceName string) error {
	bag := baggage.FromContext(ctx)
	target := bag.Member(chaosTargetKey).Value()
	if target == "" || (target != serviceName && target != "all") {
		return nil
	}
	switch bag.Member(chaosModeKey).Value() {
	case "slow":
		ms := 800
		if v := bag.Member(chaosValueKey).Value(); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				ms = n
			}
		}
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case "error":
		return fmt.Errorf("%s: injected fault via chaos baggage", serviceName)
	}
	return nil
}

package resourcecostprocessor

import "errors"

// Config controls the resourcecost processor.
//
// The processor answers two FinOps/governance questions that show up in every
// large observability bill, for ANY workload (not just GenAI): "who owns this
// telemetry?" and "roughly what does it cost to ship?". It attaches two
// resource attributes derived from config:
//
//	telemetry.owner.team    — from a service.name -> team mapping
//	telemetry.cost.estimate — a volume-based USD proxy (serialized bytes * rate)
type Config struct {
	// TeamByService maps service.name to the owning team. The matched team is
	// attached as the telemetry.owner.team resource attribute.
	TeamByService map[string]string `mapstructure:"team_by_service"`

	// DefaultTeam is attached when a resource's service.name has no explicit
	// entry in TeamByService. Empty means "leave unattributed".
	DefaultTeam string `mapstructure:"default_team"`

	// CostPerKB is the cost proxy rate in USD per kilobyte of serialized signal
	// data. The estimate is intentionally a proxy, not a bill — it makes the
	// relative cost of noisy vs. lean services visible. Set to 0 to disable
	// cost attribution entirely (and skip the per-resource sizing work).
	CostPerKB float64 `mapstructure:"cost_per_kb"`
}

// Validate implements the config validation contract. Note: the collector no
// longer exposes a component.ConfigValidator type — defining Validate() error
// on the Config is all that's required for the framework to call it.
func (c *Config) Validate() error {
	if c.CostPerKB < 0 {
		return errors.New("cost_per_kb must be >= 0")
	}
	return nil
}

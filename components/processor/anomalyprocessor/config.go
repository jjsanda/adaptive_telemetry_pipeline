package anomalyprocessor

import (
	"errors"
	"time"
)

// Config controls the anomaly processor.
type Config struct {
	// Alpha is the EWMA smoothing factor in (0,1]. Larger reacts faster but
	// absorbs sustained shifts sooner.
	Alpha float64 `mapstructure:"alpha"`
	// Threshold is the |z-score| above which a point is flagged.
	Threshold float64 `mapstructure:"threshold"`
	// Warmup is the number of samples per series observed before any flag.
	Warmup int `mapstructure:"warmup"`

	// Shards is the number of lock shards for the per-series state map. More
	// shards reduce contention under high concurrency.
	Shards int `mapstructure:"shards"`
	// StaleAfter evicts a series whose last sample is older than this, keeping
	// memory bounded regardless of how many series appear over time. 0 disables.
	StaleAfter time.Duration `mapstructure:"stale_after"`
	// SweepInterval is how often the stale-series sweeper runs. 0 disables.
	SweepInterval time.Duration `mapstructure:"sweep_interval"`
}

func (c *Config) Validate() error {
	if c.Alpha <= 0 || c.Alpha > 1 {
		return errors.New("alpha must be in (0, 1]")
	}
	if c.Threshold <= 0 {
		return errors.New("threshold must be > 0")
	}
	if c.Warmup < 0 {
		return errors.New("warmup must be >= 0")
	}
	if c.Shards < 1 {
		return errors.New("shards must be >= 1")
	}
	if c.StaleAfter < 0 || c.SweepInterval < 0 {
		return errors.New("stale_after and sweep_interval must be >= 0")
	}
	return nil
}

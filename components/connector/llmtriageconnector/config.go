package llmtriageconnector

import (
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/config/configopaque"
)

// Config controls the llmtriage connector.
type Config struct {
	// Backend selects the triage engine: "deterministic" (default, offline, no
	// key) or "anthropic" (calls the Claude Messages API).
	Backend string `mapstructure:"backend"`
	// Model is the model id for the anthropic backend.
	Model string `mapstructure:"model"`
	// APIKey for the anthropic backend. configopaque redacts it from logged config.
	APIKey configopaque.String `mapstructure:"api_key"`
	// Timeout bounds each LLM call.
	Timeout time.Duration `mapstructure:"timeout"`

	// QueueSize is the bounded channel capacity. When full, anomalies are
	// dropped (and counted) rather than blocking the pipeline — this is the
	// backpressure boundary that keeps LLM latency off the hot path.
	QueueSize int `mapstructure:"queue_size"`
	// Workers is the number of goroutines draining the queue.
	Workers int `mapstructure:"workers"`
	// MaxBatch is the most anomalies triaged in a single LLM call.
	MaxBatch int `mapstructure:"max_batch"`
	// MaxWait is how long a worker waits to fill a batch before flushing.
	MaxWait time.Duration `mapstructure:"max_wait"`
	// DebounceWindow suppresses re-triaging the same (service, metric) within
	// this window — cost control for flapping anomalies.
	DebounceWindow time.Duration `mapstructure:"debounce_window"`

	// WebhookURL, if set, receives the triage summary as JSON (e.g. Slack).
	WebhookURL string `mapstructure:"webhook_url"`
}

func (c *Config) Validate() error {
	switch c.Backend {
	case "", "deterministic", "anthropic":
	default:
		return fmt.Errorf("unknown backend %q (want deterministic|anthropic)", c.Backend)
	}
	if c.Backend == "anthropic" && c.APIKey == "" {
		return errors.New("backend anthropic requires api_key")
	}
	if c.QueueSize <= 0 {
		return errors.New("queue_size must be > 0")
	}
	if c.Workers <= 0 {
		return errors.New("workers must be > 0")
	}
	if c.MaxBatch <= 0 {
		return errors.New("max_batch must be > 0")
	}
	if c.MaxWait < 0 {
		return errors.New("max_wait must be >= 0")
	}
	return nil
}

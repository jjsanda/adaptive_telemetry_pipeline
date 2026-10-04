package llmtriageconnector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/configopaque"
)

func TestConfigValidate(t *testing.T) {
	base := createDefaultConfig().(*Config)
	require.NoError(t, base.Validate(), "the default config must be valid")

	unknownBackend := *base
	unknownBackend.Backend = "gpt"
	assert.Error(t, unknownBackend.Validate())

	anthropicNoKey := *base
	anthropicNoKey.Backend = "anthropic"
	anthropicNoKey.APIKey = ""
	assert.Error(t, anthropicNoKey.Validate(), "anthropic backend requires a key")

	anthropicWithKey := anthropicNoKey
	anthropicWithKey.APIKey = configopaque.String("sk-test")
	assert.NoError(t, anthropicWithKey.Validate())

	for name, mutate := range map[string]func(*Config){
		"queue_size": func(c *Config) { c.QueueSize = 0 },
		"workers":    func(c *Config) { c.Workers = 0 },
		"max_batch":  func(c *Config) { c.MaxBatch = 0 },
		"max_wait":   func(c *Config) { c.MaxWait = -1 },
	} {
		c := *base
		mutate(&c)
		assert.Errorf(t, c.Validate(), "invalid %s must be rejected", name)
	}
}

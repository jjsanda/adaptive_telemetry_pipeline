package resourcecostprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	require.NoError(t, (&Config{CostPerKB: 0}).Validate(), "zero cost disables the proxy and is valid")
	require.NoError(t, (&Config{CostPerKB: 0.5}).Validate())
	require.Error(t, (&Config{CostPerKB: -1}).Validate(), "negative cost must be rejected")
}

func TestCreateDefaultConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	require.NoError(t, cfg.Validate())
	assert.InDelta(t, 0.0004, cfg.CostPerKB, 1e-9)
}

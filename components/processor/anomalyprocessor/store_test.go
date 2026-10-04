package anomalyprocessor

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jjsanda/adaptive_telemetry_pipeline/components/internal/anomaly"
)

func newDet() *anomaly.Detector { return anomaly.NewDetector(0.05, 3, 20) }

func TestSweepEvictsStale(t *testing.T) {
	base := time.Now()
	cur := base
	st := newStore(4, newDet, time.Minute)
	st.now = func() time.Time { return cur }

	for i := 0; i < 10; i++ {
		st.detect("k"+strconv.Itoa(i), 1)
	}
	require.Equal(t, 10, st.size())
	require.Equal(t, 0, st.sweep(), "nothing stale yet")

	cur = base.Add(2 * time.Minute)
	require.Equal(t, 10, st.sweep(), "all series now stale")
	require.Equal(t, 0, st.size(), "memory reclaimed")
}

func TestSweepDisabled(t *testing.T) {
	st := newStore(2, newDet, 0) // staleAfter 0 disables eviction
	st.detect("k", 1)
	assert.Equal(t, 0, st.sweep())
	assert.Equal(t, 1, st.size())
}

// TestConcurrentDetect must be race-clean: many goroutines hit overlapping keys.
func TestConcurrentDetect(t *testing.T) {
	st := newStore(8, newDet, time.Minute)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				st.detect("series-"+strconv.Itoa(i%50), float64(i))
			}
		}()
	}
	wg.Wait()
	assert.LessOrEqual(t, st.size(), 50, "bounded distinct keys")
}

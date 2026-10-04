package anomaly

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reference is an independent re-implementation of the recurrence, used to
// cross-check the Detector port bit-for-bit.
func reference(samples []float64, alpha, threshold float64, warmup int) (zs []float64, flags []bool) {
	var n int
	var mean, variance float64
	for _, x := range samples {
		n++
		if n == 1 {
			mean, variance = x, 0
			zs, flags = append(zs, 0), append(flags, false)
			continue
		}
		diff := x - mean
		incr := alpha * diff
		mean += incr
		variance = (1 - alpha) * (variance + diff*incr)
		var z float64
		if variance > 0 {
			z = diff / math.Sqrt(variance)
		}
		zs = append(zs, z)
		flags = append(flags, n > warmup && math.Abs(z) > threshold)
	}
	return
}

// TestOracle pins the detector to a deterministic series: 40 steady samples of
// 100, a spike to 200/205/198, then recovery. The expected z-scores were
// computed by hand from the recurrence and demonstrate the EWMA "catch-up":
// the spike is flagged at its onset (idx 40, 41) but by the third elevated
// sample (idx 42) the mean has adapted and z has fallen back under threshold.
func TestOracle(t *testing.T) {
	const alpha, threshold, warmup = 0.05, 3.0, 20

	samples := make([]float64, 0, 44)
	for i := 0; i < 40; i++ {
		samples = append(samples, 100.0)
	}
	samples = append(samples, 200, 205, 198, 100)

	d := NewDetector(alpha, threshold, warmup)
	var flagged []int
	zs := make([]float64, len(samples))
	for i, x := range samples {
		z, anom := d.Update(x)
		zs[i] = z
		if anom {
			flagged = append(flagged, i)
		}
	}

	assert.Equal(t, []int{40, 41}, flagged, "spike flagged at onset only (catch-up thereafter)")
	assert.InDelta(t, 4.5883, zs[40], 1e-3)
	assert.InDelta(t, 3.2857, zs[41], 1e-3)
	assert.InDelta(t, 2.4912, zs[42], 1e-3, "third elevated sample already back under threshold")
	assert.InDelta(t, -0.4165, zs[43], 1e-3)
	assert.Equal(t, 0.0, zs[39], "constant warmup region has zero variance -> zero z")
}

// TestMatchesReference guards the port against the independent reference over a
// deterministic non-trivial series.
func TestMatchesReference(t *testing.T) {
	const alpha, threshold, warmup = 0.1, 2.5, 15
	samples := make([]float64, 300)
	for i := range samples {
		samples[i] = 50 + 10*math.Sin(float64(i)/7) + float64(i%13)
	}
	wantZ, wantF := reference(samples, alpha, threshold, warmup)

	d := NewDetector(alpha, threshold, warmup)
	for i, x := range samples {
		z, f := d.Update(x)
		require.InDelta(t, wantZ[i], z, 1e-12, "z mismatch at %d", i)
		require.Equal(t, wantF[i], f, "flag mismatch at %d", i)
	}
}

func TestWarmupSuppresses(t *testing.T) {
	d := NewDetector(0.5, 1.0, 10)
	// A big jump inside the warmup window must not be flagged.
	_, _ = d.Update(1)
	for i := 0; i < 5; i++ {
		_, anom := d.Update(1000)
		assert.False(t, anom, "no flag before warmup completes")
	}
}

func BenchmarkUpdate(b *testing.B) {
	d := NewDetector(0.05, 3.0, 20)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d.Update(float64(i%100) + 100)
	}
}

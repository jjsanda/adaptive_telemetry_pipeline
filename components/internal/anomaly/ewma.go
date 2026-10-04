// Package anomaly implements a tiny, O(1)-memory streaming anomaly detector
// based on an exponentially-weighted moving mean and variance (the West/Finch
// incremental EWMV recurrence). It is deliberately simple and honest about its
// limits: a fast EWMA catches transients but partially absorbs sustained level
// shifts (see Detector.Update), so it flags the ONSET of a spike and then adapts.
package anomaly

import "math"

// Detector maintains an exponentially-weighted mean and variance for a single
// series and scores each incoming sample with a z-score.
type Detector struct {
	alpha     float64
	threshold float64
	warmup    int

	n        int
	mean     float64
	variance float64
}

// NewDetector returns a detector. alpha is the EWMA smoothing factor in (0,1];
// threshold is the |z| above which a point is anomalous; warmup is the number
// of samples to observe before any point may be flagged.
func NewDetector(alpha, threshold float64, warmup int) *Detector {
	return &Detector{alpha: alpha, threshold: threshold, warmup: warmup}
}

// Update ingests one sample and returns its z-score and whether it is anomalous.
// It is the exact Go port of the validated Python oracle:
//
//	diff = x - mean
//	incr = alpha * diff
//	mean = mean + incr
//	var  = (1 - alpha) * (var + diff*incr)   # == (1-alpha)*(var + alpha*diff^2)
//	z    = diff / sqrt(var)                   # 0 during warmup / zero variance
func (d *Detector) Update(x float64) (z float64, anomaly bool) {
	d.n++
	if d.n == 1 {
		d.mean = x
		d.variance = 0
		return 0, false
	}
	diff := x - d.mean
	incr := d.alpha * diff
	d.mean += incr
	d.variance = (1 - d.alpha) * (d.variance + diff*incr)
	if d.variance > 0 {
		z = diff / math.Sqrt(d.variance)
	}
	return z, d.n > d.warmup && math.Abs(z) > d.threshold
}

// Count returns the number of samples observed.
func (d *Detector) Count() int { return d.n }

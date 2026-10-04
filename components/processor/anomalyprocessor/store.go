package anomalyprocessor

import (
	"hash/fnv"
	"sync"
	"time"

	"github.com/jjsanda/adaptive_telemetry_pipeline/components/internal/anomaly"
)

type entry struct {
	det      *anomaly.Detector
	lastSeen time.Time
}

// shard is one lock-striped partition of the series map. Sharding lets many
// concurrent ConsumeMetrics calls proceed in parallel as long as they touch
// different shards.
type shard struct {
	mu sync.Mutex
	m  map[string]*entry
}

// store holds per-series EWMA detectors, keyed by a stable series identity, and
// evicts series that go silent to keep memory O(active series) rather than
// O(series ever seen).
type store struct {
	shards     []*shard
	newDet     func() *anomaly.Detector
	staleAfter time.Duration
	now        func() time.Time // injectable for tests
}

func newStore(numShards int, newDet func() *anomaly.Detector, staleAfter time.Duration) *store {
	if numShards < 1 {
		numShards = 1
	}
	shards := make([]*shard, numShards)
	for i := range shards {
		shards[i] = &shard{m: make(map[string]*entry)}
	}
	return &store{shards: shards, newDet: newDet, staleAfter: staleAfter, now: time.Now}
}

func (s *store) shardFor(key string) *shard {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return s.shards[h.Sum64()%uint64(len(s.shards))]
}

// detect updates the detector for key with val and returns the z-score and flag.
func (s *store) detect(key string, val float64) (float64, bool) {
	sh := s.shardFor(key)
	sh.mu.Lock()
	e := sh.m[key]
	if e == nil {
		e = &entry{det: s.newDet()}
		sh.m[key] = e
	}
	z, a := e.det.Update(val)
	e.lastSeen = s.now()
	sh.mu.Unlock()
	return z, a
}

// sweep evicts series unseen for longer than staleAfter and returns the count.
func (s *store) sweep() int {
	if s.staleAfter <= 0 {
		return 0
	}
	cut := s.now().Add(-s.staleAfter)
	evicted := 0
	for _, sh := range s.shards {
		sh.mu.Lock()
		for k, e := range sh.m {
			if e.lastSeen.Before(cut) {
				delete(sh.m, k)
				evicted++
			}
		}
		sh.mu.Unlock()
	}
	return evicted
}

func (s *store) size() int {
	n := 0
	for _, sh := range s.shards {
		sh.mu.Lock()
		n += len(sh.m)
		sh.mu.Unlock()
	}
	return n
}

# ADR 0005 — An EWMA z-score detector (and its honest limits)

**Status:** Accepted

## Context
We want in-stream anomaly detection inside the Collector: O(1) memory per series,
cheap enough to run on every data point, and explainable.

## Decision
Use an exponentially-weighted moving mean and variance (the West/Finch incremental
EWMV recurrence) and flag points whose |z-score| exceeds a threshold after a warmup.
The Go implementation is a direct port of a validated Python oracle and is pinned to it
by a table test (identical z-scores within 1e-3). The hot path is ~9 ns/op with zero
allocations.

## Consequences
- **Catch-up behaviour is a feature, understood.** A fast EWMA flags the *onset* of a
  spike and then adapts to the new level, so a sustained shift stops being flagged after
  a few samples. The oracle test encodes exactly this (a 100→200/205/198 spike flags the
  first two points, not the third). Production systems pair a fast detector with a slow
  one, or add change-point detection.
- **Known limits, stated plainly:** the method assumes a roughly unimodal,
  non-seasonal series. It will misbehave on strong seasonality or multi-modal
  distributions. Knowing when a simple detector is *insufficient* matters more than
  pretending it is universal.
- Memory stays bounded by a background sweeper that evicts series unseen for
  `stale_after`; an observable gauge (`anomaly.series.tracked`) proves it.

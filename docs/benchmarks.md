# Benchmarks

The pipeline scores **every** scalar data point inline, so the detector's inner loop and the connector aggregation paths are genuinely hot. This page documents them and how to reproduce the numbers.

## Hot paths

- **`anomaly.Detector.Update`** — the EWMA mean/variance recurrence, called once per data point per series. This is the tightest loop in the project: it does a handful of float ops and no allocation. Benchmarked directly in [`components/internal/anomaly/ewma_test.go`](../components/internal/anomaly/ewma_test.go) (`BenchmarkUpdate`).
- **`redmetrics` aggregation** — folding a span into per-series counters and placing its duration in a histogram bucket (`sort.SearchFloat64s`), under a mutex shared with the flush ticker.
- **`llmtriage` enqueue** — extracting flagged points and doing a non-blocking channel send; the slow LLM call happens on a worker goroutine, off the measured path.

## Running them

```bash
make bench    # go -C components test -run '^$' -bench . -benchmem ./...
```

`-benchmem` reports allocations, which is the number that matters most for an every-data-point path. Verified result for the detector:

| Benchmark | Time | Allocations |
| --- | --- | --- |
| `BenchmarkUpdate` | **~9 ns/op** | **0 allocs/op** |

Zero allocations means no GC pressure from detection regardless of series count or throughput — reproducible on any machine via `make bench` (absolute ns/op will vary with hardware).

## Load testing and "flat memory"

For end-to-end load, the [`load/`](../load) scripts drive the real app: `load/loadgen.sh` runs steady traffic (optionally with a chaos scenario), and `load/demo.sh` warms the detector then injects a latency anomaly. To load-test the collector's components directly — without the app — point the OpenTelemetry [`telemetrygen`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/cmd/telemetrygen) tool at the OTLP receiver (`localhost:4317`) to generate synthetic traces at a controlled rate.

**"Flat memory" is a measured claim, not a hope.** New series appear continuously under load, but the `anomaly` stale-series sweeper evicts series unseen for `stale_after`, keeping the store O(*active* series). The `anomaly.series.tracked` observable gauge (scrape it from Prometheus at `localhost:8888`/`8889`) exposes the live count: under sustained load with churning series it should plateau, not climb. That gauge is the proof the sweeper works.

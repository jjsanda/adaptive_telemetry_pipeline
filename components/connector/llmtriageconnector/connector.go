package llmtriageconnector

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/jjsanda/adaptive_telemetry_pipeline/components/internal/llm"
)

const scopeName = "github.com/jjsanda/adaptive_telemetry_pipeline/components/connector/llmtriageconnector"

type triageConn struct {
	cfg    *Config
	next   consumer.Logs
	logger *zap.Logger
	client llm.Client

	queue      chan llm.Anomaly
	workerCtx  context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	httpClient *http.Client

	mu       sync.Mutex
	lastEmit map[string]time.Time

	// self-observability — the AI component instruments itself.
	dropped    metric.Int64Counter
	llmErrors  metric.Int64Counter
	emitted    metric.Int64Counter
	tokens     metric.Int64Counter
	llmLatency metric.Float64Histogram
}

func newConnector(cfg *Config, set connector.Settings, next consumer.Logs) (*triageConn, error) {
	var client llm.Client = llm.Deterministic{}
	if cfg.Backend == "anthropic" {
		client = llm.NewAnthropic(string(cfg.APIKey), cfg.Model, cfg.Timeout)
	}

	c := &triageConn{
		cfg:        cfg,
		next:       next,
		logger:     set.Logger,
		client:     client,
		queue:      make(chan llm.Anomaly, cfg.QueueSize),
		httpClient: &http.Client{Timeout: cfg.Timeout},
		lastEmit:   make(map[string]time.Time),
	}

	meter := set.MeterProvider.Meter(scopeName)
	var err error
	if c.dropped, err = meter.Int64Counter("llmtriage.queue.dropped",
		metric.WithDescription("Anomalies dropped because the queue was full")); err != nil {
		return nil, err
	}
	if c.llmErrors, err = meter.Int64Counter("llmtriage.llm.errors",
		metric.WithDescription("Failed LLM triage calls")); err != nil {
		return nil, err
	}
	if c.emitted, err = meter.Int64Counter("llmtriage.summaries.emitted",
		metric.WithDescription("Triage summary log records emitted")); err != nil {
		return nil, err
	}
	if c.tokens, err = meter.Int64Counter("llmtriage.llm.tokens",
		metric.WithDescription("Total LLM tokens consumed")); err != nil {
		return nil, err
	}
	if c.llmLatency, err = meter.Float64Histogram("llmtriage.llm.latency",
		metric.WithDescription("LLM call latency"), metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if _, err = meter.Int64ObservableGauge("llmtriage.queue.depth",
		metric.WithDescription("Current triage queue depth"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(len(c.queue)))
			return nil
		})); err != nil {
		return nil, err
	}
	return c, nil
}

// Capabilities: reads metrics, produces new logs — no mutation of the input.
func (c *triageConn) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (c *triageConn) Start(_ context.Context, _ component.Host) error {
	c.workerCtx, c.cancel = context.WithCancel(context.Background())
	for i := 0; i < c.cfg.Workers; i++ {
		c.wg.Add(1)
		go c.worker()
	}
	return nil
}

func (c *triageConn) Shutdown(_ context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	return nil
}

// ConsumeMetrics is the hot path. It NEVER blocks on the LLM: it only extracts
// anomalies and enqueues them, shedding load when the queue is full.
func (c *triageConn) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	for _, a := range extractAnomalies(md) {
		select {
		case c.queue <- a:
		default:
			c.dropped.Add(ctx, 1)
		}
	}
	return nil
}

func (c *triageConn) worker() {
	defer c.wg.Done()
	for {
		batch := c.collectBatch()
		if batch == nil {
			return // context cancelled
		}
		if len(batch) > 0 {
			c.handle(batch)
		}
	}
}

// collectBatch blocks for the first anomaly then fills the batch up to MaxBatch
// or MaxWait, whichever comes first. Returns nil only when shutting down.
func (c *triageConn) collectBatch() []llm.Anomaly {
	var batch []llm.Anomaly
	select {
	case <-c.workerCtx.Done():
		return nil
	case a := <-c.queue:
		batch = append(batch, a)
	}
	timer := time.NewTimer(c.cfg.MaxWait)
	defer timer.Stop()
	for len(batch) < c.cfg.MaxBatch {
		select {
		case <-c.workerCtx.Done():
			return batch
		case a := <-c.queue:
			batch = append(batch, a)
		case <-timer.C:
			return batch
		}
	}
	return batch
}

func (c *triageConn) handle(batch []llm.Anomaly) {
	batch = c.debounce(batch)
	if len(batch) == 0 {
		return
	}
	start := time.Now()
	sum, err := c.client.Triage(c.workerCtx, llm.Request{Anomalies: batch})
	c.llmLatency.Record(c.workerCtx, time.Since(start).Seconds())
	if err != nil {
		c.llmErrors.Add(c.workerCtx, 1)
		c.logger.Warn("llm triage failed", zap.String("engine", c.client.Name()), zap.Error(err))
		return
	}
	if sum.Tokens > 0 {
		c.tokens.Add(c.workerCtx, int64(sum.Tokens))
	}
	c.emit(batch, sum)
	c.emitted.Add(c.workerCtx, 1)
	if c.cfg.WebhookURL != "" {
		c.postWebhook(sum)
	}
}

// debounce drops anomalies whose (service, metric) was triaged within the window.
func (c *triageConn) debounce(in []llm.Anomaly) []llm.Anomaly {
	if c.cfg.DebounceWindow <= 0 {
		return in
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := in[:0:0]
	for _, a := range in {
		k := a.Service + "|" + a.Metric
		if last, ok := c.lastEmit[k]; ok && now.Sub(last) < c.cfg.DebounceWindow {
			continue
		}
		c.lastEmit[k] = now
		out = append(out, a)
	}
	return out
}

// emit turns a triage summary into a new, trace-correlated log record and pushes
// it into the logs pipeline. This runs on a worker goroutine (not the consume
// path), using the component-lifetime context.
func (c *triageConn) emit(batch []llm.Anomaly, sum llm.Summary) {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "adaptive-otelcol")
	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName(scopeName)

	lr := sl.LogRecords().AppendEmpty()
	lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	lr.SetSeverityText(strings.ToUpper(sum.Severity))
	lr.SetSeverityNumber(severityNumber(sum.Severity))
	lr.Body().SetStr(sum.Title + " — " + sum.ProbableCause)

	at := lr.Attributes()
	at.PutStr("triage.title", sum.Title)
	at.PutStr("triage.severity", sum.Severity)
	at.PutStr("triage.affected_service", sum.AffectedService)
	at.PutStr("triage.probable_cause", sum.ProbableCause)
	at.PutStr("triage.suggested_next_step", sum.SuggestedNextStep)
	at.PutStr("triage.engine", c.client.Name())
	at.PutInt("triage.anomaly_count", int64(len(batch)))

	for _, a := range batch {
		if a.TraceID != "" {
			if tid, err := traceIDFromHex(a.TraceID); err == nil {
				lr.SetTraceID(tid)
			}
			at.PutStr("trace_id", a.TraceID)
			break
		}
	}

	if err := c.next.ConsumeLogs(c.workerCtx, ld); err != nil {
		c.logger.Warn("emit triage log failed", zap.Error(err))
	}
}

func (c *triageConn) postWebhook(sum llm.Summary) {
	body, _ := json.Marshal(sum)
	req, err := http.NewRequestWithContext(c.workerCtx, http.MethodPost, c.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Warn("triage webhook failed", zap.Error(err))
		return
	}
	_ = resp.Body.Close()
}

func severityNumber(sev string) plog.SeverityNumber {
	switch sev {
	case "info":
		return plog.SeverityNumberInfo
	case "critical":
		return plog.SeverityNumberError
	default:
		return plog.SeverityNumberWarn
	}
}

func traceIDFromHex(s string) (pcommon.TraceID, error) {
	var tid pcommon.TraceID
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(tid) {
		if err == nil {
			err = errInvalidTraceID
		}
		return tid, err
	}
	copy(tid[:], b)
	return tid, nil
}

// order-api is the edge service of the demo "order pipeline". It accepts an
// order, forwards it to the fulfillment-worker, and returns the priced result.
// It is hand-instrumented with the OpenTelemetry Go SDK (traces, metrics, logs)
// via the shared bootstrap — no auto-instrumentation, so every moving part is
// explicit.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jjsanda/adaptive_telemetry_pipeline/services/shared"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const serviceName = "order-api"

var version = "0.1.0"

type server struct {
	tracer        trace.Tracer
	httpClient    *http.Client
	workerURL     string
	ordersCreated metric.Int64Counter
	orderValue    metric.Float64Histogram
	log           logger
}

type logger interface {
	InfoContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, log, err := shared.SetupOTel(ctx, serviceName, version)
	if err != nil {
		panic(err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	meter := otel.Meter(serviceName)
	ordersCreated, _ := meter.Int64Counter("orders.created",
		metric.WithDescription("Orders accepted by the edge service"))
	orderValue, _ := meter.Float64Histogram("order.value.usd",
		metric.WithDescription("Monetary value of accepted orders"), metric.WithUnit("USD"))

	app := &server{
		tracer:        otel.Tracer(serviceName),
		httpClient:    &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
		workerURL:     env("WORKER_URL", "http://fulfillment-worker:8081/fulfill"),
		ordersCreated: ordersCreated,
		orderValue:    orderValue,
		log:           log,
	}

	mux := http.NewServeMux()
	mux.Handle("/orders", otelhttp.NewHandler(http.HandlerFunc(app.handleOrder), "POST /orders"))
	mux.Handle("/", otelhttp.NewHandler(http.HandlerFunc(app.handleOrder), "GET /"))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: ":" + env("PORT", "8080"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.InfoContext(ctx, "order-api listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.ErrorContext(ctx, "server error", "err", err)
		}
	}()

	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
}

var skus = []string{"SKU-COFFEE", "SKU-MUG", "SKU-BEANS", "SKU-FILTER", "SKU-GRINDER"}

type orderRequest struct {
	CustomerID string `json:"customer_id"`
	SKU        string `json:"sku"`
	Quantity   int    `json:"quantity"`
}

type orderResponse struct {
	OrderID  string  `json:"order_id"`
	SKU      string  `json:"sku"`
	Quantity int     `json:"quantity"`
	PriceUSD float64 `json:"price_usd"`
	InStock  bool    `json:"in_stock"`
	TraceID  string  `json:"trace_id"`
	Status   string  `json:"status"`
}

func (s *server) handleOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Decode (POST) or synthesise (GET) an order.
	req := orderRequest{}
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.SKU == "" {
		req.SKU = skus[rand.Intn(len(skus))]
	}
	if req.Quantity <= 0 {
		req.Quantity = 1 + rand.Intn(3)
	}
	if req.CustomerID == "" {
		req.CustomerID = "cust-" + strconv.Itoa(1000+rand.Intn(9000))
	}

	orderID := "ord-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	ctx, span := s.tracer.Start(ctx, "create_order", trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("order.id", orderID),
			attribute.String("order.sku", req.SKU),
			attribute.Int("order.quantity", req.Quantity),
			attribute.String("enduser.id", req.CustomerID),
		))
	defer span.End()

	// Optional fault injection via query params, propagated as baggage:
	//   /orders?chaos=pricing&mode=slow&ms=900
	if target := r.URL.Query().Get("chaos"); target != "" {
		mode := valueOr(r.URL.Query().Get("mode"), "slow")
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		if ms == 0 {
			ms = 900
		}
		ctx = shared.SetChaosBaggage(ctx, target, mode, ms)
		span.SetAttributes(attribute.String("chaos.target", target), attribute.String("chaos.mode", mode))
	}

	res, err := s.fulfill(ctx, orderID, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "fulfillment failed")
		s.log.ErrorContext(ctx, "order failed", "order.id", orderID, "err", err)
		writeJSON(w, http.StatusBadGateway, orderResponse{OrderID: orderID, Status: "failed",
			TraceID: span.SpanContext().TraceID().String()})
		return
	}

	s.ordersCreated.Add(ctx, 1, metric.WithAttributes(attribute.String("order.sku", req.SKU)))
	s.orderValue.Record(ctx, res.PriceUSD)
	res.OrderID = orderID
	res.TraceID = span.SpanContext().TraceID().String()
	res.Status = "accepted"
	s.log.InfoContext(ctx, "order accepted", "order.id", orderID, "price_usd", res.PriceUSD, "in_stock", res.InStock)
	writeJSON(w, http.StatusOK, res)
}

// fulfill calls the fulfillment-worker over HTTP with context propagation.
func (s *server) fulfill(ctx context.Context, orderID string, req orderRequest) (orderResponse, error) {
	ctx, span := s.tracer.Start(ctx, "call_fulfillment", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	body, _ := json.Marshal(map[string]any{
		"order_id": orderID, "sku": req.SKU, "quantity": req.Quantity,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.workerURL, bytes.NewReader(body))
	if err != nil {
		return orderResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return orderResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return orderResponse{}, errors.New("worker returned " + resp.Status)
	}
	var out orderResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return orderResponse{}, err
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

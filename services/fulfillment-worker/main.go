// fulfillment-worker sits behind order-api. For each order it fans out — in
// parallel — to the inventory service (Node) and the pricing service (Python),
// then combines the results. This is where one trace visibly crosses three
// languages via W3C context propagation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jjsanda/adaptive_telemetry_pipeline/services/shared"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

const serviceName = "fulfillment-worker"

var version = "0.1.0"

type server struct {
	tracer       trace.Tracer
	httpClient   *http.Client
	inventoryURL string
	pricingURL   string
	fulfilled    metric.Int64Counter
	log          logger
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

	fulfilled, _ := otel.Meter(serviceName).Int64Counter("orders.fulfilled",
		metric.WithDescription("Orders fulfilled by the worker"))

	app := &server{
		tracer:       otel.Tracer(serviceName),
		httpClient:   &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport), Timeout: 10 * time.Second},
		inventoryURL: env("INVENTORY_URL", "http://inventory:8082/check"),
		pricingURL:   env("PRICING_URL", "http://pricing:8083/price"),
		fulfilled:    fulfilled,
		log:          log,
	}

	mux := http.NewServeMux()
	mux.Handle("/fulfill", otelhttp.NewHandler(http.HandlerFunc(app.handleFulfill), "POST /fulfill"))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: ":" + env("PORT", "8081"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.InfoContext(ctx, "fulfillment-worker listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.ErrorContext(ctx, "server error", "err", err)
		}
	}()

	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
}

type fulfillRequest struct {
	OrderID  string `json:"order_id"`
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type fulfillResponse struct {
	SKU      string  `json:"sku"`
	Quantity int     `json:"quantity"`
	PriceUSD float64 `json:"price_usd"`
	InStock  bool    `json:"in_stock"`
}

func (s *server) handleFulfill(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req fulfillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ctx, span := s.tracer.Start(ctx, "fulfill_order", trace.WithAttributes(
		attribute.String("order.id", req.OrderID),
		attribute.String("order.sku", req.SKU),
	))
	defer span.End()

	// This service may itself be a chaos target.
	if err := shared.ApplyChaos(ctx, serviceName); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var (
		inStock bool
		price   float64
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var out struct {
			InStock bool `json:"in_stock"`
		}
		if err := s.call(gctx, "check_inventory", s.inventoryURL, map[string]any{"sku": req.SKU, "quantity": req.Quantity}, &out); err != nil {
			return err
		}
		inStock = out.InStock
		return nil
	})
	g.Go(func() error {
		var out struct {
			PriceUSD float64 `json:"price_usd"`
		}
		if err := s.call(gctx, "compute_price", s.pricingURL, map[string]any{"sku": req.SKU, "quantity": req.Quantity}, &out); err != nil {
			return err
		}
		price = out.PriceUSD
		return nil
	})
	if err := g.Wait(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "downstream failure")
		s.log.ErrorContext(ctx, "fulfillment failed", "order.id", req.OrderID, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	s.fulfilled.Add(ctx, 1, metric.WithAttributes(attribute.Bool("order.in_stock", inStock)))
	writeJSON(w, http.StatusOK, fulfillResponse{SKU: req.SKU, Quantity: req.Quantity, PriceUSD: price, InStock: inStock})
}

// call POSTs a JSON body to url, decodes the JSON response into out, and wraps
// the round-trip in a client span named op.
func (s *server) call(ctx context.Context, op, url string, payload any, out any) error {
	ctx, span := s.tracer.Start(ctx, op, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		err := errors.New(op + ": " + resp.Status)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
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

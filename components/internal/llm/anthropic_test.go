package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicTriage(t *testing.T) {
	var gotAuth, gotVersion, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"prefix {\"title\":\"pricing anomaly\",\"severity\":\"critical\",\"affected_service\":\"pricing\",\"probable_cause\":\"latency spike\",\"suggested_next_step\":\"check deploys\"} suffix"}],"usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", "claude-sonnet-5", 5*time.Second)
	a.baseURL = srv.URL

	sum, err := a.Triage(context.Background(), Request{Anomalies: []Anomaly{{Service: "pricing", Metric: "red.latency.avg.ms", Score: 6}}})
	require.NoError(t, err)
	assert.Equal(t, "pricing anomaly", sum.Title)
	assert.Equal(t, "critical", sum.Severity)
	assert.Equal(t, 15, sum.Tokens, "input + output tokens")
	assert.Equal(t, "sk-test", gotAuth)
	assert.Equal(t, anthropicVersion, gotVersion)
	assert.Equal(t, "claude-sonnet-5", gotModel)
	assert.Equal(t, "anthropic:claude-sonnet-5", a.Name())
}

func TestAnthropicHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"rate_limited"}`)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", "claude-sonnet-5", 5*time.Second)
	a.baseURL = srv.URL
	_, err := a.Triage(context.Background(), Request{Anomalies: []Anomaly{{Service: "x"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

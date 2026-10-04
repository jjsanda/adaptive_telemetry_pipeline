package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	anthropicURL     = "https://api.anthropic.com/v1/messages"
	anthropicVersion = "2023-06-01"
	systemPrompt     = "You are an SRE incident-triage assistant embedded in an OpenTelemetry pipeline. " +
		"You receive a batch of correlated metric anomalies. Respond with ONLY a compact JSON object " +
		"(no markdown, no prose) with exactly these string keys: \"title\", \"severity\", " +
		"\"affected_service\", \"probable_cause\", \"suggested_next_step\". " +
		"\"severity\" must be one of \"info\", \"warning\", \"critical\"."
)

// Anthropic calls the Claude Messages API. It is optional and only constructed
// when the connector is configured with backend=anthropic and an API key, so no
// network access or key is ever required to build, test, or demo the pipeline.
type Anthropic struct {
	apiKey    string
	model     string
	baseURL   string
	client    *http.Client
	maxTokens int
}

func NewAnthropic(apiKey, model string, timeout time.Duration) *Anthropic {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Anthropic{apiKey: apiKey, model: model, baseURL: anthropicURL, client: &http.Client{Timeout: timeout}, maxTokens: 512}
}

func (a *Anthropic) Name() string { return "anthropic:" + a.model }

func (a *Anthropic) Triage(ctx context.Context, req Request) (Summary, error) {
	reqBody := map[string]any{
		"model":      a.model,
		"max_tokens": a.maxTokens,
		"system":     systemPrompt,
		"messages": []map[string]any{
			{"role": "user", "content": buildUserPrompt(req)},
		},
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return Summary{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL, bytes.NewReader(buf))
	if err != nil {
		return Summary{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return Summary{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Summary{}, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Summary{}, fmt.Errorf("anthropic: decode response: %w", err)
	}

	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	// LLM output is untrusted: parse + validate before it can become a log.
	sum, err := parseSummary(sb.String())
	if err != nil {
		return Summary{}, err
	}
	sum.Tokens = out.Usage.InputTokens + out.Usage.OutputTokens
	return sum, nil
}

func buildUserPrompt(req Request) string {
	var b strings.Builder
	b.WriteString("Anomalies detected in the last window:\n")
	for _, a := range req.Anomalies {
		fmt.Fprintf(&b, "- service=%s metric=%s value=%.3f z_score=%.2f", a.Service, a.Metric, a.Value, a.Score)
		if a.TraceID != "" {
			fmt.Fprintf(&b, " trace_id=%s", a.TraceID)
		}
		b.WriteByte('\n')
	}
	b.WriteString("\nProduce the triage JSON now.")
	return b.String()
}

// parseSummary tolerantly extracts the JSON object from model output and
// validates it. Exported-ish behavior is shared with tests via a fake that
// returns raw strings.
func parseSummary(text string) (Summary, error) {
	var s Summary
	if err := json.Unmarshal([]byte(extractJSON(text)), &s); err != nil {
		return Summary{}, fmt.Errorf("triage: model did not return valid JSON: %w", err)
	}
	if strings.TrimSpace(s.Title) == "" {
		return Summary{}, errors.New("triage: model output missing title")
	}
	s.Severity = normalizeSeverity(s.Severity)
	return s, nil
}

func extractJSON(text string) string {
	i := strings.IndexByte(text, '{')
	j := strings.LastIndexByte(text, '}')
	if i >= 0 && j > i {
		return text[i : j+1]
	}
	return text
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

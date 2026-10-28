// SPDX-License-Identifier: AGPL-3.0-or-later

package entry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Xustalis/OpenPanda/internal/config"
)

// sseServer answers every request with one fixed SSE body and closes.
func sseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func anthropicClient(t *testing.T, base string, retries int) *Client {
	t.Helper()
	c, err := NewClient(config.ModelConfig{
		APIType: config.APITypeAnthropic, BaseURL: base + "/v1",
		APIKey: "k", Model: "claude-test", MaxTokens: 64, MaxRetries: retries,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

// TestStreamInBandOverloadIsRetryable pins the classification of a failure the
// provider reports INSIDE the stream: it used to arrive as an untyped
// "entry: api error: stream error", which no retry, failover or user message
// path could act on. Anthropic publishes overload as a 529, so an in-band
// overload_error must keep the retry classification a 5xx would have had.
func TestStreamInBandOverloadIsRetryable(t *testing.T) {
	srv := sseServer(t, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n")
	c := anthropicClient(t, srv.URL, -1)
	_, err := c.StreamTurnsWithTools(context.Background(), "s",
		[]Turn{{Role: "user", Content: "h"}}, nil, nil, nil)
	if err == nil {
		t.Fatal("in-band error reported success")
	}
	if !retryable(err) {
		t.Fatalf("in-band overload not retryable: %v", err)
	}
	if !IsFatalModelError(err) {
		t.Fatalf("in-band overload not fatal-class (failover would skip it): %v", err)
	}
	var re *retryableError
	if errors.As(err, &re) {
		if re.status != http.StatusServiceUnavailable && re.status != 529 {
			t.Fatalf("status = %d, want 503/529", re.status)
		}
	}
}

// TestStreamInBandAuthIsNotRetryable: a definitive rejection inside the stream
// must not spend the retry ladder — it is the provider saying "fix your key".
func TestStreamInBandAuthIsNotRetryable(t *testing.T) {
	srv := sseServer(t, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"bad key\"}}\n\n")
	c := anthropicClient(t, srv.URL, -1)
	_, err := c.StreamTurnsWithTools(context.Background(), "s",
		[]Turn{{Role: "user", Content: "h"}}, nil, nil, nil)
	if err == nil {
		t.Fatal("in-band auth error reported success")
	}
	if retryable(err) {
		t.Fatalf("in-band auth error classified retryable: %v", err)
	}
	if !IsModelConfigError(err) {
		t.Fatalf("in-band auth error not a model-config error: %v", err)
	}
}

// TestOpenAIStreamInBandError covers the same rule on the Chat Completions
// path, whose in-band error rides on the chunk envelope.
func TestOpenAIStreamInBandError(t *testing.T) {
	srv := sseServer(t, "data: {\"error\":{\"type\":\"server_error\",\"message\":\"upstream died\"}}\n\n")
	c, err := NewClient(config.ModelConfig{
		APIType: config.APITypeOpenAI, BaseURL: srv.URL + "/v1",
		APIKey: "k", Model: "gpt-test", MaxTokens: 64, MaxRetries: -1,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	_, err = c.StreamTurnsWithTools(context.Background(), "s",
		[]Turn{{Role: "user", Content: "h"}}, nil, nil, nil)
	if err == nil {
		t.Fatal("in-band error reported success")
	}
	if !retryable(err) {
		t.Fatalf("OpenAI in-band server_error not retryable: %v", err)
	}
}

// TestNonStreamingInBandError: the same defect existed off the stream — a 200
// response whose body carries an error object was returned as an untyped
// "api error" that neither the retry ladder nor the fallback path recognized.
func TestNonStreamingInBandError(t *testing.T) {
	srv := sseServer(t, `{"error":{"code":"server_error","message":"upstream died"}}`)
	c, err := NewClient(config.ModelConfig{
		APIType: config.APITypeResponses, BaseURL: srv.URL + "/v1",
		APIKey: "k", Model: "gpt-test", MaxTokens: 64, MaxRetries: -1,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	_, err = c.CompleteTurnsWithTools(context.Background(), "s",
		[]Turn{{Role: "user", Content: "h"}}, nil)
	if err == nil {
		t.Fatal("200-with-error body reported success")
	}
	if !retryable(err) {
		t.Fatalf("in-band 200 error not retryable: %v", err)
	}
}

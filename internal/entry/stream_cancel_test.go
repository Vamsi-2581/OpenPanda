// SPDX-License-Identifier: AGPL-3.0-or-later

package entry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Xustalis/OpenPanda/internal/config"
)

// cancelOnWallet is a dead-simple "press stop" harness: the provider emits one
// delta and then holds the connection open, the caller cancels after seeing
// that delta, and the stream ends by the request context dying — not by EOF.
func cancelOnWallet(t *testing.T, body string, ct string) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", ct)
		_, _ = w.Write([]byte(body))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Hold the stream open until the client goes away, so the read loop
		// ends on the cancelled context rather than on a clean EOF.
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	c, err := NewClient(config.ModelConfig{
		APIType: config.APITypeOpenAI, BaseURL: srv.URL + "/v1",
		APIKey: "sk-test", Model: "gpt-test", MaxTokens: 128,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c, srv.Close
}

// TestStreamCancelIsNotASuccess pins the stop-button contract at the wire
// layer: a stream whose read ends because the CALLER cancelled is a cancelled
// turn, never a short-but-complete answer. Returning the partial text as a
// successful Response is what made a stopped turn look finished — the surface
// then persisted a truncated reply as the assistant's whole message.
func TestStreamCancelIsNotASuccess(t *testing.T) {
	c, closeSrv := cancelOnWallet(t, `data: {"choices":[{"delta":{"content":"half an ans"}}]}`+"\n\n", "text/event-stream")
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	saw := false
	var got Response
	resp, err := c.StreamTurnsWithTools(ctx, "sys",
		[]Turn{{Role: "user", Content: "hi"}}, nil,
		func(s string) { saw = true; cancel() }, nil)
	got = resp
	if !saw {
		t.Fatal("no delta reached the caller; the scenario did not exercise cancellation")
	}
	if err == nil {
		t.Fatalf("cancelled stream reported success with %q", got.Text)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestStreamCancelBillsNothing: a cancelled attempt is not a completed one, so
// it must not add to the usage counters the metrics path bills from.
func TestStreamCancelBillsNothing(t *testing.T) {
	c, closeSrv := cancelOnWallet(t, `data: {"choices":[{"delta":{"content":"x"}}]}`+"\n\n", "text/event-stream")
	defer closeSrv()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _ = c.StreamTurnsWithTools(ctx, "sys",
		[]Turn{{Role: "user", Content: "hi"}}, nil,
		func(string) { cancel() }, nil)
	if u := c.Usage(); u.InputTokens != 0 || u.OutputTokens != 0 {
		t.Fatalf("usage after a cancelled stream = %+v, want zero", u)
	}
}

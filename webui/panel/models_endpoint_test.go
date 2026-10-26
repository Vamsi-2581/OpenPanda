// SPDX-License-Identifier: AGPL-3.0-or-later

package panel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// doRaw runs a request and returns the status plus the raw body, for the
// endpoints that answer an error with plain text (http.Error) rather than
// a JSON envelope.
func doRaw(t *testing.T, h http.Handler, req *http.Request) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

// The console takes a model endpoint from the request body and the daemon
// then dials it. A caller holding the panel token could aim that round trip
// at the instance metadata service (169.254.169.254) and read the answer
// back through the console's own response — a token leak would become a
// cloud-credential leak, with the daemon as the confused deputy.
func TestCustomModelEndpointRefusesMetadataAddress(t *testing.T) {
	h, _, _ := newEnginePanel(t)

	for _, base := range []string{
		"https://169.254.169.254/latest/meta-data/",
		"http://169.254.169.254/",
		"https://100.100.100.200/latest/meta-data/",
	} {
		body := `{"provider":"custom","base_url":"` + base + `","model":"m"}`
		code, out := doJSON(t, h, jsonReq(http.MethodPost, "/api/models", body))
		if code != http.StatusBadRequest {
			t.Fatalf("add model with base_url %s: status = %d, want 400 (out %v)", base, code, out)
		}
	}
}

func TestCustomModelEndpointRefusesMetadataAddressOnFetch(t *testing.T) {
	h, _, _ := newEnginePanel(t)

	body := `{"provider":"custom","base_url":"https://169.254.169.254/latest/meta-data/","model":"m"}`
	code, raw := doRaw(t, h, jsonReq(http.MethodPost, "/api/models/fetch", body))
	if code != http.StatusBadRequest {
		t.Fatalf("fetch with a metadata base_url: status = %d, want 400 (body %s)", code, raw)
	}
	if !strings.Contains(raw, "metadata") {
		t.Fatalf("body = %q, want it to name the metadata address", raw)
	}
}

// A self-hosted model on the LAN is a normal setup — a private address stays
// allowed. What is refused is the credential service, not the home network.
//
// The check is exercised directly rather than through the endpoint: the
// network guard in internal/entry independently requires https for any
// non-loopback address, so an http:// LAN endpoint is refused by that rule
// and not by this one.
func TestCustomModelEndpointAllowsPrivateAddress(t *testing.T) {
	for _, base := range []string{
		"http://192.168.1.50:11434/v1",
		"https://10.0.0.7:8080/v1",
		"http://127.0.0.1:11434/v1",
	} {
		if err := checkCustomBaseURL(base); err != nil {
			t.Fatalf("checkCustomBaseURL(%q) = %v, want nil", base, err)
		}
	}
	for _, base := range []string{
		"https://169.254.169.254/latest/meta-data/",
		"http://169.254.169.254/",
		"https://100.100.100.200/",
		"https://fe80::1/",
	} {
		if err := checkCustomBaseURL(base); err == nil {
			t.Fatalf("checkCustomBaseURL(%q) = nil, want a refusal", base)
		}
	}
	// A malformed or schemeless endpoint is refused too — it would fail
	// later anyway, but failing here keeps the reason clear.
	for _, base := range []string{"", "ftp://example.com", "not a url", "https://"} {
		if err := checkCustomBaseURL(base); err == nil {
			t.Fatalf("checkCustomBaseURL(%q) = nil, want a refusal", base)
		}
	}
}

// A metadata address must never reach the config file either: the endpoint
// is persisted there and dialed later by the daemon.
func TestCustomModelEndpointNeverPersisted(t *testing.T) {
	h, _, configPath := newEnginePanel(t)

	body := `{"provider":"custom","base_url":"https://169.254.169.254/","model":"leaky-model"}`
	code, _ := doJSON(t, h, jsonReq(http.MethodPost, "/api/models", body))
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "169.254.169.254") {
		t.Fatalf("the refused endpoint reached the config file: %q", data)
	}
}

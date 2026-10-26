// SPDX-License-Identifier: AGPL-3.0-or-later

package security

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestMetadataHost(t *testing.T) {
	// The addresses a cloud instance uses to hand out its own credentials.
	// Reaching one turns the daemon into a confused deputy: the panel (or
	// any caller holding the panel token) would ask the instance for its
	// role credentials and read the answer back.
	metadata := []string{
		"169.254.169.254",     // AWS, Azure, GCP, DigitalOcean, OpenStack
		"169.254.169.254:80",  // with port
		"169.254.169.253",     // AWS VPC DNS
		"100.100.100.200",     // Alibaba Cloud
		"100.100.100.200:443", // Alibaba Cloud with port
		"169.254.1.1",         // the rest of link-local
		"fe80::1",             // IPv6 link-local
		"fe80::a9fe:a9fe",     // IPv6 link-local, fuller form
	}
	for _, h := range metadata {
		if !MetadataHost(h) {
			t.Fatalf("MetadataHost(%q) = false, want true", h)
		}
	}
	// A public address, a private one and a loopback one are all ordinary.
	for _, h := range []string{"api.deepseek.com", "10.0.0.5", "127.0.0.1", "8.8.8.8", ""} {
		if MetadataHost(h) {
			t.Fatalf("MetadataHost(%q) = true, want false", h)
		}
	}
}

func TestPrivateHost(t *testing.T) {
	for _, h := range []string{"10.0.0.5", "10.0.0.5:8080", "192.168.1.10", "172.16.0.1", "127.0.0.1", "127.0.0.1:11434", "::1", "fd00::1", "localhost", "foo.localhost"} {
		if !PrivateHost(h) {
			t.Fatalf("PrivateHost(%q) = false, want true", h)
		}
	}
	for _, h := range []string{"api.deepseek.com", "api.deepseek.com:443", "8.8.8.8", ""} {
		if PrivateHost(h) {
			t.Fatalf("PrivateHost(%q) = true, want false", h)
		}
	}
}

func TestCheckEndpointHost(t *testing.T) {
	// A metadata address is refused even when private addresses are allowed:
	// allowPrivate exists for the self-hosted model on the LAN, not for the
	// instance credential service.
	if err := CheckEndpointHost("169.254.169.254", true); err == nil {
		t.Fatal("a metadata address must be refused even with private allowed")
	}
	if err := CheckEndpointHost("169.254.169.254:80", true); err == nil {
		t.Fatal("a metadata address with port must be refused even with private allowed")
	}
	if err := CheckEndpointHost("100.100.100.200", true); err == nil {
		t.Fatal("the Alibaba metadata address must be refused")
	}
	// A LAN model box is legitimate when the caller allows private hosts.
	if err := CheckEndpointHost("192.168.1.10", true); err != nil {
		t.Fatalf("a private model endpoint should pass when allowed: %v", err)
	}
	if err := CheckEndpointHost("localhost:11434", true); err != nil {
		t.Fatalf("localhost model endpoint should pass when allowed: %v", err)
	}
	// ...and refused when it does not (a browser push service is public).
	if err := CheckEndpointHost("192.168.1.10", false); err == nil {
		t.Fatal("a private endpoint must be refused when private is not allowed")
	}
	if err := CheckEndpointHost("127.0.0.1", false); err == nil {
		t.Fatal("loopback must be refused when private is not allowed")
	}
	if err := CheckEndpointHost("localhost", false); err == nil {
		t.Fatal("localhost must be refused when private is not allowed")
	}
	if err := CheckEndpointHost("app.localhost:8080", false); err == nil {
		t.Fatal("subdomain localhost must be refused when private is not allowed")
	}
	// A public host passes either way.
	if err := CheckEndpointHost("fcm.googleapis.com", false); err != nil {
		t.Fatalf("a public host should pass: %v", err)
	}
	if err := CheckEndpointHost("", true); err == nil {
		t.Fatal("an empty host must be refused")
	}
}

// TestGuardedDialContext pins the connection-time half of the metadata rule:
// the literal addresses are refused before any dial, while loopback — literal
// and by name — stays dialable for local model servers and tests.
func TestGuardedDialContext(t *testing.T) {
	dial := GuardedDialContext(&net.Dialer{Timeout: 2 * time.Second})

	for _, addr := range []string{"169.254.169.254:80", "100.100.100.200:80", "[fe80::1]:80"} {
		if _, err := dial(context.Background(), "tcp", addr); err == nil {
			t.Errorf("dial %s was allowed, want a metadata refusal", addr)
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split listener addr: %v", err)
	}
	// The literal path and the name path. localhost resolves to several
	// candidates; the guard must skip a refused one and use the rest.
	for _, addr := range []string{ln.Addr().String(), net.JoinHostPort("localhost", port)} {
		conn, err := dial(context.Background(), "tcp", addr)
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		_ = conn.Close()
	}
}

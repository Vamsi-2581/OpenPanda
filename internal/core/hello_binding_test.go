// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"testing"
	"time"

	"github.com/Xustalis/OpenPanda/internal/bus"
	"github.com/Xustalis/OpenPanda/internal/ledger"
)

// TestRejectHelloBoundForAnotherNode covers the cross-node replay shape: a
// hello is signed for one receiver, so a member that captures it and offers
// it to a different node must be refused on the address alone. The HMAC is
// genuine and fresh — only the receiver it names is wrong.
func TestRejectHelloBoundForAnotherNode(t *testing.T) {
	ctx := context.Background()
	worker := newCoreWithNative(t, "worker-bound", ledger.NativeAbility{ID: "sys:info", Command: "uname"})
	if err := worker.Register(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}
	workerAddr := startListener(t, worker, "127.0.0.1:0")

	ws := rawDial(t, workerAddr)
	ts := time.Now().Unix()
	const nonce = "n-elsewhere"
	const elsewhere = "node-elsewhere"
	env, err := bus.NewEnvelope(bus.MsgHello, "attacker", "h-b1", bus.HelloPayload{
		NodeID: "attacker", Ver: "t", Ts: ts, Nonce: nonce,
		To:   elsewhere,
		Sig:  bus.HelloSigN(testSharedSecret, "attacker", ts, nonce),
		SigB: bus.HelloSigB(testSharedSecret, "attacker", elsewhere, ts, nonce),
	})
	if err != nil {
		t.Fatalf("build hello: %v", err)
	}
	if err := ws.WriteJSON(env); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if reason := expectHelloReject(t, ws); reason != "auth" {
		t.Fatalf("hello bound for another node reject reason = %q, want auth", reason)
	}

	worker.mu.RLock()
	defer worker.mu.RUnlock()
	if _, ok := worker.peers["attacker"]; ok {
		t.Fatal("hello bound for another node was registered")
	}
}

// TestAcceptHelloBoundToSelf is the positive half: a hello that names this
// node and carries the bound signature pair authenticates, and the listener
// answers with its own hello — which is itself bound back to the dialer.
func TestAcceptHelloBoundToSelf(t *testing.T) {
	ctx := context.Background()
	worker := newCoreWithNative(t, "worker-bound", ledger.NativeAbility{ID: "sys:info", Command: "uname"})
	if err := worker.Register(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}
	workerAddr := startListener(t, worker, "127.0.0.1:0")

	ws := rawDial(t, workerAddr)
	ts := time.Now().Unix()
	const nonce = "n-bound"
	env, err := bus.NewEnvelope(bus.MsgHello, "dialer", "h-b2", bus.HelloPayload{
		NodeID: "dialer", Ver: "t", Ts: ts, Nonce: nonce,
		To:   worker.nodeID,
		Sig:  bus.HelloSigN(testSharedSecret, "dialer", ts, nonce),
		SigB: bus.HelloSigB(testSharedSecret, "dialer", worker.nodeID, ts, nonce),
	})
	if err != nil {
		t.Fatalf("build hello: %v", err)
	}
	if err := ws.WriteJSON(env); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	var reply bus.Envelope
	if err := ws.ReadJSON(&reply); err != nil {
		t.Fatalf("read hello reply: %v", err)
	}
	if reply.Type != bus.MsgHello {
		t.Fatalf("expected hello reply, got %q", reply.Type)
	}
	var rp bus.HelloPayload
	if err := reply.PayloadInto(&rp); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if rp.To != "dialer" || rp.SigB == "" || rp.EdSigB == "" {
		t.Fatalf("reply must be bound back to the dialer: to=%q sig_b=%t ed_sig_b=%t",
			rp.To, rp.SigB != "", rp.EdSigB != "")
	}
}

// TestRejectDowngradedBoundHello covers the strip-and-replay downgrade: once a
// node has sent a bound hello, an unbound hello under the same id is refused
// rather than falling back to the legacy signature path — otherwise deleting
// three fields from a captured hello would restore the replay hole.
func TestRejectDowngradedBoundHello(t *testing.T) {
	ctx := context.Background()
	worker := newCoreWithNative(t, "worker-bound", ledger.NativeAbility{ID: "sys:info", Command: "uname"})
	if err := worker.Register(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := ledger.RecordHelloBinding(worker.db, "victim"); err != nil {
		t.Fatalf("record binding: %v", err)
	}
	workerAddr := startListener(t, worker, "127.0.0.1:0")

	ws := rawDial(t, workerAddr)
	ts := time.Now().Unix()
	const nonce = "n-stripped"
	env, err := bus.NewEnvelope(bus.MsgHello, "victim", "h-b3", bus.HelloPayload{
		NodeID: "victim", Ver: "t", Ts: ts, Nonce: nonce,
		Sig: bus.HelloSigN(testSharedSecret, "victim", ts, nonce),
	})
	if err != nil {
		t.Fatalf("build hello: %v", err)
	}
	if err := ws.WriteJSON(env); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if reason := expectHelloReject(t, ws); reason != "binding" {
		t.Fatalf("downgraded hello reject reason = %q, want binding", reason)
	}
}

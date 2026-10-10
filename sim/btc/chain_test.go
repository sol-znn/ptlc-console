package btc

import (
	"strings"
	"testing"

	"github.com/btcsuite/btcd/wire"
)

// The simulated chain holds a swap output to the same rules a node does: the
// refund is shut until its height, nobody but Alice opens it, and an output
// is spent once.
func TestChainHoldsTheRefundToItsHeight(t *testing.T) {
	alice, bob := keys(t)
	c := NewChain()
	locktime := uint32(c.Height() + 5)
	out, err := NewSwapOutput(xonly(alice), xonly(bob), locktime, c.Params)
	if err != nil {
		t.Fatal(err)
	}
	funding, err := c.Fund(out.Address, 100_000)
	if err != nil {
		t.Fatal(err)
	}
	op, value, err := FindOutput(funding, out.PkScript)
	if err != nil {
		t.Fatal(err)
	}
	if _, conf, ok := c.Output(op); !ok || conf != 0 {
		t.Fatalf("unconfirmed funding reads as ok=%v with %d confirmations", ok, conf)
	}
	c.Mine(1)
	if _, conf, _ := c.Output(op); conf != 1 {
		t.Fatalf("%d confirmations after one block", conf)
	}
	dest, _ := KeyAddress(alice.PubKey(), c.Params)

	refund := func(signer int) *Spend {
		s, err := NewSpend(op, value, out.PkScript, dest, 500, locktime, wire.MaxTxInSequenceNum-1)
		if err != nil {
			t.Fatal(err)
		}
		key := alice
		if signer == 1 {
			key = bob
		}
		sig, err := s.SignLeaf(out.RefundLeaf, key)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetRefundWitness(out, sig); err != nil {
			t.Fatal(err)
		}
		return s
	}

	if _, err := c.Broadcast(refund(0).Tx); err == nil || !strings.Contains(err.Error(), "non-final") {
		t.Fatalf("a refund before its height was not refused as non-final: %v", err)
	}
	// A lower locktime in the transaction gets past finality and is stopped
	// by the script's own CHECKLOCKTIMEVERIFY.
	early, _ := NewSpend(op, value, out.PkScript, dest, 500, uint32(c.Height()), wire.MaxTxInSequenceNum-1)
	sig, _ := early.SignLeaf(out.RefundLeaf, alice)
	_ = early.SetRefundWitness(out, sig)
	if _, err := c.Broadcast(early.Tx); err == nil {
		t.Fatal("a refund with a locktime below the leaf's was taken")
	}
	c.Mine(int64(locktime) - c.Height())
	if _, err := c.Broadcast(refund(1).Tx); err == nil {
		t.Fatal("Bob's signature opened Alice's refund leaf")
	}
	if _, err := c.Broadcast(refund(0).Tx); err != nil {
		t.Fatalf("Alice's refund at its height: %v", err)
	}
	if _, err := c.Broadcast(refund(0).Tx); err == nil || !strings.Contains(err.Error(), "already spent") {
		t.Fatalf("the output was spent twice: %v", err)
	}
}

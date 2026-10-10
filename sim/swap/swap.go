// Package swap runs the NoM-to-BTC swap of zenon-ptlc/SPEC.md §4 end to end:
// Alice has bitcoin and wants ZNN, Bob the reverse. Both roles run in this
// process and talk over channels; every value that crosses between them is
// one a real client would send over a wire.
//
// It is zenon-ptlc's scenarios/nom-to-btc program made into a package, so
// that a page can run it. The protocol, the checks and the log lines are that
// program's. What changed is what it stands on: the NoM clients are handed in
// instead of dialled, the Bitcoin side is btc.Chain instead of a bitcoind, and
// every wait is on the chain's clock and can be stopped.
package swap

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/wire"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/wallet"

	"ptlc-console/sim/adaptor"
	"ptlc-console/sim/btc"
	"ptlc-console/sim/nom"
)

// terms is what Alice proposes. E is a NoM timestamp, L a Bitcoin height.
type terms struct {
	T        []byte
	AliceX   [32]byte
	AliceNoM types.Address
	Sats     int64
	Znn      *big.Int
	E        int64
	L        uint32
}

type accept struct {
	BobX   [32]byte
	BobBTC btcutil.Address
	BobNoM types.Address
}

type funded struct {
	Outpoint wire.OutPoint
	Value    int64
}

// refusal is Bob's answer to terms he will not take, in place of an accept.
type refusal struct {
	Reason string
}

// claimTerms is what Bob asks Alice to pre-sign: the exact transaction.
type claimTerms struct {
	Dest btcutil.Address
	Fee  int64
}

var errRefused = errors.New("Bob refused the terms")

const (
	claimFee = int64(500)
	// Alice will not reveal with less than this left on the NoM expiry. Her
	// unlock is answered two momentums after it is sent, and one answered
	// after E is refused with t in the clear: Bob reclaims his ZNN and claims
	// the bitcoin with t as well. The CLI and the console keep the same margin.
	revealMargin = int64(60)

	// What Bob assumes of Bitcoin when he weighs L against E. His entry is open
	// to Alice until E, and her refund leaf opens at height L; if L could come
	// first, she could take the bitcoin back and still claim the ZNN. Blocks are
	// taken to come twice as fast as the ten-minute target, which is the side
	// that hurts him, and he wants claimBlocks more after E to get his claim
	// mined once he has seen t.
	fastBlockSeconds = int64(300)
	claimBlocks      = int64(3)
)

// checkTerms is Bob's test of Alice's proposal before he gives her anything.
// now is the NoM chain's clock and tip Bitcoin's height, both read by Bob.
func checkTerms(tm terms, now, tip int64) error {
	if tm.E < now+120 {
		return fmt.Errorf("the NoM expiry is %d s away; Alice needs at least 120 to claim", tm.E-now)
	}
	if _, err := btcec.ParsePubKey(tm.T); err != nil || len(tm.T) != 33 {
		return fmt.Errorf("T is not a compressed point")
	}
	blocks := int64(tm.L) - tip
	need := (tm.E-now+fastBlockSeconds-1)/fastBlockSeconds + claimBlocks
	if blocks < need {
		return fmt.Errorf("her refund opens %d blocks from now (height %d), and the ZNN stays open to her for %d s: at a block every %d s that is before his entry closes plus %d blocks to claim; he needs at least %d",
			blocks, tm.L, tm.E-now, fastBlockSeconds, claimBlocks, need)
	}
	return nil
}

// Options is one run. The zero value of a number takes the default beside it.
type Options struct {
	Scenario string // happy, refund or bad-terms
	Sats     int64  // bitcoin Alice sells; 100,000
	Znn      int64  // ZNN Bob sells; 10
	Expiry   int64  // NoM expiry, seconds after the frontier momentum; 240
	Locktime int64  // Bitcoin refund height, blocks after the tip; 30
	// Blocks Bob waits for on Alice's funding before he locks; 1. One is
	// enough where only this program mines. On a real chain Bob locks ZNN
	// against Alice's funding, and a funding transaction that a
	// reorganisation takes away leaves his ZNN open to her t with nothing on
	// Bitcoin for him: six is the usual figure.
	Confirmations int64
}

// Run plays one swap to its end. cl is the NoM chain and chain the Bitcoin
// one; log receives a line per thing a party did, prefixed with its role. A
// nil error means the scenario ended as that scenario should, which for
// bad-terms is with Bob's refusal.
func Run(cl *nom.Client, chain *btc.Chain, o Options, log func(role, line string)) error {
	def := func(v *int64, d int64) {
		if *v == 0 {
			*v = d
		}
	}
	def(&o.Sats, 100_000)
	def(&o.Znn, 10)
	def(&o.Expiry, 240)
	def(&o.Locktime, 30)
	def(&o.Confirmations, 1)
	switch o.Scenario {
	case "happy", "refund":
	case "bad-terms":
		// a refund two blocks out against an expiry minutes away
		o.Locktime = 2
	default:
		return fmt.Errorf("scenario is happy, refund or bad-terms, not %q", o.Scenario)
	}
	logf := func(role string) func(string, ...interface{}) {
		return func(f string, a ...interface{}) { log(role, fmt.Sprintf(f, a...)) }
	}

	aliceKey, err := nom.DevKey(1)
	if err != nil {
		return err
	}
	bobKey, err := nom.DevKey(3)
	if err != nil {
		return err
	}
	aliceBTC, err := btcec.NewPrivateKey()
	if err != nil {
		return err
	}
	bobBTC, err := btcec.NewPrivateKey()
	if err != nil {
		return err
	}

	toBob := make(chan interface{}, 8)
	toAlice := make(chan interface{}, 8)
	errs := make(chan error, 2)
	// A party that fails stops sending, and the other would wait on its
	// channel for ever. Closing gone wakes it.
	gone := make(chan struct{})

	a := &alice{chain: chain, nom: cl, key: aliceKey, btcKey: aliceBTC, in: toAlice, out: toBob, gone: gone, log: logf("alice"), reveal: o.Scenario == "happy", conf: o.Confirmations}
	b := &bob{chain: chain, nom: cl, key: bobKey, btcKey: bobBTC, in: toBob, out: toAlice, gone: gone, log: logf("bob"), conf: o.Confirmations}

	go func() { errs <- a.run(o.Sats, big.NewInt(o.Znn*nom.Zexp), o.Expiry, uint32(o.Locktime)) }()
	go func() { errs <- b.run() }()
	refused := 0
	var failed error
	for i := 0; i < 2; i++ {
		err := <-errs
		switch {
		case errors.Is(err, errRefused):
			refused++
		case err != nil && failed == nil:
			failed = err
			close(gone)
		}
	}
	switch {
	case failed != nil && !errors.Is(failed, errGone):
		return failed
	case failed != nil:
		return errors.New("one party stopped and the other was left waiting")
	case o.Scenario == "bad-terms" && refused != 2:
		return errors.New("Bob took terms he should have refused")
	case o.Scenario != "bad-terms" && refused != 0:
		return errRefused
	}
	return nil
}

var errGone = errors.New("the other party stopped")

func recv(in <-chan interface{}, gone <-chan struct{}) (interface{}, error) {
	select {
	case m := <-in:
		return m, nil
	case <-gone:
		return nil, errGone
	}
}

func xonly(k *btcec.PrivateKey) [32]byte {
	var x [32]byte
	copy(x[:], schnorr.SerializePubKey(k.PubKey()))
	return x
}

// ---- Alice ----------------------------------------------------------------

type alice struct {
	chain  *btc.Chain
	nom    *nom.Client
	key    *wallet.KeyPair
	btcKey *btcec.PrivateKey
	in     <-chan interface{}
	out    chan<- interface{}
	gone   <-chan struct{}
	log    func(string, ...interface{})
	reveal bool
	conf   int64
}

func (a *alice) run(sats int64, znn *big.Int, expiryAfter int64, locktimeAfter uint32) error {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return err
	}
	T, err := adaptor.PointOf(secret[:])
	if err != nil {
		return err
	}
	now, err := a.nom.Now()
	if err != nil {
		return err
	}
	tip := a.chain.Height()
	tm := terms{T: T, AliceX: xonly(a.btcKey), AliceNoM: a.key.Address, Sats: sats, Znn: znn, E: now + expiryAfter, L: uint32(tip) + locktimeAfter}
	a.log("proposes: T=%x, %d sats for %s ZNN, E=%d (%ds out), L=%d (%d blocks out)", T, sats, znn, tm.E, expiryAfter, tm.L, locktimeAfter)
	a.out <- tm

	m, err := recv(a.in, a.gone)
	if err != nil {
		return err
	}
	var acc accept
	switch m := m.(type) {
	case accept:
		acc = m
	case refusal:
		a.log("Bob refused the terms: %s; nothing was locked", m.Reason)
		return errRefused
	}
	out, err := btc.NewSwapOutput(tm.AliceX, acc.BobX, tm.L, a.chain.Params)
	if err != nil {
		return err
	}
	a.log("funds %s (%d sats)", out.Address, sats)
	tx, err := a.chain.Fund(out.Address, sats)
	if err != nil {
		return fmt.Errorf("fund: %w", err)
	}
	a.chain.Mine(a.conf)
	op, value, err := btc.FindOutput(tx, out.PkScript)
	if err != nil {
		return err
	}
	a.log("funded %s:%d, confirmed", op.Hash, op.Index)
	a.out <- funded{Outpoint: op, Value: value}

	if m, err = recv(a.in, a.gone); err != nil {
		return err
	}
	ct := m.(claimTerms)
	claim, err := btc.NewSpend(op, value, out.PkScript, ct.Dest, ct.Fee, 0, wire.MaxTxInSequenceNum)
	if err != nil {
		return err
	}
	h, err := claim.Sighash(out.ClaimLeaf)
	if err != nil {
		return err
	}
	var aux [32]byte
	if _, err := rand.Read(aux[:]); err != nil {
		return err
	}
	presig, err := adaptor.Sign(a.btcKey, h, T, aux)
	if err != nil {
		return err
	}
	a.log("pre-signs Bob's claim on T (sighash %x)", h[:8])
	a.out <- presig

	if m, err = recv(a.in, a.gone); err != nil {
		return err
	}
	id := m.(types.Hash)
	entry, err := a.nom.WaitEntry(id)
	if err != nil {
		return err
	}
	a.log("sees entry %s", nom.Summary(entry))
	switch {
	case !bytes.Equal(entry.PointLock, T):
		return fmt.Errorf("entry locks to %x, not T", entry.PointLock)
	case entry.Destination != a.key.Address:
		return fmt.Errorf("entry pays %s, not me", entry.Destination)
	case entry.ExpirationTime != tm.E:
		return fmt.Errorf("entry expires at %d, not %d", entry.ExpirationTime, tm.E)
	case entry.Amount.Cmp(znn) != 0 || entry.TokenStandard != types.ZnnTokenStandard:
		return fmt.Errorf("entry holds %s %s", entry.Amount, entry.TokenStandard)
	}

	if !a.reveal {
		a.log("refund scenario: never reveals t")
		return a.refund(out, op, value, tm.L)
	}

	now, err = a.nom.Now()
	if err != nil {
		return err
	}
	if now > tm.E-revealMargin {
		return fmt.Errorf("too close to expiry to reveal (now %d, E %d)", now, tm.E)
	}
	before, _ := a.nom.Balance(a.key.Address, types.ZnnTokenStandard)
	a.log("reveals t by unlocking %s", id)
	unlockHash, err := a.nom.Unlock(a.key, id, secret[:])
	if err != nil {
		return err
	}
	ok, err := a.nom.WaitContract(unlockHash)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the contract refused the unlock")
	}
	if _, err := a.nom.ReceiveAll(a.key); err != nil {
		return err
	}
	after, _ := a.nom.Balance(a.key.Address, types.ZnnTokenStandard)
	a.log("received %s ZNN (balance %s -> %s)", new(big.Int).Sub(after, before), before, after)
	return nil
}

func (a *alice) refund(out *btc.SwapOutput, op wire.OutPoint, value int64, L uint32) error {
	if tip := a.chain.Height(); uint32(tip) < L {
		a.log("waits for height %d (tip %d)", L, tip)
		a.chain.Mine(int64(L) - tip)
	}
	dest, err := btc.KeyAddress(a.btcKey.PubKey(), a.chain.Params)
	if err != nil {
		return err
	}
	refund, err := btc.NewSpend(op, value, out.PkScript, dest, claimFee, L, wire.MaxTxInSequenceNum-1)
	if err != nil {
		return err
	}
	sig, err := refund.SignLeaf(out.RefundLeaf, a.btcKey)
	if err != nil {
		return err
	}
	if err := refund.SetRefundWitness(out, sig); err != nil {
		return err
	}
	txid, err := a.chain.Broadcast(refund.Tx)
	if err != nil {
		return fmt.Errorf("refund broadcast: %w", err)
	}
	a.chain.Mine(1)
	a.log("refunded in %s", txid)
	return nil
}

// ---- Bob ------------------------------------------------------------------

type bob struct {
	chain  *btc.Chain
	nom    *nom.Client
	key    *wallet.KeyPair
	btcKey *btcec.PrivateKey
	in     <-chan interface{}
	out    chan<- interface{}
	gone   <-chan struct{}
	log    func(string, ...interface{})
	conf   int64
}

func (b *bob) run() error {
	m, err := recv(b.in, b.gone)
	if err != nil {
		return err
	}
	tm := m.(terms)
	now, err := b.nom.Now()
	if err != nil {
		return err
	}
	if err := checkTerms(tm, now, b.chain.Height()); err != nil {
		b.log("refuses the terms before locking anything: %v", err)
		b.out <- refusal{Reason: err.Error()}
		return errRefused
	}
	dest, err := btc.KeyAddress(b.btcKey.PubKey(), b.chain.Params)
	if err != nil {
		return err
	}
	b.log("accepts: %d sats for %s ZNN, pays bitcoin to %s", tm.Sats, tm.Znn, dest)
	b.out <- accept{BobX: xonly(b.btcKey), BobBTC: dest, BobNoM: b.key.Address}

	out, err := btc.NewSwapOutput(tm.AliceX, xonly(b.btcKey), tm.L, b.chain.Params)
	if err != nil {
		return err
	}
	if m, err = recv(b.in, b.gone); err != nil {
		return err
	}
	f := m.(funded)
	txOut, confirmations, ok := b.chain.Output(f.Outpoint)
	if !ok || confirmations < b.conf {
		return fmt.Errorf("funding has %d confirmations, fewer than the %d Bob waits for", confirmations, b.conf)
	}
	if !bytes.Equal(txOut.PkScript, out.PkScript) || txOut.Value != tm.Sats {
		return fmt.Errorf("funding output does not match the terms")
	}
	b.log("checked the funding: %d sats at %s:%d", f.Value, f.Outpoint.Hash, f.Outpoint.Index)

	claim, err := btc.NewSpend(f.Outpoint, f.Value, out.PkScript, dest, claimFee, 0, wire.MaxTxInSequenceNum)
	if err != nil {
		return err
	}
	b.out <- claimTerms{Dest: dest, Fee: claimFee}
	if m, err = recv(b.in, b.gone); err != nil {
		return err
	}
	presig := m.([]byte)
	h, err := claim.Sighash(out.ClaimLeaf)
	if err != nil {
		return err
	}
	if !adaptor.Verify(tm.AliceX[:], h, tm.T, presig) {
		return fmt.Errorf("Alice's pre-signature does not verify")
	}
	b.log("verified the pre-signature: completes only with t")

	heightBefore, err := b.nom.ContractHeight()
	if err != nil {
		return err
	}
	b.log("locks %s ZNN to T for %s until %d", tm.Znn, tm.AliceNoM, tm.E)
	id, err := b.nom.CreatePoint(b.key, tm.T, tm.AliceNoM, tm.E, types.ZnnTokenStandard, tm.Znn)
	if err != nil {
		return err
	}
	if _, err := b.nom.WaitEntry(id); err != nil {
		return err
	}
	b.log("entry %s is live", id)
	b.out <- id

	// wait for t until E, and the two momentums an unlock sent just before E
	// takes to be answered; after E the entry is his to reclaim
	witness, err := b.nom.WaitWitness(id, heightBefore, tm.E+30)
	if err != nil {
		if b.nom.Stopped() {
			return err
		}
		b.log("no reveal: %v", err)
		return b.reclaim(id, tm.E)
	}
	T2, err := adaptor.PointOf(witness)
	if err != nil || !bytes.Equal(T2, tm.T) {
		return fmt.Errorf("witness is not the scalar behind T")
	}
	b.log("read t from the chain: %x", witness)

	aliceSig, err := adaptor.Complete(presig, witness)
	if err != nil {
		return err
	}
	bobSig, err := claim.SignLeaf(out.ClaimLeaf, b.btcKey)
	if err != nil {
		return err
	}
	if err := claim.SetClaimWitness(out, aliceSig, bobSig); err != nil {
		return err
	}
	if err := claim.Execute(int32(tm.L) - 1); err != nil {
		return fmt.Errorf("claim fails locally: %w", err)
	}
	txid, err := b.chain.Broadcast(claim.Tx)
	if err != nil {
		return fmt.Errorf("claim broadcast: %w", err)
	}
	b.chain.Mine(1)
	if _, confirmations, ok := b.chain.Output(wire.OutPoint{Hash: *txid}); !ok || confirmations < 1 {
		return errors.New("claim not confirmed")
	}
	b.log("claimed %d sats in %s; on Bitcoin only an ordinary signature appears", f.Value-claimFee, txid)
	return nil
}

func (b *bob) reclaim(id types.Hash, E int64) error {
	for {
		now, err := b.nom.Now()
		if err != nil {
			return err
		}
		if now >= E {
			break
		}
		if err := b.nom.Sleep(b.nom.Poll()); err != nil {
			return err
		}
	}
	h, err := b.nom.Reclaim(b.key, id)
	if err != nil {
		return err
	}
	ok, err := b.nom.WaitContract(h)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("reclaim refused")
	}
	if _, err := b.nom.ReceiveAll(b.key); err != nil {
		return err
	}
	b.log("reclaimed the ZNN after expiry")
	return nil
}

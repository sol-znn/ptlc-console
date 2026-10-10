package btc

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// Chain is a Bitcoin chain reduced to what a swap leans on, for a page that
// has no bitcoind to talk to: unspent outputs, a mempool, a height, and the
// two checks that decide whether a spend is valid. It is a stand-in for
// regtest, and the parts are not all equally real:
//
//   - Scripts are real. Every input of a broadcast transaction is run through
//     btcd's script engine with the standard verification flags, the engine
//     Spend.Execute uses, so a Taproot leaf, a Schnorr signature and
//     OP_CHECKLOCKTIMEVERIFY are checked as a node checks them.
//   - Locktime is real. A transaction that is not final for the next block is
//     refused, by btcd's own IsFinalizedTransaction: that, with the script's
//     CLTV, is what keeps Alice's refund shut until its height.
//   - Blocks are not. There is no proof of work, no header, no fork and no
//     fee market; Mine counts up and confirms the mempool. Nothing a swap
//     checks depends on those, and a real chain's reorganisations are exactly
//     what this cannot show.
//   - Funding is not. Fund makes coins from nothing, as a regtest wallet that
//     has mined a hundred blocks effectively does.
type Chain struct {
	Params *chaincfg.Params

	mu      sync.Mutex
	height  int64
	utxo    map[wire.OutPoint]*coin
	mempool []*wire.MsgTx
	minted  uint32
}

type coin struct {
	out *wire.TxOut
	// The height of the block that confirmed it; 0 while it is in the mempool.
	height int64
}

// A locktime below 500,000,000 is a height and is never compared with this.
var blockTimeUnused = time.Unix(0, 0)

// NewChain starts at height 101, where a regtest node's first coinbase has
// just matured.
func NewChain() *Chain {
	return &Chain{Params: &chaincfg.RegressionNetParams, height: 101, utxo: map[wire.OutPoint]*coin{}}
}

func (c *Chain) Height() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.height
}

// Fund pays sats to addr in a new, unconfirmed transaction and returns it.
func (c *Chain) Fund(addr btcutil.Address, sats int64) (*wire.MsgTx, error) {
	script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.minted++
	tx := wire.NewMsgTx(2)
	// An input that spends nothing that exists: the index makes each funding
	// transaction a different one.
	tx.AddTxIn(&wire.TxIn{PreviousOutPoint: wire.OutPoint{Index: c.minted}, Sequence: wire.MaxTxInSequenceNum})
	tx.AddTxOut(wire.NewTxOut(sats, script))
	c.accept(tx)
	return tx, nil
}

func (c *Chain) accept(tx *wire.MsgTx) {
	hash := tx.TxHash()
	for i, out := range tx.TxOut {
		c.utxo[wire.OutPoint{Hash: hash, Index: uint32(i)}] = &coin{out: out}
	}
	c.mempool = append(c.mempool, tx)
}

// Mine makes n blocks. The first confirms everything in the mempool.
func (c *Chain) Mine(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := int64(0); i < n; i++ {
		c.height++
		for _, tx := range c.mempool {
			hash := tx.TxHash()
			for i := range tx.TxOut {
				if coin, ok := c.utxo[wire.OutPoint{Hash: hash, Index: uint32(i)}]; ok {
					coin.height = c.height
				}
			}
		}
		c.mempool = nil
	}
}

// Output is an unspent output and how many blocks have confirmed it; ok is
// false when there is none, spent ones included.
func (c *Chain) Output(op wire.OutPoint) (out *wire.TxOut, confirmations int64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	coin, ok := c.utxo[op]
	if !ok {
		return nil, 0, false
	}
	if coin.height > 0 {
		confirmations = c.height - coin.height + 1
	}
	return coin.out, confirmations, true
}

// Broadcast checks a transaction as a node's mempool would, as far as a swap
// depends on it, and takes it if it passes: every input unspent, the
// transaction final for the next block, outputs no larger than inputs, and
// every script satisfied.
func (c *Chain) Broadcast(tx *wire.MsgTx) (*chainhash.Hash, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(tx.TxIn) == 0 || len(tx.TxOut) == 0 {
		return nil, errors.New("a transaction needs an input and an output")
	}
	// Block time is not modelled. A locktime here is always a height, which
	// is what the swap's refund uses.
	if !blockchain.IsFinalizedTransaction(btcutil.NewTx(tx), int32(c.height+1), blockTimeUnused) {
		return nil, fmt.Errorf("non-final: locktime %d, and the next block is %d", tx.LockTime, c.height+1)
	}
	prevOuts := map[wire.OutPoint]*wire.TxOut{}
	var in, out int64
	for _, txIn := range tx.TxIn {
		coin, ok := c.utxo[txIn.PreviousOutPoint]
		if !ok {
			return nil, fmt.Errorf("input %s is missing or already spent", txIn.PreviousOutPoint)
		}
		if _, twice := prevOuts[txIn.PreviousOutPoint]; twice {
			return nil, fmt.Errorf("input %s is spent twice", txIn.PreviousOutPoint)
		}
		prevOuts[txIn.PreviousOutPoint] = coin.out
		in += coin.out.Value
	}
	for _, txOut := range tx.TxOut {
		if txOut.Value < 0 {
			return nil, errors.New("negative output")
		}
		out += txOut.Value
	}
	if out > in {
		return nil, fmt.Errorf("outputs of %d sats exceed inputs of %d", out, in)
	}
	fetcher := txscript.NewMultiPrevOutFetcher(prevOuts)
	hashes := txscript.NewTxSigHashes(tx, fetcher)
	for i, txIn := range tx.TxIn {
		prev := prevOuts[txIn.PreviousOutPoint]
		vm, err := txscript.NewEngine(prev.PkScript, tx, i, txscript.StandardVerifyFlags, nil, hashes, prev.Value, fetcher)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", i, err)
		}
		if err := vm.Execute(); err != nil {
			return nil, fmt.Errorf("input %d: %w", i, err)
		}
	}
	for _, txIn := range tx.TxIn {
		delete(c.utxo, txIn.PreviousOutPoint)
	}
	c.accept(tx)
	hash := tx.TxHash()
	return &hash, nil
}

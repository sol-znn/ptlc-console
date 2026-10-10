// Package chain is a whole Network of Momentum in one process: go-zenon's own
// chain, consensus, virtual machine and embedded contracts, started from the
// devnet's genesis, kept in memory, and answering the node's JSON-RPC.
//
// It is what the page talks to when there is no node to talk to. Nothing in
// it is a model of the ptlc contract: it is the contract, the same package a
// node runs, built for the browser. What is left out is everything a single
// process has no use for: peers, sync, and the wait for a pillar's slot. A
// momentum is made when Step is called, by the pillar whose turn it is, ten
// seconds of the chain's clock after the one before.
//
// The shape follows go-zenon's zenon/mock, which its own contract tests run
// on, with the two things a test supplies taken out: a *testing.T, and a
// directory on disk for the database. The database is the node's own LevelDB
// manager, opened on memory (overlay/README.md).
package chain

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/inconshreveable/log15"
	"github.com/tyler-smith/go-bip39"

	zchain "github.com/zenon-network/go-zenon/chain"
	cache "github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/chain/genesis"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/pillar"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/rpc"
	rpcserver "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon"
)

// The devnet's genesis as the go-zenon branch commits it (docker/devnet):
// chain 69, three pillars, the ptlc spork active from the first momentum, and
// two accounts of the mnemonic below funded and fused.
//
//go:embed genesis.json
var devnetGenesis []byte

// DevMnemonic is the devnet's, committed in go-zenon. Indexes 1 and 3 hold
// the funds; 0, 4 and 6 are the three pillars' producing keys.
const DevMnemonic = "abstract affair idle position alien fluid board ordinary exist afraid chapter wood wood guide sun walnut crew perfect place firm poverty model side million"

var pillarIndexes = []uint32{0, 4, 6}

// SlotSeconds is how far the chain's clock moves with each momentum.
const SlotSeconds = 10

// Chain is one running chain. It is also the zenon.Zenon the RPC services
// are built on, and the broadcaster a pillar hands its momentum to.
type Chain struct {
	// One momentum at a time, and no call answered while one is being made:
	// the page reads the frontier and then what hangs off it, and should not
	// have it move in between.
	mu sync.RWMutex

	chain     zchain.Chain
	consensus consensus.Consensus
	pillars   []pillar.Manager
	server    *rpcserver.Server
	client    *rpcserver.Client

	genesisTime time.Time
}

// The chain's clock is its frontier momentum's timestamp. Everything in
// go-zenon that asks the time asks common.Clock, so a block is judged against
// the chain it is on and never against the browser's clock, which runs at
// another rate here.
type frontierClock struct{ c *Chain }

func (f frontierClock) Now() time.Time {
	if s := f.c.chain.GetFrontierMomentumStore(); s != nil {
		if m, err := s.GetFrontierMomentum(); err == nil {
			return *m.Timestamp
		}
	}
	return f.c.genesisTime
}

// New starts a chain at the devnet's genesis, unchanged: the same genesis
// momentum, hash for hash, that a devnet node starts from, so the chain is the
// same chain every time the page is opened.
//
// That genesis is stamped 2025-02-19, and the chain's clock is left there on
// purpose. go-zenon's verifier refuses a momentum stamped more than ten
// seconds ahead of the machine's clock, so a chain can run faster than real
// time only while its own clock is behind, and this one makes a momentum
// whenever it is asked to. Starting in the past is what gives it the room.
func New() (c *Chain, err error) {
	// go-zenon reports a broken invariant by panicking. Starting is the one
	// place that can be turned into an error the page can show.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the chain did not start: %v", r)
		}
	}()

	cfg := new(genesis.GenesisConfig)
	if err := json.Unmarshal(devnetGenesis, cfg); err != nil {
		return nil, fmt.Errorf("the embedded genesis is not readable: %w", err)
	}
	if err := genesis.CheckGenesis(cfg); err != nil {
		return nil, fmt.Errorf("the embedded genesis is not valid: %w", err)
	}

	// Errors only, and to the console: every momentum otherwise logs a page.
	quiet := log15.LvlFilterHandler(log15.LvlError, log15.StderrHandler)
	for _, l := range []common.Logger{
		common.ZenonLogger, common.ChainLogger, common.ConsensusLogger, common.SupervisorLogger,
		common.PillarLogger, common.RPCLogger, common.EmbeddedLogger, common.VmLogger,
		common.VerifierLogger, common.ProtocolLogger,
	} {
		l.SetHandler(quiet)
	}

	c = &Chain{genesisTime: time.Unix(cfg.GenesisTimestampSec, 0)}
	c.chain = zchain.NewChain(db.NewLevelDBManagerInMemory(), cache.NewCacheDBManagerInMemory(), genesis.NewGenesis(cfg))
	// "testing" is what go-zenon calls a consensus that does not start its
	// own ticker: momentums come when Step asks for them.
	c.consensus = consensus.NewConsensus(db.NewMemDB(), c.chain, true)
	common.Clock = frontierClock{c}

	seed := bip39.NewSeed(DevMnemonic, "")
	for _, index := range pillarIndexes {
		key, err := wallet.DeriveWithIndex(index, seed)
		if err != nil {
			return nil, err
		}
		p := pillar.NewPillar(c.chain, c.consensus, c)
		p.SetCoinBase(key)
		c.pillars = append(c.pillars, p)
	}

	if err := c.Init(); err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}

	c.server = rpcserver.NewServer()
	for _, a := range rpc.GetApis(c, nil, "ledger", "embedded") {
		if err := c.server.RegisterName(a.Namespace, a.Service); err != nil {
			return nil, err
		}
	}
	// go-zenon's own stats service reads the host and the peer table. There
	// is neither here, and the page asks only how many peers there are.
	if err := c.server.RegisterName("stats", &stats{c}); err != nil {
		return nil, err
	}
	c.client = rpcserver.DialInProc(c.server)
	return c, nil
}

// Step makes the next momentum: the pillar whose slot it is collects every
// account block published since the last one, the contracts answer the calls
// the last one confirmed, and the chain's clock moves ten seconds.
func (c *Chain) Step() (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("momentum not made: %v", r)
		}
	}()

	previous, err := c.chain.GetFrontierMomentumStore().GetFrontierMomentum()
	if err != nil {
		return err
	}
	at := previous.Timestamp.Add(SlotSeconds * time.Second)
	if at.After(time.Now()) {
		// The verifier would refuse it. Reaching this takes running flat out
		// for as long as the genesis is old.
		return errors.New("the chain's clock has caught up with this machine's, and a momentum cannot be stamped in the future; it can go on at one every ten seconds")
	}
	expected, err := c.consensus.GetMomentumProducer(at)
	if err != nil {
		return err
	}
	if expected == nil {
		return errors.New("no pillar is due for the next slot")
	}
	for _, p := range c.pillars {
		if *p.GetCoinBase() != *expected {
			continue
		}
		task := p.Process(consensus.ProducerEvent{Producer: *expected, StartTime: at, EndTime: at.Add(SlotSeconds * time.Second)})
		if task == nil {
			return errors.New("the pillar is stopped")
		}
		task.Wait()
		return nil
	}
	return fmt.Errorf("the slot belongs to %s, which is not one of this chain's pillars", expected)
}

// Height and Now are the frontier momentum's.
func (c *Chain) Height() uint64 {
	return c.chain.GetFrontierMomentumStore().Identifier().Height
}

func (c *Chain) Now() time.Time { return common.Clock.Now() }

// Client is a client of this chain's RPC, in process. The scenario runner
// uses it exactly as it would use a connection to a node.
func (c *Chain) Client() *rpcserver.Client { return c.client }

// Serve answers one JSON-RPC request body, single or batch, as the node's
// HTTP port would.
func (c *Chain) Serve(body []byte) []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	answer := httptest.NewRecorder()
	c.server.ServeHTTP(answer, request)
	if answer.Code != http.StatusOK && answer.Body.Len() == 0 {
		out, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0", "id": nil,
			"error": map[string]interface{}{"code": -32600, "message": fmt.Sprintf("the request was not taken (HTTP %d)", answer.Code)},
		})
		return out
	}
	return bytes.TrimSpace(answer.Body.Bytes())
}

// ---- zenon.Zenon ---------------------------------------------------------------

func (c *Chain) Init() error {
	if err := c.chain.Init(); err != nil {
		return err
	}
	if err := c.consensus.Init(); err != nil {
		return err
	}
	for _, p := range c.pillars {
		if err := p.Init(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Chain) Start() error {
	if err := c.chain.Start(); err != nil {
		return err
	}
	if err := c.consensus.Start(); err != nil {
		return err
	}
	for _, p := range c.pillars {
		if err := p.Start(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Chain) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client.Close()
	c.server.Stop()
	for _, p := range c.pillars {
		if err := p.Stop(); err != nil {
			return err
		}
	}
	if err := c.consensus.Stop(); err != nil {
		return err
	}
	return c.chain.Stop()
}

func (c *Chain) Chain() zchain.Chain                 { return c.chain }
func (c *Chain) Consensus() consensus.Consensus      { return c.consensus }
func (c *Chain) Verifier() verifier.Verifier         { return nil }
func (c *Chain) Protocol() *protocol.ProtocolManager { return nil }
func (c *Chain) Producer() pillar.Manager            { return nil }
func (c *Chain) Config() *zenon.Config               { return nil }
func (c *Chain) Broadcaster() protocol.Broadcaster   { return c }

// ---- protocol.Broadcaster ------------------------------------------------------

// A node broadcasts what it made and what it was handed; here there is nobody
// to tell, so both are inserted and that is all.

func (c *Chain) SyncInfo() *protocol.SyncInfo {
	return &protocol.SyncInfo{State: protocol.SyncDone}
}

func (c *Chain) CreateMomentum(tx *nom.MomentumTransaction, detailed *nom.DetailedMomentum) {
	insert := c.chain.AcquireInsert("sim create-momentum")
	defer insert.Unlock()
	if err := c.chain.UpdateCache(insert, detailed, tx.Changes); err != nil {
		panic(fmt.Errorf("the chain's cache refused its own momentum: %w", err))
	}
	if err := c.chain.AddMomentumTransaction(insert, tx); err != nil {
		panic(fmt.Errorf("the chain refused its own momentum: %w", err))
	}
}

func (c *Chain) CreateAccountBlock(tx *nom.AccountBlockTransaction) error {
	insert := c.chain.AcquireInsert("sim create-account-block")
	defer insert.Unlock()
	return c.chain.AddAccountBlockTransaction(insert, tx)
}

// ---- stats ---------------------------------------------------------------------

type stats struct{ c *Chain }

type NetworkInfo struct {
	NumPeers int           `json:"numPeers"`
	Peers    []interface{} `json:"peers"`
	Self     interface{}   `json:"self"`
}

func (s *stats) NetworkInfo() (*NetworkInfo, error) {
	return &NetworkInfo{Peers: []interface{}{}, Self: map[string]string{"publicKey": "", "ip": "in this page"}}, nil
}

func (s *stats) SyncInfo() (*protocol.SyncInfo, error) {
	height := s.c.Height()
	return &protocol.SyncInfo{State: protocol.SyncDone, CurrentHeight: height, TargetHeight: height}, nil
}

type ProcessInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func (s *stats) ProcessInfo() (*ProcessInfo, error) {
	return &ProcessInfo{Version: "in-browser chain", Commit: GoZenonTree}, nil
}

// GoZenonTree is the tree of the patched go-zenon this was built from, set by
// the build (-ldflags -X) from go-zenon/PIN.
var GoZenonTree = "unknown"

var _ zenon.Zenon = (*Chain)(nil)
var _ protocol.Broadcaster = (*Chain)(nil)
var _ = types.PtlcContract

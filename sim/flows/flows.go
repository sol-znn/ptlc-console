// Package flows runs one named ptlc scenario against a chain and reports each
// step as it happens. It signs every block itself with the devnet's keys, so
// that a page can show the contract doing all it does with no wallet, no CLI
// and no tool on the visitor's side.
//
// It is zenon-ptlc's scenarios/nom-to-btc/cmd/flows made into a package: there
// the lab's back end runs it as a program and reads what it prints, and here
// it is compiled into the page and prints to a function. The catalogue, the
// scenarios and the checks are that program's, line for line.
//
// What it prints is a log, with one kind of line a program reads: "@flow "
// followed by a JSON object, either a step changing state or, last, the
// result. Everything else is for a person.
//
// A scenario passes when the contract did what the catalogue says at every
// step, refusals included. "Bob tries a wrong secret" passes when the contract
// refuses it; if the contract pays him, that is the failure.
package flows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/zenon-network/go-zenon/common/types"
	rpcclient "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/wallet"

	"ptlc-console/sim/btc"
	"ptlc-console/sim/nom"
)

// What a line beginning "@flow " carries.
type event struct {
	Step   string `json:"step,omitempty"`
	State  string `json:"state,omitempty"`  // active, sent, confirmed, done, failed
	Answer string `json:"answer,omitempty"` // accepted, refused, rejected: what a call met
	Detail string `json:"detail,omitempty"`
	Hash   string `json:"hash,omitempty"`
	Until  int64  `json:"until,omitempty"`  // a wait's deadline, unix seconds on the chain's clock
	Now    int64  `json:"now,omitempty"`    // the chain's clock when the wait began
	Result string `json:"result,omitempty"` // pass or fail: the last line
}

type runner struct {
	cl      *nom.Client
	sc      *scenario
	bitcoin *btc.Chain

	// Where the log goes, a line at a time. Steps of a swap are reported by
	// two parties at once, so it is called under a lock.
	out   sync.Mutex
	print func(string)

	alice, bob, carol *wallet.KeyPair

	mu      sync.Mutex
	settled map[string]bool
}

func (r *runner) emit(e event) {
	body, _ := json.Marshal(e)
	r.out.Lock()
	r.print("@flow " + string(body))
	r.out.Unlock()
}

func (r *runner) say(format string, args ...interface{}) {
	r.out.Lock()
	r.print(fmt.Sprintf(format, args...))
	r.out.Unlock()
}

var (
	oneZnn = big.NewInt(1 * nom.Zexp)
	tenQsr = big.NewInt(10 * nom.Zexp)
)

func (r *runner) name(a types.Address) string {
	switch a {
	case r.alice.Address:
		return "Alice"
	case r.bob.Address:
		return "Bob"
	case r.carol.Address:
		return "Carol"
	case types.PtlcContract:
		return "the contract"
	}
	return a.String()
}

func coins(amount *big.Int, token types.ZenonTokenStandard) string {
	symbol := token.String()
	switch token {
	case types.ZnnTokenStandard:
		symbol = "ZNN"
	case types.QsrTokenStandard:
		symbol = "QSR"
	}
	whole := new(big.Int).Quo(amount, big.NewInt(nom.Zexp))
	rest := new(big.Int).Rem(amount, big.NewInt(nom.Zexp))
	if rest.Sign() == 0 {
		return whole.String() + " " + symbol
	}
	return strings.TrimRight(fmt.Sprintf("%s.%08d", whole, rest), "0") + " " + symbol
}

func (r *runner) active(step, detail string) {
	r.emit(event{Step: step, State: "active", Detail: detail})
}

func (r *runner) done(step, detail string) {
	r.mu.Lock()
	r.settled[step] = true
	r.mu.Unlock()
	r.emit(event{Step: step, State: "done", Detail: detail})
}

func (r *runner) fail(step, detail string) error {
	r.emit(event{Step: step, State: "failed", Detail: detail})
	return errors.New(detail)
}

// A step a party takes alone or hands to another: nothing to wait for.
func (r *runner) note(step, detail string) {
	r.active(step, "")
	r.done(step, detail)
}

// call makes one call to the contract and holds it to what the catalogue
// expects. The three outcomes are different things on the chain: accepted and
// refused are the contract's answer a momentum after the call; rejected is the
// node declining the block, so that nothing was ever sent.
func (r *runner) call(step string, send func() (types.Hash, error)) (types.Hash, error) {
	expect, because := "", ""
	for _, s := range r.sc.Steps {
		if s.ID == step {
			expect, because = s.Expect, s.Because
		}
	}
	r.active(step, "")

	var hash types.Hash
	var err error
	for attempt := 1; ; attempt++ {
		hash, err = send()
		// A block the node will not take is the expected end of some steps.
		// For the others it is usually a race for the account's next height
		// with someone else signing as the same devnet key, so it is tried
		// again.
		if err == nil || expect == rejected || attempt == 4 {
			break
		}
		// An error can also come after the node took the block, a timeout on
		// the answer. Sent again, a Create would lock the funds twice.
		if hash != (types.Hash{}) {
			if _, lookErr := r.cl.Block(hash); lookErr == nil {
				r.say("%s: the node answered with an error (%v) but has the block; not sending it again", step, err)
				err = nil
				break
			}
		}
		r.say("%s: the node did not take the block (%v); trying again", step, err)
		if r.cl.Sleep(time.Duration(attempt)*3*r.cl.Poll()/4) != nil {
			break
		}
	}
	if err != nil {
		if expect == rejected {
			// Turned away, but for the reason the step is about: a node that is
			// down turns every block away too.
			if because != "" && !strings.Contains(err.Error(), because) {
				return types.Hash{}, r.fail(step, "The network turned it away, but not for the reason this step shows ("+because+"): "+reason(err))
			}
			r.mu.Lock()
			r.settled[step] = true
			r.mu.Unlock()
			r.emit(event{Step: step, State: "done", Answer: rejected, Detail: "The network would not take it: " + reason(err) + ". Nothing left the account."})
			return types.Hash{}, nil
		}
		return hash, r.fail(step, "The network would not take the block: "+reason(err))
	}
	if expect == rejected {
		return hash, r.fail(step, "The network took a block it should have turned away")
	}
	r.emit(event{Step: step, State: "sent", Hash: hash.String()})

	block, err := r.cl.WaitBlock(hash)
	if err != nil {
		return hash, r.fail(step, "The block never made it onto the chain")
	}
	r.emit(event{Step: step, State: "confirmed", Hash: hash.String(), Detail: fmt.Sprintf("On the chain at height %d", block.ConfirmationDetail.MomentumHeight)})

	ok, err := r.cl.WaitContract(hash)
	if err != nil {
		return hash, r.fail(step, "The contract never answered")
	}
	answer := refused
	if ok {
		answer = accepted
	}
	if answer != expect {
		if ok {
			return hash, r.fail(step, "The contract accepted this. It should have refused.")
		}
		return hash, r.fail(step, "The contract refused this. It should have accepted.")
	}
	r.mu.Lock()
	r.settled[step] = true
	r.mu.Unlock()
	detail := "The contract accepted"
	if !ok {
		detail = "The contract refused, as it should"
	}
	r.emit(event{Step: step, State: "done", Answer: answer, Hash: hash.String(), Detail: detail})
	return hash, nil
}

// reason is a node error with go-zenon's wrapping taken off.
func reason(err error) string {
	text := err.Error()
	if i := strings.LastIndex(text, ": "); i >= 0 && i+2 < len(text) {
		text = text[i+2:]
	}
	return text
}

// paid checks that the contract, answering the call, sent amount of token to
// one address and nothing to anyone else.
func (r *runner) paid(step string, call types.Hash, to types.Address, amount *big.Int, token types.ZenonTokenStandard) error {
	r.active(step, "")
	// The contract's own send is a descendant of its receive, visible as soon
	// as the receive is.
	var got []nom.Paid
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		if got, err = r.cl.ContractPaid(call); err == nil && len(got) > 0 {
			break
		}
		if r.cl.Sleep(r.cl.Poll()/2) != nil {
			break
		}
	}
	if err != nil {
		return r.fail(step, "Could not read what the contract paid: "+err.Error())
	}
	if len(got) != 1 {
		return r.fail(step, fmt.Sprintf("The contract made %d payments; one was expected", len(got)))
	}
	p := got[0]
	if p.To != to || p.Token != token || p.Amount.Cmp(amount) != 0 {
		return r.fail(step, fmt.Sprintf("The contract paid %s to %s; %s to %s was expected", coins(p.Amount, p.Token), r.name(p.To), coins(amount, token), r.name(to)))
	}
	r.done(step, fmt.Sprintf("%s paid to %s", coins(amount, token), r.name(to)))
	return nil
}

// waitUntil waits for the chain's own clock to reach a deadline. The contract
// judges expiry by the timestamp of the momentum it runs in, not by anybody's
// wall clock, so that is the clock read here.
func (r *runner) waitUntil(step string, deadline int64) error {
	now, err := r.cl.Now()
	if err != nil {
		return r.fail(step, "Could not read the chain's clock")
	}
	r.emit(event{Step: step, State: "active", Until: deadline, Now: now})
	for now < deadline {
		if r.cl.Sleep(r.cl.Poll()) != nil {
			return r.fail(step, "Stopped while waiting for the deadline")
		}
		if now, err = r.cl.Now(); err != nil {
			return r.fail(step, "Could not read the chain's clock")
		}
	}
	r.done(step, "The deadline has passed on the chain's clock")
	return nil
}

// together runs calls that do not depend on each other at once. They are made
// by different accounts, so neither waits for the other's block.
func together(calls ...func() error) error {
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	for i, c := range calls {
		wg.Add(1)
		go func(i int, c func() error) {
			defer wg.Done()
			errs[i] = c()
		}(i, c)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// needCarol gives the third account plasma the first time it is wanted. Alice
// and Bob are fused in genesis; Carol is not, and this client does no proof of
// work. She needs no funds, only to be able to send a call.
func (r *runner) needCarol() error {
	const enough = 150_000
	_, max, err := r.cl.Plasma(r.carol.Address)
	if err != nil {
		return fmt.Errorf("reading Carol's plasma: %w", err)
	}
	if max >= enough {
		return nil
	}
	r.say("first run on this chain: Alice fuses 200 QSR so that Carol can send calls")
	hash, err := r.cl.Fuse(r.alice, r.carol.Address, big.NewInt(200*nom.Zexp))
	if err != nil {
		return fmt.Errorf("fusing plasma for Carol: %w", err)
	}
	if ok, err := r.cl.WaitContract(hash); err != nil || !ok {
		return fmt.Errorf("the plasma contract did not fuse for Carol (%v)", err)
	}
	for attempt := 0; attempt < 30; attempt++ {
		if _, max, _ = r.cl.Plasma(r.carol.Address); max >= enough {
			return nil
		}
		if err := r.cl.Sleep(r.cl.Poll()); err != nil {
			return err
		}
	}
	return errors.New("Carol still has no plasma after the fuse was accepted")
}

// need makes sure key can spend amount of token, receiving what is waiting for
// it if it cannot. Payouts arrive as unreceived blocks, and a run that only
// ever locked would spend an account down while its funds sat unreceived.
func (r *runner) need(key *wallet.KeyPair, amount *big.Int, token types.ZenonTokenStandard) error {
	balance, err := r.cl.Balance(key.Address, token)
	if err != nil {
		return err
	}
	if balance.Cmp(amount) >= 0 {
		return nil
	}
	r.say("%s has %s, less than the %s this needs; receiving what is waiting", r.name(key.Address), coins(balance, token), coins(amount, token))
	if _, err := r.cl.ReceiveAll(key); err != nil {
		return fmt.Errorf("receiving for %s: %w", r.name(key.Address), err)
	}
	if err := r.cl.Sleep(3 * r.cl.Poll() / 2); err != nil {
		return err
	}
	if balance, err = r.cl.Balance(key.Address, token); err != nil {
		return err
	}
	if balance.Cmp(amount) < 0 {
		return fmt.Errorf("%s has %s and needs %s. The devnet's accounts have been spent down; restart the chain from genesis", r.name(key.Address), coins(balance, token), coins(amount, token))
	}
	return nil
}

// finish holds the run to the catalogue: every step it promised was reported.
func (r *runner) finish() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sc.Steps {
		if !r.settled[s.ID] {
			return fmt.Errorf("the run ended without reaching %q", s.Label)
		}
	}
	return nil
}

// Catalogue is every scenario there is, as the page draws them: JSON, with
// the groups they are shown in.
func Catalogue() []byte {
	body, _ := json.Marshal(map[string]interface{}{"groups": groups, "scenarios": scenarios})
	return body
}

// Env is what a run stands on.
type Env struct {
	// The node: a connection to its RPC, made by whoever knows where it is.
	Node *rpcclient.Client
	// How long a wait sleeps before it looks again. Two seconds suits a
	// devnet's ten-second momentums; a chain in the page is quicker.
	Poll time.Duration
	// The Bitcoin chain the Bitcoin scenarios use.
	Bitcoin *btc.Chain
	// Print receives the log, a line at a time.
	Print func(line string)
}

// Run plays one scenario to its end and reports whether it passed. The last
// line it prints is the result either way. Cancelling ctx stops it at its
// next call or wait, and that is a failed run.
func Run(ctx context.Context, id string, env Env) bool {
	sc := find(id)
	if sc == nil {
		env.Print(fmt.Sprintf("no scenario %q", id))
		return false
	}
	r := &runner{sc: sc, bitcoin: env.Bitcoin, print: env.Print, settled: map[string]bool{}}
	r.cl = nom.On(ctx, env.Node, env.Poll, func(string, ...interface{}) {})
	summary, err := r.start()
	if err == nil {
		err = r.finish()
	}
	if ctx.Err() != nil {
		err = errors.New("the run was stopped before it finished")
	}
	if err != nil {
		r.emit(event{Result: "fail", Detail: err.Error()})
		return false
	}
	r.emit(event{Result: "pass", Detail: summary})
	return true
}

func (r *runner) start() (string, error) {
	if err := r.cl.Ready(); err != nil {
		return "", fmt.Errorf("the node is not answering: %w", err)
	}
	for index, into := range map[uint32]**wallet.KeyPair{1: &r.alice, 3: &r.bob, 2: &r.carol} {
		key, err := nom.DevKey(index)
		if err != nil {
			return "", err
		}
		*into = key
	}
	r.say("%s: Alice %s, Bob %s, Carol %s", r.sc.Title, r.alice.Address, r.bob.Address, r.carol.Address)
	for _, party := range r.sc.Parties {
		if party == carol {
			if err := r.needCarol(); err != nil {
				return "", err
			}
		}
	}
	return r.sc.run(r)
}

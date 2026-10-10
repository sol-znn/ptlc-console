//go:build js && wasm

// The chain and the scenarios, as one WebAssembly module for the page.
//
// It puts one object on the JavaScript global, `ptlcSim`, and then waits for
// ever: a Go program that returns takes its exported functions with it. The
// page loads it in a worker (web/src/sim/worker.ts), so that a momentum being
// made never holds up a frame.
//
//	ptlcSim.build                 {commit, tree} of the go-zenon this is
//	ptlcSim.catalogue()           the scenarios, as JSON
//	ptlcSim.start()               Promise: start the chain (once)
//	ptlcSim.rpc(body)             Promise<string>: one JSON-RPC request to it
//	ptlcSim.pace(ms)              a momentum every ms milliseconds; 0 stops
//	ptlcSim.step()                Promise: one momentum now
//	ptlcSim.run(id, node, pollMs, onLine)
//	                              start a scenario; {stop(), done: Promise<bool>}
//
// `node` in run is null for the chain in this module, or a function
// (body: string) => Promise<string> that carries one JSON-RPC request to a
// node somewhere else. The scenario runner is then the same code against
// either: it signs with the devnet's keys and publishes through whatever it
// was handed.
//
// Everything that can block returns a Promise and does its work in a
// goroutine. A function called from JavaScript runs on the event loop, and
// one that waits there for a lock or a channel stops the whole module.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"

	rpcclient "github.com/zenon-network/go-zenon/rpc/server"

	"ptlc-console/sim/btc"
	"ptlc-console/sim/chain"
	"ptlc-console/sim/flows"
)

// Set by the build from go-zenon/PIN (-ldflags -X).
var goZenonCommit = "unknown"

var (
	mu      sync.Mutex
	theSim  *chain.Chain
	bitcoin = btc.NewChain()

	// Milliseconds between momentums; 0 is stopped. Read by the ticker.
	paceMs atomic.Int64
	// A scenario owns the two dev accounts for as long as it runs.
	running atomic.Bool
)

func promise(work func() (interface{}, error)) js.Value {
	var executor js.Func
	executor = js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		resolve, reject := args[0], args[1]
		go func() {
			defer executor.Release()
			defer func() {
				if r := recover(); r != nil {
					reject.Invoke(js.Global().Get("Error").New(fmt.Sprint(r)))
				}
			}()
			value, err := work()
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
				return
			}
			resolve.Invoke(value)
		}()
		return nil
	})
	return js.Global().Get("Promise").New(executor)
}

// await blocks the calling goroutine, never the event loop, until a
// JavaScript promise settles.
func await(ctx context.Context, p js.Value) (js.Value, error) {
	type settled struct {
		value js.Value
		err   error
	}
	done := make(chan settled, 1)
	then := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		done <- settled{value: args[0]}
		return nil
	})
	catch := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		message := "the request failed"
		if len(args) > 0 {
			if m := args[0].Get("message"); m.Type() == js.TypeString {
				message = m.String()
			} else {
				message = args[0].Call("toString").String()
			}
		}
		done <- settled{err: errors.New(message)}
		return nil
	})
	defer then.Release()
	defer catch.Release()
	p.Call("then", then, catch)
	select {
	case s := <-done:
		return s.value, s.err
	case <-ctx.Done():
		return js.Undefined(), ctx.Err()
	}
}

func sim() (*chain.Chain, error) {
	mu.Lock()
	defer mu.Unlock()
	if theSim == nil {
		return nil, errors.New("the chain has not been started")
	}
	return theSim, nil
}

func start(js.Value, []js.Value) interface{} {
	return promise(func() (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if theSim != nil {
			return nil, nil
		}
		c, err := chain.New()
		if err != nil {
			return nil, err
		}
		// A few momentums at once, so that the page opens on a chain that
		// already has a history to draw and clients do not wait for one.
		for i := 0; i < 6; i++ {
			if err := c.Step(); err != nil {
				return nil, err
			}
		}
		theSim = c
		go tick(c)
		return nil, nil
	})
}

// tick makes momentums at the pace the page set. It sleeps in short pieces so
// that a change of pace is felt at once.
func tick(c *chain.Chain) {
	last := time.Now()
	for {
		time.Sleep(25 * time.Millisecond)
		pace := paceMs.Load()
		if pace <= 0 {
			last = time.Now()
			continue
		}
		if time.Since(last) < time.Duration(pace)*time.Millisecond {
			continue
		}
		last = time.Now()
		if err := c.Step(); err != nil {
			js.Global().Get("console").Call("error", "ptlc sim: "+err.Error())
			paceMs.Store(0)
		}
	}
}

func rpc(_ js.Value, args []js.Value) interface{} {
	body := []byte(args[0].String())
	return promise(func() (interface{}, error) {
		c, err := sim()
		if err != nil {
			return nil, err
		}
		return string(c.Serve(body)), nil
	})
}

func pace(_ js.Value, args []js.Value) interface{} {
	ms := int64(args[0].Int())
	if ms < 0 {
		ms = 0
	}
	paceMs.Store(ms)
	return nil
}

func step(js.Value, []js.Value) interface{} {
	return promise(func() (interface{}, error) {
		c, err := sim()
		if err != nil {
			return nil, err
		}
		return nil, c.Step()
	})
}

// remote carries the RPC client's requests to a node through a function of
// the page's: the page knows how to reach the node (HTTP, a WebSocket, a
// wallet's node) and this module does not have to.
type remote struct {
	ctx  context.Context
	send js.Value
}

func (r remote) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	answer, err := await(r.ctx, r.send.Invoke(string(body)))
	if err != nil {
		return nil, err
	}
	return &http.Response{
		Status: "200 OK", StatusCode: http.StatusOK, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body:    io.NopCloser(bytes.NewReader([]byte(answer.String()))),
		Request: req,
	}, nil
}

func run(_ js.Value, args []js.Value) interface{} {
	id, node, pollMs, onLine := args[0].String(), args[1], args[2].Int(), args[3]
	ctx, cancel := context.WithCancel(context.Background())

	done := promise(func() (interface{}, error) {
		defer cancel()
		if !running.CompareAndSwap(false, true) {
			return nil, errors.New("another scenario is still running and holds both dev accounts")
		}
		defer running.Store(false)

		var client *rpcclient.Client
		if node.Type() == js.TypeFunction {
			c, err := rpcclient.DialHTTPWithClient("http://node.invalid/", &http.Client{Transport: remote{ctx, node}})
			if err != nil {
				return nil, err
			}
			defer c.Close()
			client = c
		} else {
			c, err := sim()
			if err != nil {
				return nil, err
			}
			client = c.Client()
		}
		if pollMs < 10 {
			pollMs = 10
		}
		passed := flows.Run(ctx, id, flows.Env{
			Node: client, Poll: time.Duration(pollMs) * time.Millisecond, Bitcoin: bitcoin,
			Print: func(line string) { onLine.Invoke(line) },
		})
		return passed, nil
	})

	handle := js.Global().Get("Object").New()
	handle.Set("done", done)
	handle.Set("stop", js.FuncOf(func(js.Value, []js.Value) interface{} {
		cancel()
		return nil
	}))
	return handle
}

func main() {
	api := js.Global().Get("Object").New()
	build := js.Global().Get("Object").New()
	build.Set("commit", goZenonCommit)
	build.Set("tree", chain.GoZenonTree)
	api.Set("build", build)
	api.Set("catalogue", js.FuncOf(func(js.Value, []js.Value) interface{} { return string(flows.Catalogue()) }))
	api.Set("start", js.FuncOf(start))
	api.Set("rpc", js.FuncOf(rpc))
	api.Set("pace", js.FuncOf(pace))
	api.Set("step", js.FuncOf(step))
	api.Set("run", js.FuncOf(run))
	api.Set("bitcoinHeight", js.FuncOf(func(js.Value, []js.Value) interface{} { return int(bitcoin.Height()) }))
	js.Global().Set("ptlcSim", api)
	select {}
}

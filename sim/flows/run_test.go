package flows

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"ptlc-console/sim/btc"
	"ptlc-console/sim/chain"
)

// Every scenario in the catalogue, run to its end on the chain the page runs
// when it has no node: go-zenon's own, in this process, with a momentum made
// every few milliseconds. This is the test that the page's offline mode does
// what the lab does on a devnet: the same scenarios, the same checks, the
// same contract code.
func TestEveryScenarioPassesOnTheInProcessChain(t *testing.T) {
	c, err := chain.New()
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var ticking sync.WaitGroup
	ticking.Add(1)
	go func() {
		defer ticking.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(15 * time.Millisecond):
				if err := c.Step(); err != nil {
					t.Errorf("momentum: %v", err)
					return
				}
			}
		}
	}()
	defer func() {
		close(stop)
		ticking.Wait()
		_ = c.Stop()
	}()

	bitcoin := btc.NewChain()
	for _, sc := range scenarios {
		t.Run(sc.ID, func(t *testing.T) {
			var log []string
			began := c.Now()
			passed := Run(context.Background(), sc.ID, Env{
				Node: c.Client(), Poll: 10 * time.Millisecond, Bitcoin: bitcoin,
				Print: func(line string) { log = append(log, line) },
			})
			if !passed {
				t.Fatalf("did not pass:\n%s", strings.Join(log, "\n"))
			}
			if last := log[len(log)-1]; !strings.HasPrefix(last, `@flow {"detail":`) || !strings.Contains(last, `"result":"pass"`) {
				t.Fatalf("the last line is not the result: %s", last)
			}
			t.Logf("%d s on the chain's clock", int(c.Now().Sub(began).Seconds()))
		})
	}
}

// A run that is stopped ends, and ends as a failure.
func TestAStoppedRunFails(t *testing.T) {
	c, err := chain.New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	for i := 0; i < 5; i++ {
		if err := c.Step(); err != nil {
			t.Fatal(err)
		}
	}
	// No momentums are made from here on, so the first call waits for ever.
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var log []string
	done := make(chan bool)
	go func() {
		done <- Run(ctx, "secret", Env{Node: c.Client(), Poll: 10 * time.Millisecond, Bitcoin: btc.NewChain(), Print: func(line string) {
			mu.Lock()
			log = append(log, line)
			mu.Unlock()
		}})
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case passed := <-done:
		if passed {
			t.Fatal("a stopped run passed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end when it was stopped")
	}
	if last := log[len(log)-1]; !strings.Contains(last, `"result":"fail"`) {
		t.Fatalf("the last line is not a failed result: %s", last)
	}
}

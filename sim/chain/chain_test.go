package chain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func call(t *testing.T, c *Chain, method string, params ...interface{}) json.RawMessage {
	t.Helper()
	if params == nil {
		params = []interface{}{}
	}
	body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	var answer struct {
		Result json.RawMessage
		Error  *struct{ Message string }
	}
	raw := c.Serve(body)
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("%s answered %q: %v", method, raw, err)
	}
	if answer.Error != nil {
		t.Fatalf("%s: %s", method, answer.Error.Message)
	}
	return answer.Result
}

// The chain starts from the devnet's genesis, makes momentums on request ten
// seconds apart, and answers the node's RPC with the ptlc spork active.
func TestChainStartsAndAnswers(t *testing.T) {
	start := time.Unix(1739923200, 0) // GenesisTimestampSec of the devnet's genesis
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	for i := 0; i < 5; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("momentum %d: %v", i+2, err)
		}
	}
	var m struct {
		Height          uint64
		Timestamp       int64
		ChainIdentifier uint64
	}
	if err := json.Unmarshal(call(t, c, "ledger.getFrontierMomentum"), &m); err != nil {
		t.Fatal(err)
	}
	if m.Height != 6 || m.ChainIdentifier != 69 || m.Timestamp != start.Unix()+5*SlotSeconds {
		t.Fatalf("frontier is %+v", m)
	}
	sporks := string(call(t, c, "embedded.spork.getAll", 0, 10))
	if !strings.Contains(sporks, `"name":"ptlc"`) {
		t.Fatalf("no ptlc spork in %s", sporks)
	}
	info := string(call(t, c, "ledger.getAccountInfoByAddress", "z1qq6eg8n43g032hanpsfp02qcdmv7zfj3y2lt5d"))
	if !strings.Contains(info, `"balance":"1000000000000"`) {
		t.Fatalf("Alice's genesis balance is missing from %s", info)
	}
	if peers := string(call(t, c, "stats.networkInfo")); !strings.Contains(peers, `"numPeers":0`) {
		t.Fatalf("stats.networkInfo answered %s", peers)
	}
}

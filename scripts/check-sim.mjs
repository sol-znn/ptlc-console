// Loads the built WebAssembly module in Node, as the page's worker loads it in
// a browser, and holds it to what the page needs of it: the chain starts,
// answers the node's RPC, makes momentums at the pace it is given, and runs
// scenarios to a pass. `go test ./...` in sim/ proves the same code natively;
// this proves the module that ships.
//
//   node scripts/check-sim.mjs              three scenarios, a few seconds
//   node scripts/check-sim.mjs --all        every scenario in the catalogue

import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const dir = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'web', 'src', 'sim', 'gen')
await import('file://' + path.join(dir, 'wasm_exec.js'))
const go = new globalThis.Go()
const { instance } = await WebAssembly.instantiate(await readFile(path.join(dir, 'ptlc.wasm')), go.importObject)
void go.run(instance)
const sim = globalThis.ptlcSim
if (!sim) throw new Error('the module did not set ptlcSim')

let failed = 0
const check = (ok, what) => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${what}`)
  if (!ok) failed++
}
const call = async (method, params = []) => {
  const answer = JSON.parse(await sim.rpc(JSON.stringify({ jsonrpc: '2.0', id: 1, method, params })))
  if (answer.error) throw new Error(`${method}: ${answer.error.message}`)
  return answer.result
}

console.log(`go-zenon ${sim.build.commit.slice(0, 7)}, tree ${sim.build.tree.slice(0, 8)}`)
const began = Date.now()
await sim.start()
const first = await call('ledger.getFrontierMomentum')
check(first.chainIdentifier === 69 && first.height >= 6, `the chain started: chain ${first.chainIdentifier}, momentum ${first.height}, in ${Date.now() - began} ms`)

const sporks = await call('embedded.spork.getAll', [0, 10])
check(sporks.list.some((s) => s.name === 'ptlc' && s.activated), 'the ptlc spork is active')

sim.pace(40)
await new Promise((r) => setTimeout(r, 1000))
const later = await call('ledger.getFrontierMomentum')
check(later.height >= first.height + 10, `momentums come at the pace set: ${later.height - first.height} in a second at 40 ms`)
check(later.timestamp - first.timestamp === (later.height - first.height) * 10, 'each is ten seconds of the chain clock after the last')

const catalogue = JSON.parse(sim.catalogue())
const wanted = process.argv.includes('--all') ? catalogue.scenarios.map((s) => s.id) : ['secret', 'deadline', 'swap-btc']
for (const id of wanted) {
  const lines = []
  const t0 = Date.now()
  const passed = await sim.run(id, null, 10, (line) => lines.push(line)).done
  const last = lines.at(-1) ?? ''
  check(passed === true && last.includes('"result":"pass"'), `${id} passes (${Date.now() - t0} ms)`)
  if (!passed) console.log(lines.map((l) => '       ' + l).join('\n'))
}

// A scenario against "a node somewhere else": the same chain, reached through
// a function the way the page reaches a devnet. This is the path a live node
// takes, with the transport swapped for one that cannot fail.
{
  const lines = []
  let requests = 0
  const passed = await sim.run('secret', (body) => (requests++, sim.rpc(body)), 10, (line) => lines.push(line)).done
  check(passed === true && requests > 5, `secret passes through a transport function (${requests} requests)`)
  if (!passed) console.log(lines.map((l) => '       ' + l).join('\n'))
}

// Stopped part way, a run ends as a failure and frees the accounts.
{
  sim.pace(0)
  const lines = []
  const run = sim.run('secret', null, 10, (line) => lines.push(line))
  await new Promise((r) => setTimeout(r, 300))
  run.stop()
  const passed = await run.done
  check(passed === false && (lines.at(-1) ?? '').includes('"result":"fail"'), 'a stopped run ends as a failure')
  sim.pace(40)
  check((await sim.run('secret', null, 10, () => {}).done) === true, 'and the next run starts')
}

console.log(failed ? `${failed} failed` : 'all passed')
process.exit(failed ? 1 : 0)

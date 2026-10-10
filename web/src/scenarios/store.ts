// The scenarios page's state: the catalogue, the newest run of each scenario,
// and which one, if any, is running now.
//
// Runs live here, in the tab. The runner is the Go program the lab's back end
// runs, compiled into the page (../sim); this starts it, is handed each line
// it prints, and folds them into what a card shows. One scenario runs at a
// time: they all sign as the same two devnet accounts, and an account chain
// takes one block at a time. A reload forgets every result, and on the chain
// in the page it forgets the chain too.
//
// The console is the same document (../Root.vue) and reads the same node
// through the same transport, so what a run puts on the chain is on the
// console's tape and in its Entries bench as it happens.

import { reactive } from 'vue'

import * as nodes from '../node/node.ts'
import { node } from '../node/node.ts'
import * as sim from '../sim/host.ts'
import { ZenonClient } from '../znn/client.ts'
import { apply, close, newRun, parse, type Catalogue, type RunLine, type RunView, type Scenario } from './flow.ts'

/** Alice, index 1 of the devnet mnemonic: the account every scenario spends from first. */
const ALICE = 'z1qq6eg8n43g032hanpsfp02qcdmv7zfj3y2lt5d'
const ZNN = 'zts1znnxxxxxxxxxxxxx9z4ulx'

export const state = reactive({
  catalogue: null as Catalogue | null,
  /** Why there is nothing to show, when there is nothing to show. */
  error: '',
  /** The newest run of each scenario since the page was opened. */
  runs: {} as Record<string, RunView>,
  /** The scenario that holds the dev accounts now. */
  busy: null as { id: string; title: string; flow: string } | null,
  /** The scenario whose flow is unfolded. */
  open: '',
  startError: {} as Record<string, string>,
  starting: '',
  /** The scenarios a Run all or a section's Run has still to start, in order. */
  queue: [] as string[],
  /** Which button that queue came from: 'all', a group's id, or nothing. */
  batch: '',
  momentum: null as { height: number; timestamp: number } | null,
  nodeDown: false,
  nodeError: '',
  /**
   * Whether the devnet's keys hold anything on this chain; null until read.
   * On a chain that is not a go-zenon devnet they hold nothing, and every
   * scenario would stop at its first step.
   */
  funded: null as boolean | null,
})

const scenario = (id: string): Scenario | undefined => state.catalogue?.scenarios.find((s) => s.id === id)
const client = new ZenonClient(nodes.transport)

// ---- following a run -----------------------------------------------------------

// Lines are shown a beat apart. A step a party takes alone is printed as
// started and finished in the same millisecond, and a page that applied both
// at once would show a step that was never seen to happen. On the chain in
// the page a whole scenario can also be over in a second or two, faster than
// it can be read; the beat is what makes it something to watch.
const BEAT = 420

interface Following {
  queue: (RunLine | { passed: boolean })[]
  timer: number
  stop(): void
}
const followers = new Map<string, Following>()

function pump(run: RunView): void {
  const f = followers.get(run.id)
  if (!f || f.timer) return
  const next = (): void => {
    f.timer = 0
    const item = f.queue.shift()
    if (!item) return
    if ('passed' in item) {
      // The runner's last line is its result; one that ended without it was
      // stopped or could not start.
      close(run, item.passed ? 0 : 1, Date.now())
      followers.delete(run.id)
      if (state.busy?.id === run.id) state.busy = null
      void readChain()
      void advance()
      return
    }
    apply(run, item)
    const paced = parse(item.text)?.step !== undefined
    if (paced) f.timer = window.setTimeout(next, BEAT)
    else next()
  }
  next()
}

let counter = 0

export async function run(id: string): Promise<void> {
  const sc = scenario(id)
  if (!sc || state.starting || state.busy) return
  state.starting = id
  delete state.startError[id]
  state.open = id
  try {
    const runId = `r${++counter}`
    const view = newRun(runId, sc, Date.now())
    let n = 0
    // A devnet makes a momentum every ten seconds and is asked every two.
    // The chain in the page is asked a few times per momentum.
    const pollMs = node.kind === 'sim' ? (node.pace > 0 ? Math.max(40, Math.min(400, node.pace / 4)) : 200) : 2000
    const running = sim.run(id, nodes.runnerNode(), pollMs, (text, at) => {
      f.queue.push({ n: n++, at, stream: 'out', text })
      pump(state.runs[id])
    })
    const f: Following = { queue: [], timer: 0, stop: running.stop }
    followers.set(runId, f)
    state.runs[id] = view
    state.busy = { id: runId, title: sc.title, flow: id }
    running.done.then(
      (passed) => {
        f.queue.push({ passed })
        pump(state.runs[id])
      },
      (err) => {
        f.queue.push({ n: n++, at: Date.now(), stream: 'lab', text: err instanceof Error ? err.message : String(err) })
        f.queue.push({ passed: false })
        pump(state.runs[id])
      },
    )
  } catch (err) {
    state.startError[id] = err instanceof Error ? err.message : String(err)
  } finally {
    state.starting = ''
  }
}

export function stop(runId: string): void {
  // Stop means stop: a queue that went on to the next card would leave the
  // person who pressed it watching another run start.
  state.queue = []
  followers.get(runId)?.stop()
}

// A batch is a queue of single runs. Scenarios cannot overlap, so each one is
// started when the one before it closes, whether that passed or failed: a
// batch is there to fill in the tally, and stopping at the first failure
// would hide the rest.
async function advance(): Promise<void> {
  while (state.queue.length && !state.busy && !state.starting) await run(state.queue.shift() as string)
  if (!state.queue.length && !state.busy) state.batch = ''
}

export function runBatch(batch: string, ids: string[]): void {
  if (state.starting || state.busy || state.queue.length || ids.length === 0) return
  state.batch = batch
  state.queue = [...ids]
  void advance()
}

export function stopBatch(): void {
  state.queue = []
  if (state.busy) stop(state.busy.id)
  else state.batch = ''
}

// ---- the chain -------------------------------------------------------------------

let reading = false
let failures = 0

async function readChain(): Promise<void> {
  if (reading) return
  reading = true
  const asked = `${node.kind} ${node.url}`
  try {
    const m = await client.frontierMomentum()
    if (asked !== `${node.kind} ${node.url}`) return
    if (m.height !== state.momentum?.height) state.momentum = { height: m.height, timestamp: m.timestamp }
    state.nodeDown = false
    state.nodeError = ''
    failures = 0
    if (state.funded === null) {
      const info = await client.accountInfo(ALICE)
      state.funded = BigInt(info.balanceInfoMap?.[ZNN]?.balance ?? '0') > 0n
    }
  } catch (err) {
    if (asked !== `${node.kind} ${node.url}`) return
    state.nodeDown = true
    state.nodeError = err instanceof Error ? err.message : String(err)
    if (++failures >= 2) nodes.unreachable(state.nodeError)
  } finally {
    reading = false
  }
}

export async function start(): Promise<void> {
  nodes.onChange(() => {
    // Another chain: its height, and whether the keys hold anything on it,
    // are to be read again. Results stay; they say which chain they ran on
    // only by having run, so a run in flight is stopped rather than left to
    // finish against a node the page no longer shows.
    stopBatch()
    state.momentum = null
    state.funded = null
    state.nodeDown = false
    failures = 0
    void readChain()
  })
  nodes.begin()
  const loop = (): void => {
    void readChain().finally(() => window.setTimeout(loop, node.kind === 'sim' ? 500 : 3000))
  }
  loop()
  try {
    // The catalogue is the runner's own list, read out of the module, so the
    // page cannot draw a scenario the runner does not have.
    state.catalogue = (await sim.catalogue<Catalogue>()).catalogue
  } catch (err) {
    state.error = err instanceof Error ? err.message : String(err)
  }
}

export const tally = (): { pass: number; fail: number; running: number; none: number } => {
  const out = { pass: 0, fail: 0, running: 0, none: 0 }
  for (const sc of state.catalogue?.scenarios ?? []) {
    const r = state.runs[sc.id]
    if (!r) out.none++
    else out[r.status]++
  }
  return out
}

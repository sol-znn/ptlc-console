// The chain that runs in this page, from the page's side: starts the worker
// the first time anything asks for it, and turns its messages back into
// promises. Nothing is downloaded until then, so a visitor who stays on a
// live node never fetches the module.

import type { Transport } from '../node/transport.ts'
import type { Answer, Build, Request } from './protocol.ts'

type Outgoing = Request extends infer R ? (R extends { n: number } ? Omit<R, 'n'> : never) : never

let worker: Worker | null = null
let counter = 0
const waiting = new Map<number, { resolve: (value: unknown) => void; reject: (err: Error) => void; onLine?: (line: string, at: number) => void }>()

function boot(): Worker {
  if (worker) return worker
  worker = new Worker(new URL('./worker.ts', import.meta.url), { type: 'module', name: 'ptlc-chain' })
  worker.onmessage = (event: MessageEvent<Answer>) => {
    const answer = event.data
    const w = waiting.get(answer.n)
    if (!w) return
    if ('line' in answer) {
      w.onLine?.(answer.line, answer.at)
      return
    }
    waiting.delete(answer.n)
    if (answer.ok) w.resolve(answer.value)
    else w.reject(new Error(answer.error))
  }
  worker.onerror = (event) => {
    // The module failed to load or the worker died. Every request in flight
    // is lost, and the next one starts a new worker with an empty chain.
    const why = new Error(`the in-page chain stopped: ${event.message || 'the worker failed to load'}`)
    for (const [, w] of waiting) w.reject(why)
    waiting.clear()
    worker?.terminate()
    worker = null
    started = null
  }
  return worker
}

function ask<T>(request: Outgoing, onLine?: (line: string, at: number) => void): { n: number; answer: Promise<T> } {
  const n = ++counter
  const answer = new Promise<T>((resolve, reject) => {
    waiting.set(n, { resolve: resolve as (value: unknown) => void, reject, onLine })
    boot().postMessage({ ...request, n })
  })
  return { n, answer }
}

let started: Promise<Build> | null = null

/** Starts the chain once. Resolves with what it was built from. */
export function start(): Promise<Build> {
  started ??= ask<Build>({ op: 'start' }).answer
  started.catch(() => (started = null))
  return started
}

/** A momentum every `ms` milliseconds; 0 stops the chain where it is. */
export function pace(ms: number): void {
  void ask({ op: 'pace', ms }).answer.catch(() => undefined)
}

export function step(): Promise<void> {
  return ask<void>({ op: 'step' }).answer
}

/** The chain as something a client can talk to, like any other node. */
export const transport: Transport = {
  async send(body) {
    await start()
    return ask<string>({ op: 'rpc', body }).answer
  },
  close() {},
}

/** The scenarios the module can run. It does not need the chain started. */
export function catalogue<T>(): Promise<{ catalogue: T; build: Build }> {
  return ask<{ catalogue: T; build: Build }>({ op: 'catalogue' }).answer
}

export interface Running {
  /** Whether it passed. Rejects only if the run could not start at all. */
  done: Promise<boolean>
  stop(): void
}

/**
 * Runs one scenario, against the chain in this page when `node` is null and
 * against the node at that address otherwise. The module signs every block
 * with the devnet's keys either way.
 */
export function run(scenario: string, node: string | null, pollMs: number, onLine: (line: string, at: number) => void): Running {
  let n = 0
  let stopped = false
  // The chain has to be up before a run on it is asked for; a run against a
  // node elsewhere needs only the module.
  const ready: Promise<unknown> = node === null ? start() : Promise.resolve()
  const done = ready.then(() => {
    if (stopped) return false
    const asked = ask<boolean>({ op: 'run', scenario, node, pollMs }, onLine)
    n = asked.n
    return asked.answer
  })
  return {
    done,
    stop: () => {
      stopped = true
      if (n) void ask({ op: 'stop', run: n }).answer.catch(() => undefined)
    },
  }
}

/// <reference lib="webworker" />
// The worker that holds the chain. It loads the WebAssembly module built from
// ../../../sim (go-zenon's chain, virtual machine and contracts, and the
// scenario runner) and answers the page's requests to it.
//
// It is a worker so that a momentum being made, or thirty megabytes of
// WebAssembly being compiled, never holds up a frame of the page.

// The glue that belongs to the Go that compiled the module. It puts `Go` on
// the global; the build copies it beside the module (scripts/build-sim.sh).
import './gen/wasm_exec.js'
import wasmUrl from './gen/ptlc.wasm?url'

import { transportFor, type Transport } from '../node/transport.ts'
import type { Answer, Build, Request } from './protocol.ts'

interface RunHandle {
  done: Promise<boolean>
  stop(): void
}

interface Sim {
  build: Build
  catalogue(): string
  start(): Promise<void>
  rpc(body: string): Promise<string>
  pace(ms: number): void
  step(): Promise<void>
  run(id: string, node: null | ((body: string) => Promise<string>), pollMs: number, onLine: (line: string) => void): RunHandle
}

declare const Go: new () => { importObject: WebAssembly.Imports; run(instance: WebAssembly.Instance): Promise<void> }
declare const ptlcSim: Sim | undefined

const scope = self as unknown as DedicatedWorkerGlobalScope
const post = (answer: Answer) => scope.postMessage(answer)

async function load(): Promise<Sim> {
  const go = new Go()
  let instance: WebAssembly.Instance
  try {
    instance = (await WebAssembly.instantiateStreaming(fetch(wasmUrl), go.importObject)).instance
  } catch {
    // A server that does not call the file application/wasm.
    const bytes = await (await fetch(wasmUrl)).arrayBuffer()
    instance = (await WebAssembly.instantiate(bytes, go.importObject)).instance
  }
  // Runs for as long as the worker lives; the module sets ptlcSim before it
  // first yields, which is before this line returns.
  void go.run(instance)
  if (typeof ptlcSim === 'undefined') throw new Error('the chain module loaded and exported nothing')
  return ptlcSim
}

const sim = load()
const runs = new Map<number, RunHandle>()

async function handle(request: Request): Promise<unknown> {
  const s = await sim
  switch (request.op) {
    case 'start':
      await s.start()
      return s.build
    case 'rpc':
      return s.rpc(request.body)
    case 'pace':
      s.pace(request.ms)
      return null
    case 'step':
      return s.step()
    case 'catalogue':
      return { catalogue: JSON.parse(s.catalogue()), build: s.build }
    case 'stop':
      runs.get(request.run)?.stop()
      return null
    case 'run': {
      let transport: Transport | null = null
      if (request.node !== null) transport = transportFor(request.node)
      const run = s.run(request.scenario, transport ? (body) => transport!.send(body) : null, request.pollMs, (line) =>
        post({ n: request.n, line, at: Date.now() }),
      )
      runs.set(request.n, run)
      try {
        return await run.done
      } finally {
        runs.delete(request.n)
        transport?.close()
      }
    }
  }
}

scope.onmessage = (event: MessageEvent<Request>) => {
  const request = event.data
  handle(request).then(
    (value) => post({ n: request.n, ok: true, value }),
    (err) => post({ n: request.n, ok: false, error: err instanceof Error ? err.message : String(err) }),
  )
}

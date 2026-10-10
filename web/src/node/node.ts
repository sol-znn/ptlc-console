// Which node the page talks to, and the one transport every client on the
// page sends through.
//
// There are three sources, in this order:
//
//   1. A connected wallet. Syrius publishes what it signs to its own node, so
//      while it is connected the page reads that node and no other: what the
//      page shows and what the wallet signs for are then one chain by
//      construction, and there is nothing to compare.
//   2. What the visitor picked: the devnet, the chain in this page, or an
//      address they typed. Kept in this browser.
//   3. The default, a go-zenon devnet's RPC port on this machine. If that
//      does not answer and nothing was ever picked, the page falls back to
//      the chain it carries, so that it works for someone who has no node at
//      all; it says so, and a pick is never overridden this way.
//
// The transport is one object for the life of the page. Changing the node
// swaps what is behind it, and anyone who cares hears of it through onChange.

import { reactive } from 'vue'

import * as sim from '../sim/host.ts'
import type { Build } from '../sim/protocol.ts'
import { mixedContentProblem, parseNodeUrl, transportFor, type Transport } from './transport.ts'

/**
 * The WebSocket RPC of a go-zenon devnet started on this machine: the port
 * its docker/devnet publishes, and the one a wallet extension dials. A build
 * for somewhere else sets VITE_DEFAULT_NODE.
 */
export const DEFAULT_NODE: string = import.meta.env.VITE_DEFAULT_NODE || 'ws://127.0.0.1:35998'

/** The lab this console was cut from: the same contract on a devnet anyone can reach. */
export const PUBLIC_LAB_NODE = 'wss://ptlc.zenon.foo'

export type Choice = 'default' | 'sim' | 'custom'

/** How long the chain in the page waits between momentums, in milliseconds. */
export const PACES = [
  { ms: 1000, label: 'a momentum a second', note: 'ten times a devnet' },
  { ms: 250, label: 'four a second', note: 'forty times a devnet' },
  { ms: 10000, label: 'one every ten seconds', note: 'as a devnet' },
  { ms: 0, label: 'stopped', note: 'make each one by hand' },
] as const

export const node = reactive({
  /** What the visitor picked, or 'default' if they never did. */
  choice: 'default' as Choice,
  /** Whether the choice was made by a person, here or on an earlier visit. */
  picked: false,
  custom: '',
  /** The connected wallet's node, when there is one; it overrides the choice. */
  walletUrl: null as string | null,

  /** What is in use now. */
  kind: 'remote' as 'remote' | 'sim',
  url: DEFAULT_NODE,
  source: 'default' as 'wallet' | 'default' | 'custom' | 'sim' | 'fallback',
  /** Why the node in use cannot be reached from this page, when that is known before dialling. */
  problem: '',
  /** Set when the default did not answer and the page moved to its own chain. */
  fellBack: '',

  /** The chain in the page: loading, running, or why not. */
  sim: 'idle' as 'idle' | 'loading' | 'ready' | 'failed',
  simError: '',
  build: null as Build | null,
  pace: 1000,
})

const KEY = 'ptlc-console.node.v1'

function save(): void {
  try {
    localStorage.setItem(KEY, JSON.stringify({ choice: node.choice, custom: node.custom, pace: node.pace }))
  } catch {
    /* the choice just will not be remembered */
  }
}

function load(): void {
  try {
    const stored = JSON.parse(localStorage.getItem(KEY) ?? 'null') as { choice?: Choice; custom?: string; pace?: number } | null
    if (!stored) return
    if (stored.choice === 'sim' || stored.choice === 'default' || (stored.choice === 'custom' && stored.custom)) {
      node.choice = stored.choice
      node.picked = true
    }
    if (typeof stored.custom === 'string') node.custom = stored.custom
    if (typeof stored.pace === 'number' && PACES.some((p) => p.ms === stored.pace)) node.pace = stored.pace
  } catch {
    /* start from the default */
  }
}

// ---- the transport ---------------------------------------------------------------

let remote: Transport | null = null
let remoteUrl = ''

/** Every client on the page sends through this. */
export const transport: Transport = {
  send(body) {
    if (node.kind === 'sim') return sim.transport.send(body)
    if (node.problem) return Promise.reject(new Error(node.problem))
    if (!remote || remoteUrl !== node.url) {
      remote?.close()
      remote = transportFor(node.url)
      remoteUrl = node.url
    }
    return remote.send(body)
  },
  close() {},
}

const listeners = new Set<() => void>()

/** Called whenever the node in use changes: what was read from the old one is stale. */
export function onChange(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function startSim(): void {
  if (node.sim === 'ready' || node.sim === 'loading') return
  node.sim = 'loading'
  node.simError = ''
  sim.start().then(
    (build) => {
      node.build = build
      node.sim = 'ready'
      sim.pace(node.pace)
    },
    (err) => {
      node.sim = 'failed'
      node.simError = err instanceof Error ? err.message : String(err)
    },
  )
}

/** Works out the node in use from the wallet, the choice and the default. */
function resolve(): void {
  const before = `${node.kind} ${node.url}`
  node.problem = ''
  if (node.walletUrl) {
    node.kind = 'remote'
    node.url = node.walletUrl
    node.source = 'wallet'
  } else if (node.choice === 'sim') {
    node.kind = 'sim'
    node.url = ''
    node.source = node.fellBack ? 'fallback' : 'sim'
  } else if (node.choice === 'custom') {
    node.kind = 'remote'
    node.url = node.custom
    node.source = 'custom'
  } else {
    node.kind = 'remote'
    node.url = DEFAULT_NODE
    node.source = 'default'
  }
  if (node.kind === 'remote') node.problem = mixedContentProblem(node.url, window.location.protocol)
  else startSim()
  if (`${node.kind} ${node.url}` === before) return
  if (node.kind === 'sim' && remote) {
    remote.close()
    remote = null
  }
  for (const listener of listeners) listener()
}

// ---- what a person does ----------------------------------------------------------

export function useDefault(): void {
  node.choice = 'default'
  node.picked = true
  node.fellBack = ''
  save()
  resolve()
}

export function useSim(): void {
  node.choice = 'sim'
  node.picked = true
  node.fellBack = ''
  save()
  resolve()
}

/** Returns why the address cannot be used, or nothing when it is now in use. */
export function useCustom(text: string): string {
  const parsed = parseNodeUrl(text)
  if ('error' in parsed) return parsed.error
  node.choice = 'custom'
  node.custom = parsed.url
  node.picked = true
  node.fellBack = ''
  save()
  resolve()
  return ''
}

export function setPace(ms: number): void {
  node.pace = ms
  save()
  if (node.sim === 'ready') sim.pace(ms)
}

/** One momentum now, for a chain that is stopped. */
export function stepSim(): Promise<void> {
  return sim.step()
}

/** The wallet connected, moved to another node, or went away. */
export function setWalletNode(url: string | null): void {
  let clean: string | null = null
  if (url) {
    const parsed = parseNodeUrl(url)
    clean = 'url' in parsed ? parsed.url : null
  }
  if (clean === node.walletUrl) return
  node.walletUrl = clean
  resolve()
}

/**
 * The node in use did not answer. If it is the default and nobody chose it,
 * the page moves to the chain it carries; otherwise the failure stands, to be
 * read and acted on.
 */
export function unreachable(why: string): void {
  if (node.source !== 'default' || node.picked) return
  node.fellBack = why
  node.choice = 'sim'
  resolve()
}

/** What the scenario runner is told: null for the chain in the page, else the address. */
export function runnerNode(): string | null {
  return node.kind === 'sim' ? null : node.url
}

/** How the node in use is named in a sentence. */
export function describe(): string {
  if (node.kind === 'sim') return 'the chain in this page'
  if (node.source === 'wallet') return `the wallet's node, ${node.url}`
  return node.url
}

let begun = false

export function begin(): void {
  if (begun) return
  begun = true
  load()
  resolve()
  // resolve() tells listeners only of a change, and the first resolution of
  // a stored remote choice is not one against the initial values.
  if (node.kind === 'sim') startSim()
}

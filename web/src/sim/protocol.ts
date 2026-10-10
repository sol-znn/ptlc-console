// What the page and the worker that holds the chain say to each other.

export type Request =
  | { n: number; op: 'start' }
  | { n: number; op: 'rpc'; body: string }
  | { n: number; op: 'pace'; ms: number }
  | { n: number; op: 'step' }
  | { n: number; op: 'catalogue' }
  /** node is null for the chain in the worker, or the address of a node to run against. */
  | { n: number; op: 'run'; scenario: string; node: string | null; pollMs: number }
  | { n: number; op: 'stop'; run: number }

export type Answer =
  | { n: number; ok: true; value: unknown }
  | { n: number; ok: false; error: string }
  /** One line a running scenario printed; n is the run's request. */
  | { n: number; line: string; at: number }

export interface Build {
  /** The go-zenon commit the module was compiled from, and its tree. */
  commit: string
  tree: string
}

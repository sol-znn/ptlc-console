// How one JSON-RPC request gets to a node and its answer gets back. There are
// three ways and the rest of the page does not care which: HTTP, a WebSocket,
// or the chain that runs in this page (../sim/host.ts, which has the same
// shape). A request is the body as text and the answer is the body as text,
// because that is also what the scenario runner hands over from WebAssembly.
//
// Nothing here touches the document, so the same file serves the page and the
// worker.

export interface Transport {
  send(body: string): Promise<string>
  close(): void
}

/** What a person may type or a wallet may report as a node. */
export function parseNodeUrl(text: string): { url: string } | { error: string } {
  const trimmed = text.trim()
  if (!trimmed) return { error: 'give the node an address' }
  let url: URL
  try {
    url = new URL(trimmed)
  } catch {
    return { error: 'not an address: start it with ws://, wss://, http:// or https://' }
  }
  if (!['ws:', 'wss:', 'http:', 'https:'].includes(url.protocol)) {
    return { error: `${url.protocol}// is not a way to reach a node; use ws, wss, http or https` }
  }
  // "ws://host:35998/" and "ws://host:35998" are one node.
  return { url: url.href.replace(/\/$/, '') }
}

/**
 * Why a page served over https may be unable to reach this node at all, or
 * nothing. A browser lets a secure page dial an insecure address only on the
 * machine it runs on.
 */
export function mixedContentProblem(nodeUrl: string, pageProtocol: string): string {
  if (pageProtocol !== 'https:') return ''
  const url = new URL(nodeUrl)
  if (url.protocol !== 'ws:' && url.protocol !== 'http:') return ''
  if (['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)) return ''
  return `this page is served over https, and a browser will not let it dial ${url.protocol}//${url.host}: only a node on this machine may be reached without TLS. Give the node a wss:// or https:// address, or open the page from a local build.`
}

export function httpTransport(url: string): Transport {
  return {
    async send(body) {
      // A go-zenon node refuses any other content type, and this one makes
      // the request "not simple": the browser asks leave first (a CORS
      // preflight), so the node must allow this origin and each call costs
      // two round trips. A WebSocket has neither cost, which is why the
      // defaults are ws addresses.
      const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body })
      const text = await response.text()
      if (!response.ok && !text.trim().startsWith('{')) throw new Error(`the node answered HTTP ${response.status}`)
      return text
    },
    close() {},
  }
}

const OPEN_TIMEOUT_MS = 8000
const ANSWER_TIMEOUT_MS = 25000

/**
 * One socket, opened at the first request and again after it drops. Requests
 * are told apart by id, and several clients share the socket with ids of their
 * own, so each request is given an id that is unique here and gets its own
 * back on the answer.
 */
export function wsTransport(url: string): Transport {
  let socket: WebSocket | null = null
  let opening: Promise<WebSocket> | null = null
  let next = 0
  let closed = false
  const waiting = new Map<number, { resolve: (text: string) => void; reject: (err: Error) => void; id: unknown; timer: ReturnType<typeof setTimeout> }>()

  const failAll = (why: string) => {
    for (const [, w] of waiting) {
      clearTimeout(w.timer)
      w.reject(new Error(why))
    }
    waiting.clear()
  }

  function open(): Promise<WebSocket> {
    if (socket?.readyState === WebSocket.OPEN) return Promise.resolve(socket)
    if (opening) return opening
    opening = new Promise<WebSocket>((resolve, reject) => {
      let s: WebSocket
      try {
        s = new WebSocket(url)
      } catch (err) {
        opening = null
        reject(new Error(`${url} cannot be dialled from this page: ${err instanceof Error ? err.message : String(err)}`))
        return
      }
      const timer = setTimeout(() => {
        opening = null
        s.close()
        reject(new Error(`${url} did not answer within ${OPEN_TIMEOUT_MS / 1000} s`))
      }, OPEN_TIMEOUT_MS)
      s.onopen = () => {
        clearTimeout(timer)
        socket = s
        opening = null
        resolve(s)
      }
      s.onerror = () => {
        // A browser says nothing about why: refused, no route, a certificate,
        // and a mixed-content block all look the same from here.
        clearTimeout(timer)
        if (opening) {
          opening = null
          reject(new Error(`${url} cannot be reached from this page`))
        }
      }
      s.onclose = () => {
        if (socket === s) socket = null
        failAll(`the connection to ${url} closed`)
      }
      s.onmessage = (message) => {
        let answer: { id?: unknown }
        try {
          answer = JSON.parse(String(message.data))
        } catch {
          return
        }
        // Not an answer to anything asked here: a subscription notice.
        if (typeof answer.id !== 'number') return
        const w = waiting.get(answer.id)
        if (!w) return
        waiting.delete(answer.id)
        clearTimeout(w.timer)
        answer.id = w.id
        w.resolve(JSON.stringify(answer))
      }
    })
    return opening
  }

  return {
    async send(body) {
      if (closed) throw new Error('this connection was closed')
      const request = JSON.parse(body) as { id?: unknown }
      if (Array.isArray(request)) throw new Error('batched requests are not sent over this connection')
      const s = await open()
      const n = ++next
      return new Promise<string>((resolve, reject) => {
        const timer = setTimeout(() => {
          waiting.delete(n)
          reject(new Error(`${url} did not answer within ${ANSWER_TIMEOUT_MS / 1000} s`))
        }, ANSWER_TIMEOUT_MS)
        waiting.set(n, { resolve, reject, id: request.id, timer })
        try {
          s.send(JSON.stringify({ ...request, id: n }))
        } catch (err) {
          waiting.delete(n)
          clearTimeout(timer)
          reject(err instanceof Error ? err : new Error(String(err)))
        }
      })
    },
    close() {
      closed = true
      socket?.close()
      socket = null
      failAll('this connection was closed')
    },
  }
}

export function transportFor(url: string): Transport {
  return url.startsWith('ws') ? wsTransport(url) : httpTransport(url)
}

#!/usr/bin/env node
// Opens the built page in a headless Chrome and uses it the way a person
// would, with no node anywhere: the page has to find that out, move onto the
// chain it carries, and then do what it does on a devnet.
//
//   npm --prefix web run build:offline      # a build whose default node is a closed port
//   node e2e/check.mjs                      # three scenarios and the console's swap
//   node e2e/check.mjs --all                # every scenario in the catalogue
//   node e2e/check.mjs --site https://sol-znn.github.io/ptlc-console
//                                           # the published page instead of the local build
//   node e2e/check.mjs --live ws://127.0.0.1:35998
//                                           # also: one scenario against that node, and a
//                                           # stand-in wallet whose node it is
//
// The page is served from web/dist-offline under /ptlc-console/, the path a
// GitHub project page has, so a link that only works at the root of a domain
// fails here and not after a deploy.
//
// What a pass means: the WebAssembly module loaded in a worker in a real
// browser, the node menu's fallback happened, the console signed blocks in
// JavaScript that go-zenon's verifier in WebAssembly accepted, the contract
// answered, and the scenarios page drew each scenario to a pass. Whether the
// contract's answers are right is the runner's own check (sim/flows), and
// `go test ./...` in sim/ holds it to that natively.
//
// It needs Chrome (CHROME_PATH to say where).

import { spawn } from 'node:child_process'
import { createReadStream, existsSync, mkdirSync, mkdtempSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:http'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const args = process.argv.slice(2)
const flag = (name, fallback) => {
  const at = args.indexOf(`--${name}`)
  return at === -1 ? fallback : args[at + 1]
}
const all = args.includes('--all')
const live = flag('live', '')
const shots = flag('shots', '')
const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'web', flag('dist', 'dist-offline'))
if (!existsSync(path.join(root, 'index.html'))) {
  console.error(`${root} has no build. Run: npm --prefix web run build:offline`)
  process.exit(2)
}

const chrome = [
  process.env.CHROME_PATH,
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
].find((candidate) => candidate && existsSync(candidate))
if (!chrome) {
  console.error('No Chrome found. Point CHROME_PATH at one.')
  process.exit(2)
}

const stamp = () => new Date().toISOString().slice(11, 19)
const say = (text) => console.log(`${stamp()} ${text}`)
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

let failures = 0
function check(what, ok, detail = '') {
  console.log(`${stamp()}   ${ok ? 'ok  ' : 'FAIL'} ${what}${detail ? `: ${detail}` : ''}`)
  if (!ok) failures++
  return ok
}

// ---- the page, served as GitHub Pages serves it --------------------------------

const BASE = '/ptlc-console'
const TYPES = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.wasm': 'application/wasm', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.json': 'application/json' }
const server = createServer((request, response) => {
  const url = new URL(request.url, 'http://x')
  if (!url.pathname.startsWith(BASE + '/') && url.pathname !== BASE) {
    response.writeHead(404).end('outside the page')
    return
  }
  let file = path.join(root, decodeURIComponent(url.pathname.slice(BASE.length)))
  if (!file.startsWith(root)) return void response.writeHead(403).end()
  if (existsSync(file) && statSync(file).isDirectory()) {
    if (!url.pathname.endsWith('/')) return void response.writeHead(301, { Location: url.pathname + '/' }).end()
    file = path.join(file, 'index.html')
  }
  if (!existsSync(file)) return void response.writeHead(404).end('not found')
  response.writeHead(200, { 'Content-Type': TYPES[path.extname(file)] ?? 'application/octet-stream' })
  createReadStream(file).pipe(response)
})
await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
// --site checks a page that is already published instead of the local build.
const site = flag('site', '').replace(/\/$/, '') || `http://127.0.0.1:${server.address().port}${BASE}`

// ---- a browser -----------------------------------------------------------------

const profile = mkdtempSync(path.join(tmpdir(), 'ptlc-console-e2e-'))
const port = 9400 + Math.floor(Math.random() * 400)
const browser = spawn(chrome, ['--headless=new', '--disable-gpu', '--no-first-run', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, '--window-size=1440,1100', 'about:blank'], { stdio: 'ignore' })

async function connect() {
  for (let attempt = 0; attempt < 60; attempt++) {
    try {
      const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
      const tab = targets.find((t) => t.type === 'page')
      if (tab) return new WebSocket(tab.webSocketDebuggerUrl)
    } catch {
      /* not up yet */
    }
    await sleep(250)
  }
  throw new Error('Chrome did not come up')
}

let socket
let nextId = 1
const pending = new Map()
const consoleErrors = []

function send(method, params = {}) {
  const id = nextId++
  socket.send(JSON.stringify({ id, method, params }))
  return new Promise((resolve, reject) => pending.set(id, { resolve, reject }))
}

async function page(expression) {
  const out = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true, userGesture: true })
  if (out.exceptionDetails) throw new Error(out.exceptionDetails.exception?.description ?? out.exceptionDetails.text)
  return out.result.value
}

async function until(what, read, done, timeoutMs = 60_000, everyMs = 400) {
  const deadline = Date.now() + timeoutMs
  let last
  for (;;) {
    last = await read()
    if (done(last)) return last
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}; last saw ${JSON.stringify(last)}`)
    await sleep(everyMs)
  }
}

async function open(where, ready) {
  await send('Page.navigate', { url: `${site}${where}` })
  await until(`${where} to draw`, () => page(ready).catch(() => false), Boolean, 30_000)
  await page(helpers)
}

async function picture(name) {
  if (!shots) return
  // The whole page, not the window: a bench is longer than a screen.
  const shot = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true })
  mkdirSync(shots, { recursive: true })
  writeFileSync(path.join(shots, `${name}.png`), Buffer.from(shot.data, 'base64'))
}

// The page's controls, found by what they say.
const helpers = `window.__p = {
  button(text) { return Array.from(document.querySelectorAll('button')).find((b) => b.textContent.trim().startsWith(text)) },
  click(text) { const b = this.button(text); if (!b) return 'no button: ' + text; if (b.disabled) return 'disabled: ' + text; b.click(); return 'ok' },
  set(el, value) {
    const proto = el.tagName === 'SELECT' ? HTMLSelectElement.prototype : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, value)
    el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }))
  },
  field(label) {
    const l = Array.from(document.querySelectorAll('label.field')).find((x) => (x.querySelector('span') || {}).textContent.trim().startsWith(label))
    return l ? l.querySelector('input,select,textarea') : null
  },
  fill(label, value) { const el = this.field(label); if (!el) return 'no field: ' + label; this.set(el, value); return 'ok' },
  text(selector) { const el = document.querySelector(selector); return el ? el.innerText : '' },
  steps() { return Array.from(document.querySelectorAll('.main ol.steps > li')).map((li) => ['done', 'now', 'later'].find((s) => li.classList.contains(s))) },
  top() {
    const li = document.querySelector('.feed > ol > li')
    if (!li) return null
    return { label: li.querySelector('strong').textContent, stage: ['signing', 'published', 'confirmed', 'accepted', 'refused', 'declined', 'failed'].find((s) => li.classList.contains(s)), text: li.innerText.replace(/\\n+/g, ' | ') }
  },
  node() { return document.querySelector('.np-button').innerText.replace(/\\n+/g, ' / ') },
  height() { const el = document.querySelector('.clock .height'); return el ? Number(el.textContent.replace(/\\D/g, '')) : 0 },
  card(title) { return Array.from(document.querySelectorAll('article.card')).find((c) => c.querySelector('h3').textContent.trim() === title) },
  titles() { return Array.from(document.querySelectorAll('article.card h3')).map((h) => h.textContent.trim()) },
  run(title) {
    const card = this.card(title)
    if (!card) return 'no card: ' + title
    const b = card.querySelector('.card-foot .btn-primary')
    if (!b) return 'no Run button'
    if (b.disabled) return 'held: ' + card.querySelector('.eta').textContent.trim()
    b.click()
    return 'ok'
  },
  read(title) {
    const card = this.card(title)
    if (!card) return null
    return {
      state: ['running', 'pass', 'fail'].find((s) => card.classList.contains(s)) || 'none',
      badge: card.querySelector('.badge').textContent.trim(),
      verdict: ((card.querySelector('.verdict') || {}).innerText || '').replace(/\\n+/g, ' | '),
      steps: Array.from(card.querySelectorAll('.steps > li')).map((li) => ({
        state: ['pending', 'active', 'sent', 'confirmed', 'done', 'failed'].find((s) => li.classList.contains(s)),
        label: li.querySelector('.label').childNodes[0].textContent.trim(),
        detail: (li.querySelector('.detail') || {}).textContent || '',
      })),
      log: Array.from(card.querySelectorAll('.log li, .log p, pre')).map((x) => x.innerText).join(' | ').slice(-600),
    }
  },
  section(i) {
    const g = document.querySelectorAll('section.group')[i]
    const b = g && g.querySelector('header .btn')
    return g ? { title: g.querySelector('h2').textContent.trim(), score: g.querySelector('.score').textContent.trim(), button: b.textContent.trim(), disabled: b.disabled, cards: g.querySelectorAll('article.card').length, queued: Array.from(g.querySelectorAll('.eta')).filter((e) => e.textContent.trim() === 'Queued').length } : null
  },
  runSection(i) { const b = document.querySelectorAll('section.group')[i].querySelector('header .btn-run'); if (!b || b.disabled) return 'held'; b.click(); return 'ok' },
  runAll() { const b = document.querySelector('.run-all .btn'); return b ? { text: b.textContent.trim(), disabled: b.disabled, note: document.querySelector('.run-all .eta').textContent.trim() } : null },
  tally() { return Array.from(document.querySelectorAll('.tally dd')).map((d) => d.textContent.trim()).join(' / ') },
}; 'ok'`

const settled = (label, timeoutMs = 60_000) =>
  until(`"${label}" to settle`, () => page('window.__p.top()'), (a) => a && a.label.startsWith(label) && ['accepted', 'refused', 'declined', 'failed'].includes(a.stage), timeoutMs)
const press = (text, timeoutMs = 30_000) => until(`the button "${text}"`, () => page(`window.__p.click(${JSON.stringify(text)})`), (r) => r === 'ok', timeoutMs)

async function runScenario(sc, timeoutMs) {
  await until('the Run button to be free', () => page(`window.__p.run(${JSON.stringify(sc.title)})`), (r) => r === 'ok', 30_000)
  const started = Date.now()
  const end = await until(`${sc.id} to end`, () => page(`window.__p.read(${JSON.stringify(sc.title)})`), (c) => c.state === 'pass' || c.state === 'fail', timeoutMs)
  // The page shows a run's lines a beat apart; the badge turns when the last
  // one has landed, and the steps are read after it.
  const card = await page(`window.__p.read(${JSON.stringify(sc.title)})`)
  const ok = check(`${sc.id}: the page says it passed`, end.state === 'pass', `${card.verdict || card.badge}; ${Math.round((Date.now() - started) / 1000)} s here`)
  check(`${sc.id}: every step is drawn settled`, card.steps.length > 0 && card.steps.every((s) => s.state === 'done'), card.steps.map((s) => s.state).join(' '))
  if (!ok) for (const step of card.steps) console.log(`${stamp()}     [${step.state}] ${step.label}${step.detail ? `: ${step.detail}` : ''}`)
}

async function main() {
  socket = await connect()
  await new Promise((resolve, reject) => {
    socket.onopen = resolve
    socket.onerror = () => reject(new Error('could not open the debugging socket'))
  })
  socket.onmessage = (event) => {
    const message = JSON.parse(event.data)
    if (message.method === 'Runtime.exceptionThrown') consoleErrors.push(message.params.exceptionDetails.exception?.description ?? message.params.exceptionDetails.text)
    const waiting = pending.get(message.id)
    if (!waiting) return
    pending.delete(message.id)
    if (message.error) waiting.reject(new Error(message.error.message))
    else waiting.resolve(message.result)
  }
  await send('Page.enable')
  await send('Runtime.enable')

  // ---- the console, with no node ------------------------------------------------
  say(`the console at ${site}/, default node a closed port`)
  await open('/', `!!document.querySelector('.np-button')`)
  const fell = await until('the page to move onto its own chain', () => page('window.__p.node()'), (t) => /In-page chain/.test(t) && !/loading/.test(t), 60_000)
  check('nothing answered at the default, and the page moved onto the chain it carries', /In-page chain/.test(fell), fell)
  check('it says so', /No node answered at the default address/.test(await page(`window.__p.text('.main')`)))
  const h1 = await until('the chain to be read', () => page('window.__p.height()'), (h) => h > 0, 30_000)
  await sleep(3500)
  const h2 = await page('window.__p.height()')
  check('the chain in the page makes momentums at its pace', h2 - h1 >= 2 && h2 - h1 <= 5, `${h1} to ${h2} in 3.5 s at one a second`)
  const overview = await until('the status panel', () => page(`window.__p.text('.main')`), (t) => /spork active/.test(t), 20_000)
  check('the ptlc spork is active on it', /spork active/.test(overview) && /in this page/.test(overview))
  await picture('console')

  say('the swap bench: Alice and Bob as keys the page holds, signed in JavaScript')
  await page(`(location.hash = '#/swap', 'ok')`)
  await sleep(600)
  for (const [label, value] of [["Alice's entry expires", '12'], ["Bob's entry expires", '6']]) {
    const r = await page(`window.__p.fill(${JSON.stringify(label)}, ${JSON.stringify(value)})`)
    if (r !== 'ok') throw new Error(r)
  }
  await press('Make t and d', 10_000)
  await press('Lock 100 QSR as Alice')
  check("Alice's Create accepted by the contract in WebAssembly", (await settled('Swap: Alice locks')).stage === 'accepted')
  await press('Lock 10 ZNN as Bob', 60_000)
  check("Bob's Create accepted, after his checks of Alice's entry passed", (await settled('Swap: Bob locks')).stage === 'accepted')
  await press('Unlock as Alice, showing t')
  check("Alice's Unlock accepted", (await settled('Swap: Alice unlocks')).stage === 'accepted')
  await until('step 5 to open', () => page('window.__p.steps()'), (s) => s[3] === 'done' && s[4] === 'now', 30_000)
  await press('Unlock as Bob with t + d')
  check("Bob's Unlock with t + d accepted", (await settled('Swap: Bob unlocks')).stage === 'accepted')
  const done = await until('the swap to read as done', () => page(`window.__p.text('.main')`), (t) => /Swapped/.test(t), 30_000)
  check('both entries are paid and the bench says so', /Swapped/.test(done))
  await picture('swap')

  // ---- the scenarios page -------------------------------------------------------
  say('the scenarios page')
  const tapeBefore = await page(`window.__p.text('.tape')`)
  const heightBefore = Number((tapeBefore.match(/height (\d+)/) || [0, 0])[1])
  // By its link, as a person gets there: a change of fragment, not a load, so
  // the chain the swap above ran on is the chain the scenarios run on.
  await page(`(document.querySelector('.rail a.away').click(), 'ok')`)
  await until('the cards', () => page(`document.querySelectorAll('article.card').length`), (n) => n > 0, 60_000)
  const catalogue = await page(`window.__p.titles()`)
  check('it draws a card for every scenario the module has', catalogue.length >= 21, `${catalogue.length} cards`)
  await until('its own chain', () => page(`window.__p.text('.chain')`), (t) => /In-page chain, block/.test(t), 60_000)
  const wanted = all ? catalogue : ['Unlock with a secret', 'After the deadline, the owner takes it back', 'Swap bitcoin for ZNN']
  for (const title of wanted) {
    // Open the card first, so its steps are drawn and can be counted.
    await page(`(window.__p.card(${JSON.stringify(title)}).querySelector('button.title').click(), 'ok')`)
    await sleep(350)
    await runScenario({ id: title, title }, 240_000)
  }
  // A section's Run is a queue of the same single runs, so what is checked is
  // the queue: that it holds the rest, goes through them, and lets go.
  const section = await page('window.__p.section(0)')
  await until("the section's Run to be free", () => page('window.__p.runSection(0)'), (r) => r === 'ok', 30_000)
  const during = await until('the section to queue its scenarios', () => page('window.__p.section(0)'), (g) => g.button === 'Stop', 10_000)
  check(`${section.title}: its Run queues the rest of the section and turns into Stop`, during.queued === section.cards - 1, `${during.queued} queued of ${section.cards}`)
  const held = await page('window.__p.runAll()')
  check('Run all is held while a section runs', held.disabled, held.text)
  const ended = await until(`${section.title} to finish`, () => page('window.__p.section(0)'), (g) => g.button === 'Run section' && !g.disabled, section.cards * 240_000)
  check(`${section.title}: every scenario in it passed`, ended.score === `${section.cards} of ${section.cards} passed`, ended.score)
  const free = await page('window.__p.runAll()')
  check('Run all is offered again', free.text === 'Run all' && !free.disabled, `${free.text}; ${free.note}`)
  say(`tally on the page (passed / failed / not run): ${await page('window.__p.tally()')}`)
  await picture('scenarios')

  // One document, one chain: what the scenarios did is on the console's tape.
  await page(`(location.hash = '#/entries', 'ok')`)
  const tape = await until("the console to show the scenarios' blocks",() => page(`window.__p.text('.tape')`), (t) => Number((t.match(/height (\d+)/) || [0, 0])[1]) > heightBefore, 20_000)
  const heightAfter = Number(tape.match(/height (\d+)/)[1])
  check("the console's tape shows the blocks the scenarios put on the chain", heightAfter > heightBefore, `contract chain height ${heightBefore} before the scenarios, ${heightAfter} after`)
  const entries = await page(`window.__p.text('.main')`)
  check("and its Entries bench lists their entries beside the swap's",/1 ZNN/.test(entries) && /100 QSR/.test(entries), (entries.match(/\d+ (locked|paid|returned)/g) || []).join(', '))
  await picture('entries')

  // The old address of the page, when it was a document of its own. This one
  // is a load, and so a new chain.
  await open('/scenarios/', `location.hash === '#/scenarios' && document.querySelectorAll('article.card').length > 0`)
  check('the old /scenarios/ address lands on the page, inside the console document', !(await page('location.pathname')).includes('scenarios'), await page('location.href'))

  // ---- a node, if there is one ---------------------------------------------------
  if (live) {
    say(`a live node, ${live}: picked in the node menu`)
    await page(`(document.querySelector('.np-button').click(), 'ok')`)
    await sleep(300)
    await page(`(() => { const i = document.querySelector('.np-row input'); window.__p.set(i, ${JSON.stringify(live)}); return 'ok' })()`)
    await page(`window.__p.click('Use it')`)
    const on = await until('the page to read the node', () => page(`window.__p.text('.chain')`), (t) => /Chain live, block/.test(t), 30_000)
    check('the page reads the node over its WebSocket', /Chain live/.test(on), `${await page('window.__p.node()')}; ${on.replace(/\s+/g, ' ')}`)
    await page(`(document.querySelector('.np-button').click(), 'ok')`)
    const title = 'Unlock with a secret'
    await runScenario({ id: `secret on ${live}`, title }, 300_000)
  }

  // ---- a wallet's node overrides the pick -------------------------------------
  if (live) {
    // Not Syrius: a stand-in with the provider's shape, which answers as a
    // connected wallet whose node is the live one. It proves the page's rule
    // (a connected wallet's node is the node the page reads) and nothing
    // about the extension, which zenon-ptlc's lab/e2e/syrius.mjs drives for
    // real against the same adapter.
    say(`a connected wallet on ${live}, while the node menu says the chain in the page`)
    await send('Page.addScriptToEvaluateOnNewDocument', {
      source: `(() => {
        let connected = false
        const handlers = {}
        window.zenon = {
          version: 2, isSyriusExtension: true,
          connect: async () => { connected = true; return {} },
          disconnect: async () => { connected = false; (handlers.disconnect || []).forEach((h) => h()) },
          getAccounts: async () => (connected ? ['z1qq6eg8n43g032hanpsfp02qcdmv7zfj3y2lt5d'] : []),
          getChainId: async () => (connected ? 69 : null),
          getNodeUrl: async () => (connected ? ${JSON.stringify(live)} : null),
          sendAccountBlock: async () => { throw { code: 4001, message: 'stand-in' } },
          signMessage: async () => { throw { code: 4001, message: 'stand-in' } },
          on: (e, h) => { (handlers[e] = handlers[e] || []).push(h) },
          removeListener: () => {},
        }
      })()`,
    })
    await open('/', `!!document.querySelector('.np-button')`)
    await page(`(localStorage.setItem('ptlc-console.node.v1', JSON.stringify({ choice: 'sim', custom: '', pace: 1000 })), 'ok')`)
    // A change of fragment alone is not a load, and the pick is read at load.
    await send('Page.reload')
    await sleep(500)
    await until('the page again', () => page(`!!document.querySelector('.np-button')`).catch(() => false), Boolean, 30_000)
    await page(helpers)
    await page(`(location.hash = '#/wallet', 'ok')`)
    const first = await until('the picked chain', () => page('window.__p.node()'), (t) => /In-page chain/.test(t) && !/loading/.test(t), 60_000)
    check('before connecting, the page is on what was picked', /In-page chain/.test(first), first)
    await press('Connect Syrius', 15_000)
    const on = await until("the page to move to the wallet's node", () => page('window.__p.node()'), (t) => /Syrius/.test(t), 20_000)
    check("connected, the page reads the wallet's node whatever was picked", on.includes(live.replace(/^wss?:[/][/]/, '')), on)
    const h = await until("the wallet's chain to be read", () => page('window.__p.height()'), (n) => n > 100, 30_000)
    const panel = await until('the checks', () => page(`window.__p.text('.main .checks')`), (t) => /The page reads its node/.test(t) && /ptlc contract is active/.test(t), 30_000)
    check('the wallet bench says so, and that the contract is active there', /Signing for chain 69/.test(panel), `momentum ${h}`)
    const signer = await page(`(Array.from(document.querySelectorAll('.signer button')).find((b) => b.textContent.trim() === 'Syrius').className)`)
    check('Syrius is offered as a signer, with nothing left to compare', !/blocked/.test(signer), signer || '(no class)')
    await press('Disconnect', 10_000)
    const back = await until('the page to go back', () => page('window.__p.node()'), (t) => /In-page chain/.test(t), 20_000)
    check('disconnected, the page is back on what was picked', /In-page chain/.test(back), back)
  }

  check('no script error was thrown on any page', consoleErrors.length === 0, consoleErrors.slice(0, 3).join(' || '))
  console.log(`\n${failures === 0 ? 'All checks passed' : `${failures} check${failures === 1 ? '' : 's'} FAILED`}`)
}

let code = 0
try {
  await main()
  code = failures === 0 ? 0 : 1
} catch (err) {
  console.error(`${stamp()} stopped: ${err.message}`)
  await picture('stopped').catch(() => undefined)
  code = 1
} finally {
  try {
    socket?.close()
  } catch {
    /* already closed */
  }
  browser.kill()
  server.close()
  await sleep(500)
  try {
    rmSync(profile, { recursive: true, force: true })
  } catch {
    /* Chrome may still hold a file; the directory is in the system's temp */
  }
}
process.exit(code)

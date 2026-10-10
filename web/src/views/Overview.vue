<script setup lang="ts">
import { computed } from 'vue'
import MermaidDiagram from '../components/MermaidDiagram.vue'
import HexValue from '../components/HexValue.vue'
import { POINT_TYPES, PTLC_CONTRACT, PTLC_SPORK_ID } from '../ptlc/contract.ts'
import { node, describe, DEFAULT_NODE, PUBLIC_LAB_NODE } from '../node/node.ts'
import { state } from '../store.ts'

defineProps<{ param: string }>()

const lifecycle = `stateDiagram-v2
  direction LR
  [*] --> Locked: Create
  Locked --> Paid: Unlock or ProxyUnlock, before expiry
  Locked --> Returned: Reclaim, at or after expiry
  Paid --> [*]
  Returned --> [*]`

const counts = computed(() => ({
  locked: state.entries.filter((e) => e.state === 'locked').length,
  paid: state.entries.filter((e) => e.state === 'paid').length,
  returned: state.entries.filter((e) => e.state === 'returned').length,
  refused: state.events.filter((e) => !e.ok).length,
}))

const short = (sha: string | undefined) => (sha ? sha.slice(0, 7) : 'not reported')
const origin = window.location.origin
</script>

<template>
  <h1 class="bench-title">Every way into the ptlc contract, on one bench</h1>
  <p class="bench-lede">
    A ptlc entry holds funds until someone shows the secret behind a point on a curve, or a time passes and its creator takes them back. This page makes each of the contract's four calls and shows what the chain did.
  </p>

  <section class="panel">
    <h2 class="section-title">What is running</h2>
    <dl class="kv">
      <dt>Node</dt>
      <dd v-if="state.momentum">
        <span class="tag paid">answering</span> chain {{ state.momentum.chainIdentifier }}, momentum {{ state.momentum.height.toLocaleString('en') }}
      </dd>
      <dd v-else><span class="tag refused">not answering</span> {{ state.nodeError || 'reading' }}</dd>

      <dt>Read from</dt>
      <dd v-if="node.kind === 'sim'">
        <span class="tag new">in this page</span>
        go-zenon as WebAssembly; gone on reload
        
      </dd>
      <dd v-else>
        <code>{{ node.url }}</code>
        <span class="small">
          {{ node.source === 'wallet' ? 'the node Syrius is on' : node.source === 'default' ? 'the default devnet' : 'picked in the node menu' }}
        </span>
      </dd>

      <template v-if="state.peers !== null && node.kind !== 'sim'">
        <dt>Its peers</dt>
        <dd>
          <span class="tag" :class="state.peers >= 4 ? 'paid' : 'locked'">{{ state.peers }}</span>
          <span v-if="state.peers < 4" class="small">
            Few peers: answers can come several momentums late.
          </span>
        </dd>
      </template>

      <dt>Contract</dt>
      <dd>
        <span class="tag" :class="state.spork ? 'paid' : state.spork === false ? 'refused' : ''">
          {{ state.spork ? 'spork active' : state.spork === false ? 'spork not active' : 'checking' }}
        </span>
        <HexValue :value="PTLC_CONTRACT" full />
      </dd>

      <dt>Spork</dt>
      <dd><HexValue :value="PTLC_SPORK_ID" :head="16" :tail="8" /> <span class="small">the id mainnet has held since 2023, never activated there</span></dd>

      <dt>Entries so far</dt>
      <dd>
        <a href="#/entries">{{ counts.locked }} locked, {{ counts.paid }} paid, {{ counts.returned }} returned</a>; {{ counts.refused }} calls refused
      </dd>

      <template v-if="node.build">
        <dt>The chain in this page</dt>
        <dd class="small">
          built from go-zenon <span class="data">{{ short(node.build.commit) }}</span>, tree <span class="data">{{ short(node.build.tree) }}</span>: the branch that carries the contract, taken whole
        </dd>
      </template>
    </dl>
    <p v-if="state.spork === false" class="note bad" style="margin-top: 12px" role="alert">
      The ptlc spork is not active on this chain. Pick a devnet on the ptlc branch, or the chain in this page.
    </p>
    <p v-else-if="node.kind === 'remote' && state.momentum" class="note warn" style="margin-top: 12px">
      On a shared devnet everyone signs as Alice and Bob. If two people send at once the node refuses one block; send it again.
    </p>
  </section>

  <section class="panel">
    <h2 class="section-title">Where the chain comes from</h2>
    <div class="table-scroll">
      <table class="grid">
        <thead>
          <tr>
            <th>Node</th>
            <th>When</th>
            <th>What it takes</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>Your wallet's node</strong></td>
            <td>while Syrius is connected. It overrides the other three.</td>
            <td>A page served over https can dial <code>wss://</code> anywhere, and <code>ws://</code> only on this machine.</td>
          </tr>
          <tr>
            <td><strong>A devnet on this machine</strong><br /><code>{{ DEFAULT_NODE }}</code></td>
            <td>the default</td>
            <td>A go-zenon devnet on the ptlc branch, with <code>WSOrigins</code> allowing this page, as the branch's own devnet configs do.</td>
          </tr>
          <tr>
            <td><strong>The chain in this page</strong></td>
            <td>the node menu, or when the default does not answer</td>
            <td>Nothing. About 8 MB of WebAssembly, fetched the first time it is wanted.</td>
          </tr>
          <tr>
            <td><strong>Another node</strong></td>
            <td>typed into the node menu; <code>{{ PUBLIC_LAB_NODE }}</code> is the public lab's</td>
            <td>Over HTTP, a node that allows <code>{{ origin }}</code> in <code>HTTPCors</code>. Over a WebSocket, nothing.</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="small" style="margin-top: 10px; max-width: 72ch">
      Now: {{ describe() }}. Alice and Bob hold funds only on a go-zenon devnet or the chain in this page.
    </p>
  </section>

  <section class="panel">
    <h2 class="section-title">The ways in</h2>
    <div class="table-scroll">
      <table class="grid">
        <thead>
          <tr>
            <th>Client</th>
            <th>Who signs</th>
            <th>Here</th>
            <th>What it cannot do</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>JSON-RPC</strong><br /><span class="small">any HTTP client</span></td>
            <td>nobody, it only reads</td>
            <td><a href="#/rpc">Raw RPC</a>, and the tape on the right</td>
            <td><code>embedded.ptlc.getById</code> is the only read call.</td>
          </tr>
          <tr>
            <td><strong>A key the page holds</strong><br /><span class="small">Alice, Bob, Carol</span></td>
            <td>this page, in JavaScript</td>
            <td>pick one in the header, then <a href="#/lock">Lock funds</a> or <a href="#/entries">Entries</a></td>
            <td>Nothing here; the keys are public devnet ones.</td>
          </tr>
          <tr>
            <td><strong>Syrius extension</strong><br /><span class="small">v0.3.4 and later</span></td>
            <td>your wallet, after you approve each block</td>
            <td><a href="#/wallet">Syrius and accounts</a>, then pick Syrius in the header</td>
            <td>It shows the contract as unrecognized. Its own key cannot be an Ed25519 lock.</td>
          </tr>
          <tr>
            <td><strong>The scenario runner</strong><br /><span class="small">Go, compiled into the page</span></td>
            <td>the runner, with the devnet's keys, every party in one program</td>
            <td><a href="#/scenarios">Scenarios page</a>; what it does shows on the tape here</td>
            <td>A scripted run. Its Bitcoin side is a chain in the page.</td>
          </tr>
          <tr>
            <td><strong>znn-cli</strong><br /><span class="small">the Dart reference client</span></td>
            <td>the CLI, with a keystore</td>
            <td>not on this page. The lab at <a href="https://ptlc.zenon.foo" rel="noopener">ptlc.zenon.foo</a> does.</td>
            <td>Expiry is whole hours, 1 to 168, so a refund cannot be watched in one sitting.</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <section class="panel">
    <h2 class="section-title">Three kinds of lock</h2>
    <div class="table-scroll">
      <table class="grid">
        <thead>
          <tr>
            <th>pointType</th>
            <th>The lock is</th>
            <th>It opens with</th>
            <th>The chain then shows</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in POINT_TYPES" :key="p.type">
            <td><span class="data">{{ p.type }}</span> {{ p.name }}</td>
            <td>{{ p.lock }}</td>
            <td>{{ p.witness }}</td>
            <td>{{ p.shown }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="note" style="margin-top: 12px">
      A key lock keeps its secret off the chain. A point lock publishes its scalar when opened, so it must name the address it pays.
    </p>
  </section>

  <section class="panel">
    <h2 class="section-title">The life of an entry</h2>
    <MermaidDiagram :code="lifecycle" label="An entry is locked by Create, then either paid by an unlock before its expiry or returned by a reclaim at or after it." />
    <p class="small" style="margin-top: 8px; max-width: 68ch">
      Expiry is the frontier momentum's timestamp. A refused call moves nothing; a refused Create sends the funds back.
    </p>
  </section>
</template>

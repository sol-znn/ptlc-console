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
    A ptlc entry holds funds until someone shows the secret behind a point on a curve, or until a time passes and its creator takes them back. This page makes each of the contract's four calls, signed by a key it holds or by your wallet, against a node you pick or against a copy of the chain that runs in this tab, and shows what the chain did.
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
        go-zenon's chain, virtual machine and contracts compiled to WebAssembly, started from the devnet's genesis.
        <span class="small">No node and no network; reloading the tab starts it again from genesis.</span>
      </dd>
      <dd v-else>
        <code>{{ node.url }}</code>
        <span class="small">
          {{ node.source === 'wallet' ? 'the node Syrius is on, which the page follows while it is connected' : node.source === 'default' ? 'the default: a go-zenon devnet on this machine' : 'picked in the node menu' }}
        </span>
      </dd>

      <template v-if="state.peers !== null && node.kind !== 'sim'">
        <dt>Its peers</dt>
        <dd>
          <span class="tag" :class="state.peers >= 4 ? 'paid' : 'locked'">{{ state.peers }}</span>
          <span v-if="state.peers < 4" class="small">
            The node a block is published to passes it only to its own peers. A pillar it does not reach learns of the block from somebody else's momentum, so calls wait for the right pillar's turn and answers come several momentums late.
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
      The ptlc spork is not active on this chain, so the contract's address answers nothing: a Create sent to it would be a plain transfer to an address no one holds. Pick a devnet that runs the ptlc branch, or the chain in this page.
    </p>
    <p v-else-if="node.kind === 'remote' && state.momentum" class="note warn" style="margin-top: 12px">
      On a shared devnet you are not alone. Alice and Bob are the devnet's published keys, so everyone signs as the same accounts. An account chain takes one block at a time: when two people send from Alice in the same moment the node refuses one of the blocks, and that call has to be made again. Entries on the tape that you did not make are somebody else's.
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
            <td>The page dials the address Syrius reports. A page served over https can dial <code>wss://</code> anywhere, and <code>ws://</code> only on this machine.</td>
          </tr>
          <tr>
            <td><strong>A devnet on this machine</strong><br /><code>{{ DEFAULT_NODE }}</code></td>
            <td>the default</td>
            <td>A go-zenon devnet on the ptlc branch, with <code>WSOrigins</code> allowing this page, as the branch's own devnet configs do.</td>
          </tr>
          <tr>
            <td><strong>The chain in this page</strong></td>
            <td>picked in the node menu, or by itself when the default does not answer and nothing was ever picked</td>
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
      Now: {{ describe() }}. The keys named Alice and Bob hold funds only on a devnet started from go-zenon's devnet genesis, which the chain in this page is. Anywhere else, connect a wallet that does.
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
            <td><code>embedded.ptlc.getById</code> is the only read call. Who opened an entry, and with what, has to be read off the contract's chain.</td>
          </tr>
          <tr>
            <td><strong>A key the page holds</strong><br /><span class="small">Alice, Bob, Carol</span></td>
            <td>this page, in JavaScript</td>
            <td>pick one in the header, then <a href="#/lock">Lock funds</a> or <a href="#/entries">Entries</a></td>
            <td>Nothing here; the keys are the devnet's public ones, and worth nothing elsewhere.</td>
          </tr>
          <tr>
            <td><strong>Syrius extension</strong><br /><span class="small">v0.3.4 and later</span></td>
            <td>your wallet, after you approve each block</td>
            <td><a href="#/wallet">Syrius and accounts</a>, then pick Syrius in the header</td>
            <td>It shows the ptlc contract as unrecognized, with a warning. It will not sign a 32-byte message, so its own key cannot be an Ed25519 lock.</td>
          </tr>
          <tr>
            <td><strong>The scenario runner</strong><br /><span class="small">Go, compiled into the page</span></td>
            <td>the runner, with the devnet's keys, every party in one program</td>
            <td><a href="#/scenarios">Scenarios page</a>; what it does shows on the tape here</td>
            <td>It is a scripted run, not a client: no step waits for a person. Its Bitcoin side is a chain in the page, never a bitcoind.</td>
          </tr>
          <tr>
            <td><strong>znn-cli</strong><br /><span class="small">the Dart reference client</span></td>
            <td>the CLI, with a keystore</td>
            <td>not on this page: a static page cannot run a program. The lab at <a href="https://ptlc.zenon.foo" rel="noopener">ptlc.zenon.foo</a> does.</td>
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
      A key lock keeps its secret off the chain: a swap moves it inside an adaptor signature. A point lock publishes its scalar when it is opened, which is what lets the other party read it with no signing session, and why such an entry must name the one address it pays.
    </p>
  </section>

  <section class="panel">
    <h2 class="section-title">The life of an entry</h2>
    <MermaidDiagram :code="lifecycle" label="An entry is locked by Create, then either paid by an unlock before its expiry or returned by a reclaim at or after it." />
    <p class="small" style="margin-top: 8px; max-width: 68ch">
      Expiry is judged by the timestamp of the frontier momentum, not by anyone's clock. A refused call changes nothing and moves nothing; a refused Create sends the funds straight back.
    </p>
  </section>
</template>

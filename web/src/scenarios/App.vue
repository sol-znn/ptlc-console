<script setup lang="ts">
// The scenarios page: what the ptlc contract can do, one card each, and a
// button on every card that runs it for real. The button names a scenario;
// the runner, a Go program compiled into the page, signs every step with the
// devnet's keys and reports what the chain did. The chain is the node picked
// in the bar, or go-zenon's own chain running in this tab.
//
// It is the plain half. The console beside it shows every byte and lets a
// person make each call by hand; this page shows what happened and whether
// it was what should happen.
import { computed } from 'vue'
import Icon from '../components/Icon.vue'
import LabMark from '../components/LabMark.vue'
import NodePicker from '../components/NodePicker.vue'
import ThemeToggle from '../components/ThemeToggle.vue'
import FlowStage from './FlowStage.vue'
import { about, took, type Scenario } from './flow.ts'
import { node } from '../node/node.ts'
import { run, runBatch, state, stop, stopBatch, tally } from './store.ts'

/** Seconds of the chain's clock per second of this machine's, for the countdown of a wait. */
const rate = computed(() => (node.kind === 'sim' ? (node.pace > 0 ? 10000 / node.pace : 0) : 1))
/** How long a scenario takes here: its devnet figure, scaled by how fast this chain runs. */
const eta = (s: Scenario): string => {
  if (node.kind !== 'sim') return about(s.seconds)
  if (node.pace === 0) return 'as fast as you step the chain'
  const seconds = Math.max(5, Math.round((s.seconds * node.pace) / 10000 + s.steps.length * 0.5))
  return seconds < 90 ? `about ${seconds} s` : about(seconds)
}

const groups = computed(() =>
  (state.catalogue?.groups ?? []).map((group) => {
    const scenarios = (state.catalogue?.scenarios ?? []).filter((s) => s.group === group.id)
    return {
      ...group,
      scenarios,
      ids: scenarios.map((s) => s.id),
      passed: scenarios.filter((s) => state.runs[s.id]?.status === 'pass').length,
      failed: scenarios.filter((s) => state.runs[s.id]?.status === 'fail').length,
    }
  }),
)
const counts = computed(tally)
const total = computed(() => state.catalogue?.scenarios.length ?? 0)
/** What Run all runs: the catalogue, in the order the page draws it. */
const all = computed(() => groups.value.flatMap((g) => g.ids))
/** Whether a batch could start now: no run holds the accounts, and the chain could take one. */
const free = computed(() => !state.starting && !state.busy && !state.nodeDown && !!state.momentum && state.funded !== false)
const allNote = computed(() => {
  if (state.batch === 'all') return state.queue.length ? `${state.queue.length} still to start` : 'On the last one'
  return `${all.value.length} scenarios, one after another`
})

const status = (s: Scenario): 'none' | 'running' | 'pass' | 'fail' => state.runs[s.id]?.status ?? 'none'
const badge = (s: Scenario): string => {
  const r = state.runs[s.id]
  if (!r) return 'Not run yet'
  if (r.status === 'running') return 'Running'
  return `${r.status === 'pass' ? 'Passed' : 'Failed'} in ${took(r)}`
}

/** Why a card's button cannot be pressed now, or nothing. */
function held(s: Scenario): string {
  if (state.runs[s.id]?.status === 'running') return ''
  if (state.queue.includes(s.id)) return 'Queued'
  if (state.starting) return 'Starting…'
  if (state.busy) return `Waiting for “${state.busy.title}”`
  if (state.funded === false) return 'The test keys hold nothing on this chain'
  return ''
}
const toggle = (s: Scenario): void => {
  state.open = state.open === s.id ? '' : s.id
}
</script>

<template>
  <header class="head top">
    <a class="wordmark" href="#/scenarios">
      <LabMark />
      <div>
        <strong>PTLC scenarios</strong>
        <span>every case, run for you on a test chain</span>
      </div>
    </a>

    <div class="chain" :class="{ down: state.nodeDown }">
      <span class="beat" :key="state.momentum?.height ?? 0"></span>
      <template v-if="state.nodeDown">Chain not answering</template>
      <template v-else-if="state.momentum">
        {{ node.kind === 'sim' ? 'In-page chain, block' : 'Chain live, block' }}
        <Transition name="roll" mode="out-in">
          <b :key="state.momentum.height">{{ state.momentum.height.toLocaleString('en') }}</b>
        </Transition>
      </template>
      <template v-else>Reading the chain</template>
    </div>

    <dl class="tally" aria-label="Results so far">
      <div class="pass">
        <dt>Passed</dt>
        <dd><Transition name="roll" mode="out-in"><span :key="counts.pass">{{ counts.pass }}</span></Transition></dd>
      </div>
      <div class="fail" :class="{ zero: counts.fail === 0 }">
        <dt>Failed</dt>
        <dd><Transition name="roll" mode="out-in"><span :key="counts.fail">{{ counts.fail }}</span></Transition></dd>
      </div>
      <div class="none">
        <dt>Not run</dt>
        <dd><Transition name="roll" mode="out-in"><span :key="counts.none">{{ counts.none + counts.running }}</span></Transition></dd>
      </div>
    </dl>

    <NodePicker :down="state.nodeDown ? state.nodeError : ''" no-wallet />
    <a class="other" href="#/">Console<Icon name="arrow" :size="14" /></a>
    <ThemeToggle />
  </header>

  <main class="page">
    <section class="intro">
      <h1>What a PTLC can do, shown working</h1>
      <p>
        A PTLC holds funds until someone proves they have the right secret or key, or until a deadline passes and the owner takes them back. Press <b>Run</b> on any card. The page signs each step with test keys, the chain answers, and the card shows what happened. There is nothing to install and no wallet to connect.
        <template v-if="node.kind === 'sim'"> The chain is running in this tab: it is go-zenon's own code, the same contract a node runs, with no network behind it.</template>
      </p>
      <p class="legend">
        <span><i class="key ok"></i>the step went as it should</span>
        <span><i class="key line-ok"></i>the contract accepted</span>
        <span><i class="key line-no"></i>the contract refused, which some steps are there to show</span>
      </p>
      <div class="run-all">
        <button v-if="state.batch === 'all'" class="btn btn-stop" type="button" @click="stopBatch()"><Icon name="square" :size="11" />Stop</button>
        <button v-else class="btn btn-primary btn-run" type="button" :disabled="!free || all.length === 0" @click="runBatch('all', all)"><Icon name="play" :size="11" />Run all</button>
        <span class="eta">{{ allNote }}</span>
      </div>
    </section>

    <p v-if="state.error" class="alert" role="alert">The scenario runner did not load: {{ state.error }}</p>
    <p v-else-if="!state.catalogue" class="loading">Loading the scenarios</p>
    <p v-if="node.kind === 'sim' && node.fellBack" class="alert note-sim">
      No node answered at the default address, so the scenarios run on the chain this page carries. The node menu in the bar picks another.
    </p>
    <p v-if="node.kind === 'sim' && node.sim === 'failed'" class="alert" role="alert">The chain in this page did not load: {{ node.simError }}</p>
    <p v-if="state.nodeDown && node.kind === 'remote'" class="alert" role="alert">
      {{ node.url }} is not answering ({{ state.nodeError }}). Pick the chain in this page from the node menu to run without a node.
    </p>
    <p v-if="state.funded === false" class="alert" role="alert">
      The scenarios sign with the published keys of a go-zenon devnet, and on this chain those keys hold nothing, so no scenario could take its first step. Pick a devnet, or the chain in this page, from the node menu.
    </p>

    <section v-for="group in groups" :key="group.id" class="group" :class="group.id">
      <header>
        <h2>{{ group.title }}</h2>
        <span class="ledger score" :class="{ all: group.passed === group.scenarios.length, bad: group.failed > 0 }">
          {{ group.passed }} of {{ group.scenarios.length }} passed<template v-if="group.failed">, {{ group.failed }} failed</template>
        </span>
        <button v-if="state.batch === group.id" class="btn btn-quiet btn-stop" type="button" @click="stopBatch()"><Icon name="square" :size="11" />Stop</button>
        <button v-else class="btn btn-quiet btn-run" type="button" :disabled="!free || group.ids.length === 0" :aria-label="`Run every scenario in ${group.title}`" @click="runBatch(group.id, group.ids)"><Icon name="play" :size="11" />Run section</button>
        <p>{{ group.lede }}</p>
      </header>

      <div class="cards">
        <article v-for="s in group.scenarios" :key="s.id" class="card" :class="[status(s), { open: state.open === s.id }]">
          <div class="card-top">
            <button class="title" type="button" :aria-expanded="state.open === s.id" @click="toggle(s)">
              <h3>{{ s.title }}</h3>
              <svg class="chev" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"><path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" /></svg>
            </button>
            <p class="shows">{{ s.shows }}</p>
          </div>

          <div class="card-foot">
            <button v-if="status(s) === 'running'" class="btn btn-quiet btn-stop" type="button" @click="stop(state.runs[s.id].id)"><Icon name="square" :size="11" />Stop</button>
            <button v-else class="btn btn-primary btn-run" type="button" :disabled="!!held(s) || state.nodeDown || !state.momentum" @click="run(s.id)">
              <Icon name="play" :size="11" />{{ status(s) === 'none' ? 'Run' : 'Run again' }}
            </button>
            <span class="eta">{{ held(s) || `${s.steps.length} steps, ${eta(s)}` }}</span>
            <Transition name="pop" mode="out-in">
              <span class="badge" :class="status(s)" :key="badge(s)">
                <i></i>{{ badge(s) }}
              </span>
            </Transition>
          </div>
          <p v-if="state.startError[s.id]" class="alert small">{{ state.startError[s.id] }}</p>

          <div class="unfold">
            <div class="unfold-inner">
              <FlowStage v-if="state.open === s.id" :scenario="s" :run="state.runs[s.id] ?? null" :rate="rate" />
            </div>
          </div>
        </article>
      </div>

      <aside v-if="group.id === 'bitcoin'" class="zbtc">
        <div class="zbtc-head">
          <h3>Where this leads: zBTC</h3>
          <span class="tag featured">proof of concept</span>
        </div>
        <p>
          zBTC is a prototype for holding bitcoin on Zenon. In its design, bitcoin goes into a reserve that only the network's pillars can spend, and the same amount of zBTC is issued here. Every Zenon node checks Bitcoin for itself with a small built-in Bitcoin client, so nobody has to trust a bridge operator.
        </p>
        <ol class="zbtc-flow" aria-label="How zBTC is meant to work">
          <li><b>Bitcoin</b><span>a deposit, in an ordinary Taproot output</span></li>
          <li><b>Reserve</b><span>keys split between pillars; no single holder</span></li>
          <li><b>zBTC on Zenon</b><span>issued against the deposit, checked by every node</span></li>
        </ol>
        <p>
          The first two cards above are its first building block: bitcoin traded for ZNN through this lock, with no custodian in between. The third is the check a client makes before it locks anything. In zBTC's own test lab that swap passed 35 of 35 repeated runs, including refunds, crashes mid-swap, Bitcoin reorganisations and a signer going missing.
        </p>
        <dl class="zbtc-status">
          <div><dt>Built and tested</dt><dd>the bitcoin-for-ZNN swap; the built-in Bitcoin client, replayed against 4,044 real Bitcoin block headers; three relays that feed it headers and proofs</dd></div>
          <div><dt>In progress</dt><dd>the contract that issues zBTC; the ceremony in which pillars create the reserve key together</dd></div>
          <div><dt>Here</dt><dd>zBTC's own lab is not part of this page. What runs here is its swap, on the PTLC contract. The prototype is the <code>zenon-zbtc</code> repository, status as of 5 October 2026.</dd></div>
        </dl>
      </aside>
    </section>

    <footer class="foot">
      <p>
        {{ total }} scenarios. Results are kept until this tab is reloaded. One scenario runs at a time, because they share two test accounts. The keys are a devnet's published ones; the funds are worth nothing. The Bitcoin in the last group is a chain kept in this page, with real transactions checked by a real script engine and no blocks behind them.
      </p>
      <p>Want to make each call by hand, use a wallet, or see every block these runs put on the chain? <a href="#/entries">Open the console</a>: it reads the same chain.</p>
    </footer>
  </main>
</template>

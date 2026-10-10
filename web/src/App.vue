<script setup lang="ts">
// One document, routed by its fragment. That is a deliberate fit with the
// wallet extension: a Syrius approval belongs to the document that asked for
// it and is cancelled by a navigation, and moving between benches here is not
// one, so an approval window can stay open while the entry it is about is
// looked up on another bench.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import ActivityFeed from './components/ActivityFeed.vue'
import ChainTape from './components/ChainTape.vue'
import Icon from './components/Icon.vue'
import LabMark from './components/LabMark.vue'
import MomentumClock from './components/MomentumClock.vue'
import NodePicker from './components/NodePicker.vue'
import SignerPicker from './components/SignerPicker.vue'
import ThemeToggle from './components/ThemeToggle.vue'
import { node } from './node/node.ts'
import { state } from './store.ts'

import Overview from './views/Overview.vue'
import Secrets from './views/Secrets.vue'
import LockFunds from './views/LockFunds.vue'
import Entries from './views/Entries.vue'
import Swap from './views/Swap.vue'
import RawRpc from './views/RawRpc.vue'
import Wallet from './views/Wallet.vue'

const benches = [
  { path: '', name: 'Start here', sub: 'what is running', view: Overview },
  { path: 'secrets', name: 'Secrets and points', sub: 'keys, scalars, tweaks, adaptors', view: Secrets },
  { path: 'lock', name: 'Lock funds', sub: 'Create', view: LockFunds },
  { path: 'entries', name: 'Entries', sub: 'unlock, proxy unlock, reclaim', view: Entries },
  { path: 'swap', name: 'Swap, step by step', sub: 'two tweaked point locks', view: Swap },
  { path: 'rpc', name: 'Raw RPC', sub: 'call the node', view: RawRpc },
  { path: 'wallet', name: 'Syrius and accounts', sub: 'wallet and balances', view: Wallet },
] as const

const read = () => window.location.hash.replace(/^#\/?/, '').split('/')
const route = ref(read())
const onHash = () => {
  route.value = read()
  window.scrollTo({ top: 0 })
}
onMounted(() => window.addEventListener('hashchange', onHash))
onBeforeUnmount(() => window.removeEventListener('hashchange', onHash))

const bench = computed(() => benches.find((b) => b.path === (route.value[0] ?? '')) ?? benches[0])
const param = computed(() => route.value[1] ?? '')
</script>

<template>
  <div class="frame">
    <header class="head">
      <a class="wordmark" href="#/">
        <LabMark />
        <div>
          <strong>PTLC console</strong>
          <span>point time-locked contracts on Zenon</span>
        </div>
      </a>
      <MomentumClock />
      <NodePicker :down="state.nodeError" />
      <SignerPicker />
      <ThemeToggle />
    </header>

    <nav class="rail" aria-label="Benches">
      <a v-for="b in benches" :key="b.path" :href="`#/${b.path}`" :aria-current="b === bench ? 'page' : undefined">
        {{ b.name }}
        <small>{{ b.sub }}</small>
      </a>
      <!-- The other half: the same contract and the same chain, with the page
           signing every step and one button per scenario. It takes over the
           whole window, unlike the benches above, so it is set apart. -->
      <a class="away" href="#/scenarios">
        Scenarios page<Icon name="arrow" :size="14" />
        <small>every case, one button</small>
      </a>
    </nav>


    <main class="main">
      <p v-if="node.kind === 'sim' && node.fellBack" class="note warn" style="margin-bottom: 18px">
        No node answered at the default address, so this runs on the chain in this page. The node menu picks another.
      </p>
      <p v-else-if="node.kind === 'sim' && node.sim === 'failed'" class="note bad" style="margin-bottom: 18px" role="alert">
        The chain in this page did not load: {{ node.simError }}
      </p>
      <component :is="bench.view" :param="param" />
    </main>

    <aside class="tape">
      <ActivityFeed />
      <ChainTape />
    </aside>
  </div>
</template>

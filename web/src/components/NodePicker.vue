<script setup lang="ts">
// Which node the page reads, and the control that changes it. Both pages
// carry it. The rule it shows is ../node/node.ts's: a connected wallet's node
// first, then what was picked here, then the default.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { DEFAULT_NODE, PACES, PUBLIC_LAB_NODE, node, setPace, stepSim, useCustom, useDefault, useSim } from '../node/node.ts'

const props = defineProps<{
  /** Why the node in use is not answering, when it is not. */
  down?: string
  /** The scenarios page signs with the devnet's keys and has no wallet to follow. */
  noWallet?: boolean
}>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const typed = ref(node.custom)
const typedError = ref('')
const stepping = ref(false)

const byWallet = computed(() => node.source === 'wallet')
const short = (url: string) => url.replace(/^(wss?|https?):\/\//, '')
const label = computed(() => {
  if (node.kind === 'sim') return 'In-page chain'
  if (byWallet.value) return `Syrius's node`
  return short(node.url)
})
const sub = computed(() => {
  if (node.kind === 'sim') {
    if (node.sim === 'loading') return 'loading the chain'
    if (node.sim === 'failed') return 'the chain did not load'
    return PACES.find((p) => p.ms === node.pace)?.label ?? ''
  }
  if (props.down || node.problem) return 'not answering'
  if (byWallet.value) return short(node.url)
  return node.source === 'default' ? 'devnet on this machine' : 'picked here'
})
const lamp = computed(() => {
  if (node.kind === 'sim') return node.sim === 'ready' ? 'sim' : node.sim === 'failed' ? 'down' : 'wait'
  return props.down || node.problem ? 'down' : 'live'
})

function pickCustom(): void {
  typedError.value = useCustom(typed.value)
}
function pickLab(): void {
  typed.value = PUBLIC_LAB_NODE
  pickCustom()
}
async function step(): Promise<void> {
  stepping.value = true
  try {
    await stepSim()
  } finally {
    stepping.value = false
  }
}

const outside = (event: MouseEvent) => {
  if (open.value && root.value && !root.value.contains(event.target as Node)) open.value = false
}
const escape = (event: KeyboardEvent) => {
  if (event.key === 'Escape') open.value = false
}
onMounted(() => {
  document.addEventListener('mousedown', outside)
  document.addEventListener('keydown', escape)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', outside)
  document.removeEventListener('keydown', escape)
})
</script>

<template>
  <div ref="root" class="nodepick">
    <button type="button" class="np-button" :aria-expanded="open" aria-haspopup="dialog" @click="open = !open">
      <span class="np-lamp" :class="lamp" aria-hidden="true"></span>
      <span class="np-text">
        <strong>{{ label }}</strong>
        <small>{{ sub }}</small>
      </span>
      <svg class="np-chev" viewBox="0 0 24 24" width="14" height="14" aria-hidden="true"><path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" /></svg>
    </button>

    <div v-if="open" class="np-panel" role="dialog" aria-label="The node this page reads">
      <p v-if="byWallet" class="np-note wallet">
        <strong>Syrius is connected, so the page reads its node:</strong> <code>{{ node.url }}</code>. A wallet publishes what it signs to its own node, and a page reading another would never see it. Change the node in Syrius; the pick below comes back when it disconnects.
      </p>
      <p v-if="node.problem" class="np-note bad" role="alert">{{ node.problem }}</p>
      <p v-else-if="down && node.kind === 'remote'" class="np-note bad" role="alert">
        {{ short(node.url) }} is not answering: {{ down }}
      </p>
      <p v-if="node.fellBack && node.kind === 'sim'" class="np-note">
        Nothing answered at <code>{{ DEFAULT_NODE }}</code>, so the page is on the chain it carries. Start a devnet and pick it below, or pick another node.
      </p>

      <fieldset :disabled="byWallet">
        <legend>Read the chain from</legend>

        <label class="np-option" :class="{ on: node.choice === 'default' && !node.fellBack }">
          <input type="radio" name="np-node" :checked="node.choice === 'default'" @change="useDefault" />
          <span>
            <strong>A devnet on this machine</strong>
            <code>{{ DEFAULT_NODE }}</code>
            <small>The port a go-zenon devnet publishes its WebSocket RPC on, and the default.</small>
          </span>
        </label>

        <label class="np-option" :class="{ on: node.choice === 'sim' }">
          <input type="radio" name="np-node" :checked="node.choice === 'sim'" @change="useSim" />
          <span>
            <strong>The chain in this page</strong>
            <small>
              go-zenon's own chain and contracts, compiled to WebAssembly and started from the devnet's genesis in this tab. No node, no network. It is gone when the tab is reloaded.
            </small>
          </span>
        </label>
        <div v-if="node.choice === 'sim'" class="np-sim">
          <p v-if="node.sim === 'loading'" class="np-load">Fetching and starting the chain, about 8 MB the first time</p>
          <p v-else-if="node.sim === 'failed'" class="np-note bad" role="alert">{{ node.simError }}</p>
          <template v-else>
            <label>
              <span>Pace</span>
              <select :value="node.pace" @change="setPace(Number(($event.target as HTMLSelectElement).value))">
                <option v-for="p in PACES" :key="p.ms" :value="p.ms">{{ p.label }} ({{ p.note }})</option>
              </select>
            </label>
            <button type="button" class="btn btn-quiet" :disabled="stepping" @click="step">One momentum now</button>
            <small>
              Each momentum moves the chain's clock ten seconds, whatever the pace, so a deadline four minutes out is twenty-four momentums away.
              <template v-if="node.build"> Built from go-zenon <code>{{ node.build.commit.slice(0, 7) }}</code>.</template>
            </small>
          </template>
        </div>

        <label class="np-option" :class="{ on: node.choice === 'custom' }">
          <input type="radio" name="np-node" :checked="node.choice === 'custom'" @change="pickCustom" />
          <span>
            <strong>Another node</strong>
            <span class="np-row">
              <input type="text" v-model="typed" spellcheck="false" placeholder="wss://node.example or http://host:35997" aria-label="The node's address" @keydown.enter="pickCustom" />
              <button type="button" class="btn btn-quiet" @click="pickCustom">Use it</button>
            </span>
            <small>
              A WebSocket address needs nothing of the node. An HTTP one works only if the node allows this page's origin.
              <button type="button" class="np-link" @click="pickLab">Use the public lab's devnet, {{ short(PUBLIC_LAB_NODE) }}</button>
            </small>
          </span>
        </label>
        <p v-if="typedError" class="np-note bad" role="alert">{{ typedError }}</p>
      </fieldset>

      <p v-if="!noWallet && !byWallet" class="np-foot">
        Connecting Syrius on <a href="#/wallet" @click="open = false">Syrius and accounts</a> overrides this: the page then reads the wallet's node.
      </p>
      <p v-if="noWallet" class="np-foot">
        The scenarios sign with the devnet's published keys, so they need a chain where those hold funds: a go-zenon devnet, or the chain in this page. The console reads the same node, so every block a scenario sends is on its tape.
      </p>
    </div>
  </div>
</template>

<style scoped>
.nodepick {
  position: relative;
  flex: none;
}
.np-button {
  display: flex;
  align-items: center;
  gap: 9px;
  font: inherit;
  color: var(--fg);
  background: var(--sunk);
  border: 1px solid var(--line-strong);
  border-radius: var(--radius);
  padding: 5px 10px 5px 11px;
  cursor: pointer;
  max-width: 250px;
  transition: border-color 150ms var(--ease);
}
.np-button:hover,
.np-button[aria-expanded='true'] {
  border-color: var(--dim);
}
.np-text {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  min-width: 0;
  line-height: 1.2;
}
.np-text strong {
  font-size: 0.82rem;
  font-weight: 600;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.np-text small {
  font-size: 0.7rem;
  color: var(--dim);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.np-chev {
  color: var(--dim);
  flex: none;
  transition: transform 180ms var(--ease);
}
.np-button[aria-expanded='true'] .np-chev {
  transform: rotate(180deg);
}
.np-lamp {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
  background: var(--live);
}
/* The chain in the page is not a node anyone else can see: plasma, the
   colour the console keeps for what is the page's own doing. */
.np-lamp.sim {
  background: var(--plasma);
}
.np-lamp.down {
  background: var(--danger);
  border-radius: 1px;
}
.np-lamp.wait {
  background: var(--warn);
  animation: np-wait 900ms ease-in-out infinite alternate;
}
@keyframes np-wait {
  to {
    opacity: 0.25;
  }
}

.np-panel {
  position: absolute;
  right: 0;
  top: calc(100% + 8px);
  z-index: 40;
  width: min(430px, calc(100vw - 24px));
  background: var(--raised);
  border: 1px solid var(--line-strong);
  border-radius: var(--radius);
  box-shadow: 0 18px 44px rgb(0 0 0 / 0.38);
  padding: 14px;
  animation: np-in 160ms var(--ease);
}
@keyframes np-in {
  from {
    opacity: 0;
    transform: translateY(-5px);
  }
}
fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}
fieldset:disabled {
  opacity: 0.5;
}
legend {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--dim);
  padding: 0;
  margin-bottom: 6px;
}
.np-option {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 10px;
  padding: 9px 10px;
  border: 1px solid transparent;
  border-radius: var(--radius);
  cursor: pointer;
}
.np-option:hover {
  background: var(--sunk);
}
.np-option.on {
  border-color: var(--line-strong);
  background: var(--sunk);
}
.np-option input[type='radio'] {
  margin-top: 3px;
  accent-color: var(--znn);
}
.np-option > span {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}
.np-option strong {
  font-size: 0.88rem;
  font-weight: 600;
}
.np-option small,
.np-sim small {
  font-size: 0.76rem;
  color: var(--dim);
  line-height: 1.45;
}
code {
  font-family: var(--font-mono);
  font-size: 0.76rem;
  color: var(--soft);
  overflow-wrap: anywhere;
}
.np-row {
  display: flex;
  gap: 6px;
}
.np-row input {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: 0.78rem;
  color: var(--fg);
  background: var(--bg);
  border: 1px solid var(--line-strong);
  border-radius: var(--radius);
  padding: 5px 8px;
}
.np-link {
  display: block;
  font: inherit;
  color: var(--znn);
  background: none;
  border: 0;
  padding: 0;
  margin-top: 2px;
  cursor: pointer;
  text-align: left;
}
.np-link:hover {
  text-decoration: underline;
}
.np-sim {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 0 0 4px 34px;
  padding: 2px 10px 8px 0;
}
.np-sim label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.8rem;
  color: var(--dim);
}
.np-sim select {
  flex: 1;
  min-width: 0;
  font: inherit;
  font-size: 0.8rem;
  color: var(--fg);
  background: var(--bg);
  border: 1px solid var(--line-strong);
  border-radius: var(--radius);
  padding: 4px 6px;
}
.np-sim .btn {
  align-self: flex-start;
}
.np-load {
  margin: 0;
  font-size: 0.8rem;
  color: var(--warn);
}
.np-note {
  margin: 0 0 10px;
  padding: 8px 10px;
  font-size: 0.8rem;
  line-height: 1.45;
  color: var(--soft);
  background: var(--sunk);
  border-left: 2px solid var(--warn);
  border-radius: 0 var(--radius) var(--radius) 0;
  overflow-wrap: anywhere;
}
.np-note.wallet {
  border-left-color: var(--znn);
}
.np-note.bad {
  border-left-color: var(--danger);
}
.np-foot {
  margin: 10px 0 0;
  padding-top: 10px;
  border-top: 1px solid var(--line);
  font-size: 0.76rem;
  color: var(--dim);
  line-height: 1.45;
}
.np-foot a {
  color: var(--znn);
}
@media (max-width: 760px) {
  .nodepick {
    order: 2;
  }
  .np-panel {
    left: 0;
    right: auto;
  }
}
</style>

<script setup lang="ts">
// One document for both halves: the console, and the scenarios page at
// #/scenarios. They were two documents, as in the lab, and that was wrong
// here. The chain in the page lives in this document's worker, so two
// documents were two chains: a scenario ran on one, and the console's tape
// and its Entries bench, reading the other, never saw it. In one document
// there is one worker, one chain, one node pick and one wallet connection.
//
// The scenarios page keeps its own stylesheet, written for a document of its
// own (it styles body, and names like .steps and .card that the console also
// uses). It is put in the document as a sheet of its own and switched off
// while the console is showing, which is exactly the isolation two documents
// gave it.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import App from './App.vue'
import Scenarios from './scenarios/App.vue'
import scenariosCss from './scenarios/scenarios.css?inline'
import { start as startScenarios } from './scenarios/store.ts'

const read = () => window.location.hash.replace(/^#\/?/, '').split('/')[0] === 'scenarios'
const onScenarios = ref(read())
const onHash = () => (onScenarios.value = read())
onMounted(() => window.addEventListener('hashchange', onHash))
onBeforeUnmount(() => window.removeEventListener('hashchange', onHash))

const sheet = document.createElement('style')
sheet.dataset.for = 'scenarios'
sheet.textContent = scenariosCss
document.head.appendChild(sheet)

// The runner's catalogue comes out of the WebAssembly module, so asking for
// it fetches the module. That waits for the first visit to the page that
// needs it.
let begun = false
watch(
  onScenarios,
  (on) => {
    sheet.disabled = !on
    document.title = on ? 'PTLC scenarios' : 'PTLC console'
    window.scrollTo({ top: 0 })
    if (on && !begun) {
      begun = true
      void startScenarios()
    }
  },
  { immediate: true },
)

const view = computed(() => (onScenarios.value ? Scenarios : App))
</script>

<template>
  <component :is="view" />
</template>

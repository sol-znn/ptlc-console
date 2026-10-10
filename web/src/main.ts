import { createApp } from 'vue'

// The design system's two faces, Space Grotesk for words and JetBrains Mono
// for everything the chain said. Both ship with the page: it is meant to work
// with no network at all once it has loaded.
import '@fontsource-variable/space-grotesk'
import '@fontsource-variable/jetbrains-mono'
import './style.css'

import Root from './Root.vue'
import { start } from './store.ts'

start()
createApp(Root).mount('#app')

/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<Record<string, never>, Record<string, never>, unknown>
  export default component
}

interface ImportMetaEnv {
  /** The node the page reads when nothing was picked; ws://127.0.0.1:35998 if unset. */
  readonly VITE_DEFAULT_NODE?: string
}

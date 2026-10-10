import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// A static page: there is no back end to forward to. The chain it reads is a
// node the visitor picks, or the WebAssembly module in src/sim/gen, which
// `npm run sim` builds from ../sim (it needs Go) and which must exist before
// `dev` or `build`.
export default defineConfig({
  plugins: [vue()],
  // Relative, so the same build works at the root of a domain and under
  // /ptlc-console/ on GitHub Pages without being told which.
  base: './',
  build: {
    target: 'es2022',
    // Mermaid is the bulk of the script and only the swap bench draws with
    // it, so it loads when that bench opens.
    chunkSizeWarningLimit: 900,
  },
  worker: { format: 'es' },
  server: { port: 5184 },
  test: {
    include: ['tests/**/*.test.ts'],
    environment: 'node',
  },
})

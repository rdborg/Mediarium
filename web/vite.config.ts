import react from '@vitejs/plugin-react'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { defineConfig, type Plugin } from 'vite'

// Writes this build's id and the list of files the page needs into the service
// worker (public/push-sw.js), so the worker keeps them for the next visit and
// changes when the app does, which is how browsers notice a new version.
function workerManifest(): Plugin {
  return {
    name: 'mediarium-worker-manifest',
    apply: 'build',
    closeBundle() {
      const html = readFileSync('dist/index.html', 'utf8')
      const files = [...new Set([...html.matchAll(/(?:\.\/|\/)?(assets\/[^"'\s>]+\.(?:js|css))/g)].map((m) => m[1]))].sort()
      const id = createHash('sha1').update(files.join('|')).digest('hex').slice(0, 10)
      const path = 'dist/push-sw.js'
      let js = readFileSync(path, 'utf8')
      js = js.replace("const BUILD = 'dev'", `const BUILD = '${id}'`).replace('const PRECACHE = []', `const PRECACHE = ${JSON.stringify(files)}`)
      writeFileSync(path, js)
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), workerManifest()],
  // Relative addresses in the built files, so the same build works at the root
  // and under any URL_BASE prefix. The server rewrites the few files that need
  // an absolute address (internal/api/static_prefix.go).
  base: './',
  build: {
    outDir: 'dist',
  },
  server: {
    // Go backend during `npm run dev` (the UI talks to the same REST
    // API the production single binary serves itself in prod).
    proxy: {
      '/api': {
        target: process.env.API_TARGET ?? 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})

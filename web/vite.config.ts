import react from '@vitejs/plugin-react'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { defineConfig, type Plugin } from 'vite'

// Writes this build's id and the list of files the page needs into the service
// worker (public/push-sw.js), so the worker keeps them for the next visit and
// changes when the app does, which is how browsers notice a new version.
function workerManifest(): Plugin {
  let outDir = 'dist'
  return {
    name: 'mediarium-worker-manifest',
    apply: 'build',
    configResolved(config) {
      outDir = config.build.outDir
    },
    closeBundle() {
      const html = readFileSync(join(outDir, 'index.html'), 'utf8')
      const files = [...new Set([...html.matchAll(/(?:\.\/|\/)?(assets\/[^"'\s>]+\.(?:js|css))/g)].map((m) => m[1]))].sort()
      const id = createHash('sha1').update(files.join('|')).digest('hex').slice(0, 10)
      const path = join(outDir, 'push-sw.js')
      let js = readFileSync(path, 'utf8')
      js = js.replace("const BUILD = 'dev'", `const BUILD = '${id}'`).replace('const PRECACHE = []', `const PRECACHE = ${JSON.stringify(files)}`)
      writeFileSync(path, js)
    },
  }
}

// The page starts by loading the chosen language, and only then fetches the app (src/main.tsx), so the app's own
// files used to wait for the language file. Naming them in the page lets the browser fetch them at the same time;
// they still run only after the language is in.
// The preloaded stylesheets carry crossorigin too, because the app asks for them that way: a preload with a different
// credentials mode is not reused, so the file would be fetched twice and the console would warn on every page.
function preloadApp(): Plugin {
  return {
    name: 'mediarium-preload-app',
    apply: 'build',
    transformIndexHtml: {
      order: 'post',
      handler(_html, ctx) {
        const bundle = ctx.bundle
        if (!bundle) return
        const start = Object.values(bundle).find((c) => c.type === 'chunk' && c.isDynamicEntry && /start\.tsx$/.test(c.facadeModuleId ?? ''))
        if (!start || start.type !== 'chunk') return
        const scripts = new Set<string>()
        const styles = new Set<string>()
        const walk = (name: string) => {
          const c = bundle[name]
          // the page's own script is already in the page, and it is what the preload is for
          if (!c || c.type !== 'chunk' || c.isEntry || scripts.has(name)) return
          scripts.add(name)
          for (const css of c.viteMetadata?.importedCss ?? []) styles.add(css)
          for (const dep of c.imports) walk(dep)
        }
        walk(start.fileName)
        const tags: { tag: string; attrs: Record<string, string | boolean>; injectTo: 'head' }[] = []
        for (const f of scripts) tags.push({ tag: 'link', attrs: { rel: 'modulepreload', crossorigin: true, href: `./${f}` }, injectTo: 'head' })
        for (const f of styles) tags.push({ tag: 'link', attrs: { rel: 'preload', as: 'style', crossorigin: true, href: `./${f}` }, injectTo: 'head' })
        return tags
      },
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), preloadApp(), workerManifest()],
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

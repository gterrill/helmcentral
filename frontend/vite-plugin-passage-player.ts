import { readFileSync } from 'node:fs'
import path from 'node:path'
import { build, type Plugin } from 'vite'

// The blog card's player (ADR 0205) is TypeScript in src/export-player, built
// from the same modules the app's replay uses. passage-export.ts needs its
// built text at export time, in `vite`, `vite build` and vitest alike, so the
// build is a virtual module: importing 'virtual:passage-player' runs a small
// IIFE library build and hands back the script as a string. No second build
// command, no generated file to go stale.

const VIRTUAL = 'virtual:passage-player'
const RESOLVED = `\0${VIRTUAL}`
const ENTRY = 'src/export-player/main.ts'

interface Built {
  code: string
  files: string[]
}

const EXTENSIONS = ['.ts', '.tsx', '/index.ts']
const IMPORT = /(?:from|import\()\s*['"](@\/[^'"]+)['"]/g

/** Every source file the player reaches through '@/' imports, found by reading
 * the files rather than by building them, so it is known even when the build
 * fails and the edit that would fix it is still to be seen. */
export function playerSources(root: string): string[] {
  const src = path.resolve(root, 'src')
  const seen = new Set<string>()
  const visit = (file: string) => {
    if (seen.has(file)) return
    seen.add(file)
    let text: string
    try {
      text = readFileSync(file, 'utf8')
    } catch {
      return
    }
    for (const m of text.matchAll(IMPORT)) {
      const base = path.join(src, m[1].slice(2))
      const found = EXTENSIONS.map((e) => base + e).find((f) => {
        try { readFileSync(f); return true } catch { return false }
      })
      if (found) visit(found)
    }
    // Relative imports inside the player's own folder.
    for (const m of text.matchAll(/from\s*['"](\.\/[^'"]+)['"]/g)) {
      const base = path.join(path.dirname(file), m[1])
      const found = EXTENSIONS.map((e) => base + e).find((f) => {
        try { readFileSync(f); return true } catch { return false }
      })
      if (found) visit(found)
    }
  }
  visit(path.resolve(root, ENTRY))
  return [...seen]
}

export function passagePlayer(root: string): Plugin {
  let built: Promise<Built> | null = null
  let watched = new Set<string>(playerSources(root))

  const run = async (): Promise<Built> => {
    const result = await build({
      root,
      configFile: false,
      mode: 'production',
      logLevel: 'warn',
      resolve: { alias: { '@': path.resolve(root, 'src') } },
      // Always a production build, whatever is running it: the bundle a blog
      // receives must not depend on whether `vite`, `vite build` or vitest made
      // it (no dev-only debugging hooks).
      define: {
        'import.meta.env.DEV': 'false',
        'import.meta.env.PROD': 'true',
        'import.meta.env.MODE': JSON.stringify('production'),
        'process.env.NODE_ENV': JSON.stringify('production'),
      },
      build: {
        write: false,
        minify: true,
        target: 'ES2020',
        emptyOutDir: false,
        lib: { entry: path.resolve(root, ENTRY), name: 'HcPlayer', formats: ['iife'], fileName: 'passage-player' },
        // MapLibre is the page's own global and three.js is fetched by the
        // card at run time: neither is bundled, and neither is imported.
        rollupOptions: { external: ['maplibre-gl', 'three'] },
      },
    })
    const outputs = (Array.isArray(result) ? result : [result]).flatMap((r) => ('output' in r ? r.output : []))
    const chunk = outputs.find((o) => o.type === 'chunk')
    if (!chunk || chunk.type !== 'chunk') throw new Error('The passage player build produced no script')
    return { code: chunk.code, files: chunk.moduleIds.filter((id) => !id.includes('node_modules') && !id.startsWith('\0')) }
  }

  return {
    name: 'passage-player',
    resolveId(id) {
      return id === VIRTUAL ? RESOLVED : null
    },
    async load(id) {
      if (id !== RESOLVED) return null
      // Watched whether or not the build works, so fixing a broken file is seen.
      for (const f of watched) this.addWatchFile(f)
      const attempt = (built ??= run())
      try {
        const { code, files } = await attempt
        watched = new Set([...watched, ...files])
        for (const f of files) this.addWatchFile(f)
        return `export default ${JSON.stringify(code)}`
      } catch (err) {
        // A failed build is not kept: the next load tries again.
        if (built === attempt) built = null
        throw err
      }
    },
    // A player source may lie outside the app's own module graph, so Vite would
    // find no module to update for it. Clear the build, invalidate the virtual
    // module and hand it back with the rest, so passage-export.ts updates.
    hotUpdate({ file, modules }) {
      if (!watched.has(file)) return
      built = null
      const mod = this.environment.moduleGraph.getModuleById(RESOLVED)
      if (!mod) return
      this.environment.moduleGraph.invalidateModule(mod)
      return [...modules, mod]
    },
  }
}

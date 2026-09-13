import path from 'node:path'
import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const apiProxyTarget = process.env.VITE_API_PROXY_TARGET || 'http://localhost:8080'

export default defineConfig(({ mode }) => ({
  // React Fast Refresh. Without this plugin Vite has no component boundary to
  // swap at, so every save under src/ degraded to a full page reload — losing
  // component state (an open drawer, a half-filled settings form, map pan and
  // zoom) on each edit. The plugin also supplies the automatic JSX runtime, so
  // components no longer need React in scope.
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  // react-grid-layout's bundled react-draggable reads `process.env.NODE_ENV` (a Node/webpack
  // convention) for a dev-only debug log. Vite doesn't polyfill `process` in the browser, so
  // without this define the reference throws `ReferenceError: process is not defined` on every
  // drag-start mousedown, aborting the drag before it engages.
  define: {
    'process.env.NODE_ENV': JSON.stringify(mode),
  },
  server: {
    port: 5173,
    host: '0.0.0.0',
    // No usePolling here on purpose. This was `usePolling: true, interval: 300`,
    // which stat-polled every watched file every 300ms, forever, whether or not
    // a browser was connected — the container bind-mounts the whole repo
    // (~32k files) at /workspace, and that poll loop was the frontend
    // container's entire ~5% idle CPU baseline.
    //
    // Polling was tested and rejected, not merely never tried: Docker Desktop
    // for macOS forwards FSEvents into the container as inotify over VirtioFS,
    // so native watching works. Verified end to end with a real browser on
    // :5173 — appending a module-level `console.log` to src/main.tsx made the
    // browser execute the new code on its own, with no manual reload. With
    // @vitejs/plugin-react now in `plugins` above, that update is a true Fast
    // Refresh component swap rather than a full page reload. If you are on a
    // setup where inotify does not cross the mount (older Docker Desktop with
    // gRPC-FUSE/osxfs, some Windows/WSL2 layouts) and HMR stops firing, restore
    // polling with `interval: 1000` and an `ignored` list covering
    // '**/.git/**', '**/dist/**', '**/ds-bundle/**' and '**/backend/**' —
    // Vite only needs frontend/src, but the mount gives it the whole repo.
    watch: {
      ignored: ['**/settings.yaml'],
    },
    fs: {
      allow: ['..'],
    },
    proxy: {
      '/api': {
        target: apiProxyTarget,
        // Deliberately NOT changeOrigin. The backend builds the basemap
        // style's absolute sprite URL from the incoming Host (MapLibre
        // rejects a relative sprite URL and aborts the whole style load),
        // and in production it serves the frontend itself, so Host is the
        // browser-facing origin. Rewriting Host here would make dev the
        // only place that disagrees, handing the browser a sprite URL on
        // the backend's own port. The target is our own Go server, which
        // does not vhost on Host, so preserving it costs nothing.
        // See docs/adr/0067-carto-basemap-proxy-and-offline-cache.md.
        changeOrigin: false,
        // The radar spoke relay is a WebSocket on this same /api prefix.
        // Vite does not forward upgrade requests unless an entry opts in,
        // and it fails silently: the socket never opens, nothing is logged,
        // and the overlay just shows nothing. Measured before this line
        // existed, the browser got 0 frames through :5173 against 19 in the
        // same 8 s straight to the backend on :8080.
        //
        // Production never hits this path, because there the Go server
        // serves the frontend itself and no proxy sits in between, which is
        // what makes the gap easy to miss until someone tries it in dev.
        ws: true,
      }
    }
  },
  build: {
    target: 'ES2020',
    // Vite 8 minifies CSS with lightningcss, which takes browser versions
    // rather than a JS language target — `target: 'ES2020'` above is not a
    // value it accepts, and leaving it to inherit fails the build outright
    // with `Unsupported target "ES2020"`. These versions encode the Baseline
    // 2024 target that AGENTS.md sets for this project, so authored CSS is
    // downlevelled no further than that floor. It drops Safari below 16.4.
    cssTarget: ['chrome111', 'edge111', 'firefox111', 'safari16.4'],
    outDir: 'dist',
  },
  test: {
    // Migrated from jsdom: Vitest's own breakdown showed per-file environment
    // setup as the dominant cost, roughly 2-3x cheaper here than under jsdom on
    // the same machine, with no change to test bodies. setup.ts carries the two
    // happy-dom-specific stubs this move needed (window.confirm/alert, which
    // neither environment implements, and forcing off Element.getAnimations,
    // which happy-dom implements and jsdom doesn't, so Base UI's exit-animation
    // completion hook took its synchronous no-Animations-API fallback under
    // jsdom but not here).
    environment: 'happy-dom',
    // Unlike jsdom, happy-dom actually navigates an <iframe>'s src, which
    // means EmbedTile's tests (fixtures point at real boat and Grafana hosts,
    // e.g. 192.168.50.240:3030) were making real outbound TCP connections
    // that only failed because nothing in this sandbox answers on them. No
    // test depended on that navigation completing, so turn it off rather
    // than let the suite's pass/fail depend on network reachability.
    environmentOptions: {
      happyDOM: { settings: { navigation: { disableChildFrameNavigation: true } } },
    },
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    // Worker threads rather than Vitest's default child-process forks. Nearly
    // all of this suite's wall clock is per-file fixed cost (happy-dom setup, then
    // transforming and importing the module graph) rather than the test bodies
    // themselves, and threads start cheaper and share a module cache across
    // files, which cuts transform and import time roughly in half.
    //
    // Files stay isolated (pool isolation is still on): the suite depends on
    // it, since Testing Library's auto-cleanup is per-file and sharing one
    // happy-dom document across files leaves mounted trees behind, breaking every
    // getByText that then matches twice.
    pool: 'threads',
  },
}))

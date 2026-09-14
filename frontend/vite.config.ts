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
    rollupOptions: {
      output: {
        // rolldown's own grouping API (output.codeSplitting.groups), not the
        // deprecated Rollup-compat `manualChunks(id)` function this replaced.
        // The two are not equivalent here: manualChunks is shimmed onto a
        // *single* codeSplitting group with one dynamic `name()`, which by
        // default (`includeDependenciesRecursively: true`) recursively pulls
        // a captured module's dependencies into whichever group visited it
        // first - so @base-ui/react's direct `require('react-dom')` was
        // dragging the real react-dom implementation into dashboard-vendor
        // regardless of manualChunks separately saying react-dom's own id
        // belonged in react-vendor. Explicit `priority` across real groups
        // fixes that: rolldown resolves groups highest-priority first and
        // removes whatever a group captures from every lower-priority
        // group's pool (see the CodeSplittingGroup.priority doc in
        // node_modules/rolldown/dist/shared/define-config-*.d.mts), so
        // react-vendor claims react/react-dom/scheduler before
        // dashboard-vendor is ever evaluated, and dashboard-vendor ends up
        // importing them from react-vendor like any other cross-chunk
        // reference instead of re-bundling them.
        codeSplitting: {
          groups: [
            {
              // Vite's own runtime helper that every React.lazy()/dynamic
              // import() call gets wrapped in (__vitePreload, module id
              // "\0vite/preload-helper.js" - the leading \0 marks it a
              // virtual module, never a real node_modules path). It has no
              // group of its own to match against, so with no explicit
              // claim here the "recursively pull a captured module's
              // dependencies into whichever group visited it first" behaviour
              // described below swept it into map-vendor - one lazy() call
              // anywhere in the app (say, sea-state-tile.tsx's) pulls in this
              // shared helper, and since map-vendor's own maplibre-gl/
              // react-map-gl modules happened to be the first group to touch
              // it, the *entire* 1 MB map-vendor chunk became a static,
              // eagerly-preloaded dependency of the entry chunk purely to
              // supply this one tiny function - defeating every map
              // component's own lazy-split. Top priority claims it before
              // any other group's recursive capture can.
              name: 'vite-preload-helper',
              priority: 20,
              test: (id) => id === '\0vite/preload-helper.js',
            },
            {
              name: 'react-vendor',
              priority: 10,
              // Matched on path segments rather than a bare
              // `id.includes('react')`: react, react-dom and scheduler are
              // what every other group below actually shares, and a loose
              // substring would also catch react-markdown, react-map-gl,
              // react-grid-layout, react-resizable, react-smooth,
              // react-transition-group, react-draggable and react-is - none
              // of which belong in this chunk.
              test: (id) =>
                id.includes('/node_modules/react/') ||
                id.includes('/node_modules/react-dom/') ||
                id.includes('/node_modules/scheduler/'),
            },
            {
              name: 'map-vendor',
              priority: 8,
              test: (id) =>
                id.includes('maplibre-gl') ||
                id.includes('react-map-gl') ||
                // Path-segment, not a bare `id.includes('mapbox')`: the
                // only real "mapbox" dependency this project pulls in is
                // the @mapbox/* scope (point-geometry, vector-tile, ... -
                // all maplibre-gl transitive deps). react-map-gl only
                // imports its own maplibre provider
                // (`react-map-gl/maplibre`), so its sibling
                // `@vis.gl/react-mapbox` package never actually reaches the
                // bundle, but a loose substring match is one dependency
                // bump away from silently pulling in whatever unrelated
                // package next happens to spell "mapbox" somewhere in its
                // path.
                id.includes('/node_modules/@mapbox/'),
            },
            {
              name: 'dashboard-vendor',
              priority: 6,
              test: (id) =>
                id.includes('react-grid-layout') ||
                id.includes('react-resizable') ||
                id.includes('@base-ui') ||
                id.includes('@floating-ui'),
            },
            {
              // recharts split out of dashboard-vendor on purpose (kiosk
              // bundle-split follow-up): react-grid-layout is genuinely
              // eager (the dashboard grid itself), so grouping recharts
              // alongside it forced the whole ~395 KB library into the
              // startup bundle even after sea-state-tile.tsx/forecast-
              // drawer.tsx started reaching it only through a React.lazy()
              // boundary - manualChunks groups by module id, not by import
              // graph reachability, so a "vendor" chunk is only as lazy as
              // its least-lazy member. A dedicated group lets this one be
              // exactly as lazy as its own (lazy) importers.
              name: 'chart-vendor',
              priority: 5,
              test: (id) => id.includes('recharts'),
            },
            {
              name: 'markdown-vendor',
              priority: 4,
              test: (id) =>
                id.includes('react-markdown') ||
                id.includes('remark-gfm') ||
                id.includes('github-slugger') ||
                // Path-segment, not a bare `id.includes('marked')`: no
                // "marked" package is actually a dependency here, but that
                // substring also matches lucide-react's
                // dist/esm/icons/book-marked.js - the moment anything
                // imports the BookMarked icon, a loose match would
                // silently route it into markdown-vendor instead of
                // ui-vendor.
                id.includes('/node_modules/marked/'),
            },
            {
              name: 'ui-vendor',
              priority: 2,
              test: (id) =>
                id.includes('lucide-react') ||
                id.includes('clsx') ||
                id.includes('tailwind-merge') ||
                id.includes('class-variance-authority') ||
                id.includes('sonner'),
            },
            // No catch-all group. A `vendor` group matching bare
            // `node_modules` used to make every unmatched dependency a
            // candidate for an eagerly loaded chunk even when only a
            // lazy-loaded drawer imports it - leaving a dependency
            // unmatched instead lets rolldown's automatic chunking place it
            // with whatever chunk actually imports it, so a dependency used
            // only behind a React.lazy() boundary stays out of the startup
            // bundle.
          ],
        },
      },
    },
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

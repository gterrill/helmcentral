# ADR 0046: Vite 8 makes the CSS browser floor an explicit decision, set to Baseline 2024

## Status

Accepted.

## Context

The frontend build sat on Vite 5.4.21 for a long time. Vite supports only its `latest` and `previous` branches — currently 8.x and 7.x — so 5.x had stopped receiving fixes, and [GHSA-4w7w-66w2-5vf9](https://github.com/advisories/GHSA-4w7w-66w2-5vf9) (path traversal in optimized-deps `.map` handling, affecting `<=6.4.1`) had no fixed 5.x release to move to. The 6.x line was patched; the 5.x line simply ended inside the affected range.

That advisory and the esbuild dev-server one never shipped: `frontend/Dockerfile` serves a static `dist/` through `http-server` and runs no Vite. The exposure was to the development machine only.

Vite 7 was taken first, deliberately, as a bump with no engine change — still Rollup and esbuild, and `@vitejs/plugin-react` 4.7.0 already declared `^7.0.0`. Vite 8 was then evaluated separately because it replaces Rollup with Rolldown and esbuild with oxc, which is where real breakage would surface against a ~2.3 MB bundle carrying `maplibre-gl`, `recharts` and `react-grid-layout`.

**Vite 8 does not build this project as configured.** `build.target: 'ES2020'` is a JS language target. Vite 8 minifies CSS with lightningcss, which takes browser versions instead, and inheriting the JS target fails the build outright:

```
[plugin vite:css-post]
Error: [lightningcss minify] Unsupported target "ES2020"
```

Under Vite 5 and 7 this never came up — esbuild accepted the same value for both. So upgrading forces a question the build had never had to answer: **which browsers must the shipped CSS actually support?**

## Decision

### 1. Vite 8, with the CSS floor set explicitly to Baseline 2024

```ts
build: {
  target: 'ES2020',
  cssTarget: ['chrome111', 'edge111', 'firefox111', 'safari16.4'],
}
```

These versions encode the Baseline 2024 target that `AGENTS.md` already sets for this project, so the build now enforces the policy the repo had only stated. **This drops Safari below 16.4**, and with it iPads too old to run iPadOS 16.4.

Two alternatives were measured and rejected:

- **ES2020-era browsers** (`chrome80`, `safari13.1`) would have preserved whatever compatibility the old build incidentally had, at 78.89 kB gzip. Rejected because "whatever it happened to do before" is not a support policy, and the project already has one written down.
- **`esnext`**, no downlevelling at all, at 78.78 kB gzip. Rejected as relying on every viewing device being current, with silent breakage as the failure mode.

The spread across all three is 0.12 kB gzip, so browser support determined the choice rather than output size.

### 2. `build.target` stays `ES2020` for JS

Only the CSS pipeline changed. The JS target is untouched, so this upgrade moves no JS compatibility boundary.

## Consequences

- Safari 16.3 and earlier no longer receive downlevelled CSS. On a boat this means an iPad that cannot reach iPadOS 16.4 is no longer a supported helm display. If one turns up, the fix is to widen `cssTarget`, not to pin Vite.
- Builds got substantially faster: ~6.3s to ~1.4s, Rolldown against Rollup.
- The client bundle shrank from 654 kB gzip to 620 kB.
- `@vitejs/plugin-react` moves to 6.x, whose `oxc-transform-react`, `@rolldown/plugin-babel` and `babel-plugin-react-compiler` peers are all declared optional and are not installed. React Fast Refresh was verified working without them.
- `frontend/src/test/toolchain-node-floor.test.ts` pins the Vite major against the node major in both `Dockerfile` and `frontend/Dockerfile`. Vite 7 raised the node floor to `^20.19.0 || >=22.12.0` while `frontend/Dockerfile` was still `node:18-alpine`. That image is not on the release path — releases build from the root `Dockerfile`, which was already `node:24-alpine`, and CI's release dry run runs goreleaser on node 24. What it does serve is the `frontend` service of `docker compose --profile prod`, so the break would have hit anyone running the compose production stack rather than a tagged release. The test covers both files so neither can drift from the pinned Vite again.

## Addendum: the entry chunk has its own, older JS floor

This ADR's `cssTarget` sets a CSS floor of Baseline 2024 (Safari 16.4 among others). That is a floor for authored CSS. It says nothing about what JS syntax is safe to ship in the module the browser has to parse before any of that CSS, or any of the app, can run at all: `dist/index.html`'s single `<script type="module" src="...">` tag, referred to here as the entry chunk.

The wall-display kiosk found that gap the hard way. Its browser is WPE WebKit 2.38.5, a JavaScriptCore build from the Safari 16.0 era, not 16.4. react-markdown's dependency tree pulled `mdast-util-gfm-autolink-literal` into the entry chunk (assistant-markdown.tsx and manual-markdown.tsx imported it as a static import, so it built into the same chunk as everything else), and that module contains a regex lookbehind literal, `(?<=...)`. Regex literal syntax is validated at parse time in the ECMAScript grammar, not deferred to when the code using it runs, so a single unsupported literal anywhere in a script file makes the whole file fail to parse. The kiosk's screen went blank: nothing in the entry chunk ran, because the entry chunk never finished parsing. Lookbehind assertions did not reach JavaScriptCore until Safari 16.4, in March 2023 (verified against MDN's compatibility data); WPE WebKit 2.38.5 predates that.

The fix already shipped separately: the markdown renderers now load behind `React.lazy`, reached through a dynamic `import()`, so `mdast-util-gfm-autolink-literal` and its lookbehind literal build into their own chunk instead of the entry chunk. `frontend/src/test/markdown-lazy.test.tsx` pins that both wrapper components never touch react-markdown synchronously on first render.

The decision this addendum records:

- **The entry chunk's floor is WPE WebKit 2.38.5 / Safari 16.0.** This is a JS-syntax floor, stricter than and older than both this ADR's `cssTarget` (Safari 16.4) and the Baseline 2024 floor AGENTS.md states for the project generally. It is not a change to either of those; it is a narrower, additional floor that applies specifically to whatever ships in the one chunk `dist/index.html` loads directly.
- **Anything needing newer JS syntax belongs behind a dynamic `import()`, never in the entry chunk.** A lazily-loaded chunk only has to parse once the app decides to load it, on whatever browser the app is already running in by that point. The failure mode this addendum is about, a script that cannot parse at all, does not apply to code the entry chunk never has to touch.
- **The floor for lazily-loaded code is unchanged: Baseline 2024, Safari 16.4.** This addendum tightens the entry chunk specifically. It does not lower or raise the floor stated elsewhere in this ADR or in AGENTS.md for everything else.

That decision is now enforced mechanically, not left for someone to notice in review. `frontend/scripts/check-entry-chunk.mjs` runs as the last step of `npm run build` (`tsc && vite build && node scripts/check-entry-chunk.mjs`), reads the entry chunk `dist/index.html` actually references, and fails the build if it finds a regex lookbehind (`(?<=`, `(?<!`) anywhere in it. It scans for lookbehind as regex-literal syntax specifically, not as a plain substring search, so a string or template literal that happens to contain the same characters (maplibre-gl builds one of its regexes from a template string handed to the `RegExp` constructor at runtime, which is not a regex literal and parses everywhere) is left alone. Unicode property escapes (`\p{...}`) are not checked: JavaScriptCore has parsed them since Safari 11.1, and maplibre-gl ships two in the entry chunk today without trouble on the wall.
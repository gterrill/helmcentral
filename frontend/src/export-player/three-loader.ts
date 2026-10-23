// Fetching three.js for the boat model. The card never bundles it: the pinned
// build comes from jsDelivr, is fetched with its integrity hash (the browser
// refuses a body that does not match), and only then run, from a Blob URL.
// One load serves every card on the page: the promise is kept on the page
// itself, keyed by address and hash, so two cards evaluate three.js once.

import type { Three } from '@/lib/boat-geometry'

import { warnCsp } from './csp'

export interface ThreeSource {
  url: string
  sri: string
}

export interface LoaderDeps {
  fetchFn?: typeof fetch
  /** Runs the checked module text and returns its namespace. */
  evaluate?: (text: string) => Promise<Three>
}

async function evaluateModule(text: string): Promise<Three> {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/javascript' }))
  try {
    return (await import(/* @vite-ignore */ url)) as Three
  } catch (err) {
    warnCsp(err, 'script-src blob:')
    throw err
  } finally {
    URL.revokeObjectURL(url)
  }
}

const PAGE_KEY = '__hcpThree'
type Page = { [PAGE_KEY]?: Map<string, Promise<Three>> }

async function load({ url, sri }: ThreeSource, { fetchFn = (...a) => fetch(...a), evaluate = evaluateModule }: LoaderDeps): Promise<Three> {
  let res: Response
  try {
    res = await fetchFn(url, { integrity: sri, mode: 'cors' })
  } catch (err) {
    warnCsp(err, 'connect-src cdn.jsdelivr.net')
    throw err
  }
  if (!res.ok) throw new Error(`three.js: the CDN answered ${res.status}`)
  return evaluate(await res.text())
}

export function loadThree(source: ThreeSource, deps: LoaderDeps = {}): Promise<Three> {
  const page = globalThis as Page
  const loads = (page[PAGE_KEY] ??= new Map())
  const key = `${source.url}#${source.sri}`
  const have = loads.get(key)
  if (have) return have
  const started = load(source, deps)
  loads.set(key, started)
  // A failure is not kept: a card that comes along later tries again.
  started.catch(() => { if (loads.get(key) === started) loads.delete(key) })
  return started
}

// The query string as the page was opened. The router rewrites the address to
// its canonical form soon after the app starts, which drops it, so it is read
// once here. main.tsx imports this module before anything else: it is the only
// module whose result depends on import order. dev-flags.ts (the development
// switches) and inbox-launch.ts (how the share target opened the upload page)
// read it from here.
export const OPENED_SEARCH: string = typeof window === 'undefined' ? '' : window.location.search

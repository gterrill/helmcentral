/// <reference types="vite/client" />

// The blog card's player, built from src/export-player by the plugin in
// vite-plugin-passage-player.ts and handed over as text.
declare module 'virtual:passage-player' {
  const source: string
  export default source
}

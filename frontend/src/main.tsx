import React from 'react'
import ReactDOM from 'react-dom/client'

import { App } from '@/App'
import '@fontsource/geist-sans/400.css'
import '@fontsource/geist-sans/500.css'
import '@fontsource/geist-sans/600.css'
import '@fontsource/geist-sans/700.css'
import '@fontsource/geist-mono/400.css'
import '@fontsource/geist-mono/500.css'
import '@fontsource/geist-mono/600.css'
import '@fontsource/geist-mono/700.css'
import '@/index.css'

// This project's Baseline target is Baseline 2024 (see AGENTS.md); the
// Popover API only reached Baseline "newly available" on 2025-01-27, so
// browsers within the target window (e.g. Safari 16/17) need this polyfill.
async function renderApp() {
  if (!HTMLElement.prototype.hasOwnProperty('popover')) {
    await import('@oddbird/popover-polyfill')
  }

  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  )
}

void renderApp()

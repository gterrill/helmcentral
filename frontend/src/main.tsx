import React from 'react'
import ReactDOM from 'react-dom/client'

import { App } from '@/App'
import { AdminPage } from '@/components/caches-page'
import '@fontsource/geist-sans/400.css'
import '@fontsource/geist-sans/500.css'
import '@fontsource/geist-sans/600.css'
import '@fontsource/geist-sans/700.css'
import '@fontsource/geist-mono/400.css'
import '@fontsource/geist-mono/500.css'
import '@fontsource/geist-mono/600.css'
import '@fontsource/geist-mono/700.css'
import '@/index.css'

const isAdminPage = window.location.pathname === '/admin'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    {isAdminPage ? <AdminPage /> : <App />}
  </React.StrictMode>,
)

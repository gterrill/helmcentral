import React from 'react'
import ReactDOM from 'react-dom/client'

import { App } from '@/App'
import { AdminPage } from '@/components/caches-page'
import '@/index.css'

const isAdminPage = window.location.pathname === '/admin'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    {isAdminPage ? <AdminPage /> : <App />}
  </React.StrictMode>,
)

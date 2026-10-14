import { describe, it, expect, afterEach, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

import { ModelCatalogDialog, type ModelCapability } from '@/components/settings/sections/model-picker'
import { jsonResponse, stubApi } from '@/test/helpers/stub-api'

// The previous rows (and page count) stay up only while paging, sorting or
// searching in one open dialog for one capability. Closing it or switching
// capability must not show another capability's catalog.

const models = (n: number) =>
  Array.from({ length: 2 }, (_, i) => ({ id: `p/m${n}-${i}`, name: `Model ${n}-${i}`, price: 0, created_at: '' }))

function dialog(open: boolean, capability: ModelCapability) {
  return (
    <ModelCatalogDialog
      open={open}
      onOpenChange={() => {}}
      capability={capability}
      title="Choose a model"
      description="d"
      renderActions={() => null}
    />
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ModelCatalogDialog stale rows', () => {
  it('shows no page count from the previous capability after reopening', async () => {
    stubApi({
      'GET /api/assistant/models?capability=tools&sort=popular&order=desc&page=1&page_size=10': { models: models(1), page: { total_pages: 5 } },
      'GET /api/assistant/models?capability=images&sort=popular&order=desc&page=1&page_size=10': () => jsonResponse({ error: 'down' }, 502),
    })
    const view = render(dialog(true, 'tools'))
    await screen.findByText('Model 1-0')
    expect(screen.getByText('Page 1 of 5')).toBeTruthy()

    view.rerender(dialog(false, 'tools'))
    view.rerender(dialog(true, 'images'))
    await screen.findByText('Unable to load model catalog.')
    expect(screen.queryByText('Model 1-0')).toBeNull()
    expect(screen.getByText('Page 1 of 1')).toBeTruthy()
  })

  it('keeps the page count while the next page of the same catalog loads', async () => {
    let release!: () => void
    const gate = new Promise<void>((resolve) => { release = resolve })
    stubApi({
      'GET /api/assistant/models?capability=tools&sort=popular&order=desc&page=1&page_size=10': { models: models(1), page: { total_pages: 5 } },
      'GET /api/assistant/models?capability=tools&sort=popular&order=desc&page=2&page_size=10': async () => { await gate; return { models: models(2), page: { total_pages: 5 } } },
    })
    render(dialog(true, 'tools'))
    await screen.findByText('Model 1-0')
    screen.getByRole('button', { name: 'Next' }).click()
    await waitFor(() => expect(screen.getByText('Page 2 of 5')).toBeTruthy())
    release()
    await screen.findByText('Model 2-0')
  })
})

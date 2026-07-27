import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { EmbedConfigDialog } from '@/components/embed-config-dialog'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

const widget: DashboardLayoutItem = {
  id: 'embed:m1x8abcd',
  x: 0,
  y: 0,
  w: 6,
  h: 8,
  embed: { title: 'Windrose', url: 'http://boat.local:3000/d-solo/abc?panelId=2' },
}

function renderDialog(overrides: Partial<React.ComponentProps<typeof EmbedConfigDialog>> = {}) {
  const onSave = vi.fn()
  const onOpenChange = vi.fn()
  render(
    <EmbedConfigDialog
      widget={widget}
      open
      onOpenChange={onOpenChange}
      onSave={onSave}
      {...overrides}
    />,
  )
  return { onSave, onOpenChange }
}

const urlField = () => screen.getByLabelText(/url/i)
const titleField = () => screen.getByLabelText(/title/i)
const saveButton = () => screen.getByRole('button', { name: /save/i })

describe('EmbedConfigDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  test('seeds the fields from the widget being edited', () => {
    renderDialog()

    expect(urlField()).toHaveValue('http://boat.local:3000/d-solo/abc?panelId=2')
    expect(titleField()).toHaveValue('Windrose')
  })

  test('saves the trimmed title and URL', () => {
    const { onSave, onOpenChange } = renderDialog()

    fireEvent.change(urlField(), { target: { value: '  https://grafana.local/d-solo/x?panelId=7  ' } })
    fireEvent.change(titleField(), { target: { value: '  Polars  ' } })
    fireEvent.click(saveButton())

    expect(onSave).toHaveBeenCalledWith(widget.id, {
      title: 'Polars',
      url: 'https://grafana.local/d-solo/x?panelId=7',
    })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  test('blocks saving a non-http(s) URL and explains why', () => {
    const { onSave } = renderDialog()

    fireEvent.change(urlField(), { target: { value: 'javascript:alert(1)' } })

    expect(saveButton()).toBeDisabled()
    expect(screen.getByText(/must be an http/i)).toBeInTheDocument()

    fireEvent.click(saveButton())
    expect(onSave).not.toHaveBeenCalled()
  })

  test('blocks saving an empty URL without showing an error before anything is typed', () => {
    renderDialog({ widget: { ...widget, embed: { title: '', url: '' } } })

    expect(saveButton()).toBeDisabled()
    expect(screen.queryByText(/must be an http/i)).toBeNull()
  })

  test('re-enables saving once the URL is corrected', () => {
    const { onSave } = renderDialog()

    fireEvent.change(urlField(), { target: { value: 'not a url' } })
    expect(saveButton()).toBeDisabled()

    fireEvent.change(urlField(), { target: { value: 'https://grafana.local/d-solo/x' } })
    expect(saveButton()).toBeEnabled()

    fireEvent.click(saveButton())
    expect(onSave).toHaveBeenCalledWith(widget.id, { title: 'Windrose', url: 'https://grafana.local/d-solo/x' })
  })

  test('warns that an http embed is blocked when the app itself is served over https', () => {
    renderDialog()
    expect(screen.getByText(/mixed content|https/i)).toBeInTheDocument()
  })

  test('re-seeds the fields when a different widget is opened', () => {
    const { rerender } = render(
      <EmbedConfigDialog widget={widget} open onOpenChange={vi.fn()} onSave={vi.fn()} />,
    )
    fireEvent.change(urlField(), { target: { value: 'https://scratch.local/a' } })

    const other: DashboardLayoutItem = {
      ...widget,
      id: 'embed:m1x8efgh',
      embed: { title: 'Battery History', url: 'https://grafana.local/d-solo/b' },
    }
    rerender(<EmbedConfigDialog widget={other} open onOpenChange={vi.fn()} onSave={vi.fn()} />)

    expect(urlField()).toHaveValue('https://grafana.local/d-solo/b')
    expect(titleField()).toHaveValue('Battery History')
  })
})

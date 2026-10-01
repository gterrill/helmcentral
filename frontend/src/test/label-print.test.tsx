import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'
import qrcode from 'qrcode-generator'
import { PrintBinLabelsDialog, PrintLabelButton } from '@/components/inventory/label-print'
import { TagRow } from '@/components/inventory/tag-row'
import { binTagPath, equipmentTagPath, tagUrl } from '@/lib/tag-url'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

// What a scan must open is what the NFC tag opens: TagRow shows the exact
// string it writes, so the QR's encoded value is compared against that.
function nfcUrlFor(path: string): string {
  const { unmount } = render(<TagRow path={path} />)
  const shown = screen.getByTitle(/^https?:\/\//).getAttribute('title')!
  unmount()
  return shown
}

describe('tag URL builder', () => {
  it('builds bin and equipment paths and the absolute URL from the page origin', () => {
    expect(binTagPath('LAZ-02')).toBe('/inventory/bins/LAZ-02')
    expect(equipmentTagPath('abc-123')).toBe('/inventory/equipment/abc-123')
    expect(tagUrl('/inventory/bins/LAZ-02')).toBe(`${window.location.origin}/inventory/bins/LAZ-02`)
  })

  it('percent-encodes a bin code the way the app parses it back', () => {
    expect(binTagPath('A B')).toBe('/inventory/bins/A%20B')
  })
})

describe('PrintLabelButton', () => {
  it('encodes the same URL as the NFC tag for a bin', async () => {
    render(<PrintLabelButton path={binTagPath('LAZ-02')} code="LAZ-02" />)
    fireEvent.click(screen.getByRole('button', { name: 'Print label' }))
    const svgs = await screen.findAllByRole('img', { name: /QR code/ })
    const expected = nfcUrlFor(binTagPath('LAZ-02'))
    for (const svg of svgs) expect(svg).toHaveAttribute('data-value', expected)
  })

  it('encodes the same URL as the NFC tag for an equipment item', async () => {
    render(<PrintLabelButton path={equipmentTagPath('0b1e-uuid')} code="Raw water pump" />)
    fireEvent.click(screen.getByRole('button', { name: 'Print label' }))
    const svgs = await screen.findAllByRole('img', { name: /QR code/ })
    expect(svgs[0]).toHaveAttribute('data-value', nfcUrlFor(equipmentTagPath('0b1e-uuid')))
  })

  it('draws the modules a standard encoder produces for that URL', async () => {
    render(<PrintLabelButton path={binTagPath('LAZ-02')} code="LAZ-02" />)
    fireEvent.click(screen.getByRole('button', { name: 'Print label' }))
    const svg = (await screen.findAllByRole('img', { name: /QR code/ }))[0]
    const qr = qrcode(0, 'M')
    qr.addData(tagUrl(binTagPath('LAZ-02')))
    qr.make()
    // quiet zone of 4 modules each side
    expect(svg).toHaveAttribute('viewBox', `0 0 ${qr.getModuleCount() + 8} ${qr.getModuleCount() + 8}`)
    let dark = 0
    for (let r = 0; r < qr.getModuleCount(); r++) for (let c = 0; c < qr.getModuleCount(); c++) if (qr.isDark(r, c)) dark++
    expect(svg.querySelector('path')!.getAttribute('d')!.match(/M/g)!.length).toBe(dark)
  })

  it('prints the code large under the QR code, with the caption when given', async () => {
    render(<PrintLabelButton path={binTagPath('LAZ-02')} code="LAZ-02" caption="Lazarette" />)
    fireEvent.click(screen.getByRole('button', { name: 'Print label' }))
    await screen.findAllByRole('img', { name: /QR code/ })
    expect(screen.getAllByText('LAZ-02').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Lazarette').length).toBeGreaterThan(0)
  })

  it('opens the browser print dialog from the Print button', async () => {
    const print = vi.fn()
    vi.stubGlobal('print', print)
    render(<PrintLabelButton path={binTagPath('LAZ-02')} code="LAZ-02" />)
    fireEvent.click(screen.getByRole('button', { name: 'Print label' }))
    await screen.findAllByRole('img', { name: /QR code/ })
    fireEvent.click(screen.getByRole('button', { name: 'Print' }))
    expect(print).toHaveBeenCalledTimes(1)
  })
})

describe('PrintBinLabelsDialog', () => {
  const bins = [
    { id: 'b1', code: 'ER-01', zoneName: 'Engine room' },
    { id: 'b2', code: 'ER-02', zoneName: 'Engine room' },
    { id: 'b3', code: 'LAZ-01', zoneName: 'Lazarette' },
  ]

  it('renders one label per bin on the sheet', async () => {
    render(<PrintBinLabelsDialog open onOpenChange={() => {}} bins={bins} />)
    const sheet = within(await screen.findByRole('dialog')).getByTestId('label-sheet')
    await within(sheet).findAllByRole('img', { name: /QR code/ })
    expect(within(sheet).getAllByRole('img', { name: /QR code/ })).toHaveLength(3)
    for (const b of bins) expect(within(sheet).getByText(b.code)).toBeInTheDocument()
  })

  it('says so when there are no bins to print', () => {
    render(<PrintBinLabelsDialog open onOpenChange={() => {}} bins={[]} />)
    expect(screen.getByText('No bins to print yet.')).toBeInTheDocument()
  })
})

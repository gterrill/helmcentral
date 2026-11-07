import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { RodeProfileDiagram } from '@/components/rode-profile-diagram'

const base = {
  rodeM: 30,
  depthFromHawseM: 6,
  bowRollerHeightM: 1,
  tideCorrected: true,
  liftWarning: 'Too short for this load: the anchor would be lifted off the bottom.',
  catenaryParameterM: 10 as number | null,
  isImperial: false,
}

function label(): string {
  return screen.getByRole('img').getAttribute('aria-label') ?? ''
}

describe('RodeProfileDiagram', () => {
  it('draws an svg whose label states the rode, depth to hawse and bow height', () => {
    render(<RodeProfileDiagram {...base} />)
    expect(screen.getByRole('img').tagName.toLowerCase()).toBe('svg')
    expect(label()).toContain('30 m of rode')
    expect(label()).toContain('6.0 m')
    expect(label()).toContain('high water')
    expect(label()).toContain('1.0 m')
  })

  it('states the figures in feet when imperial', () => {
    render(<RodeProfileDiagram {...base} isImperial />)
    expect(label()).toContain('98 ft of rode')
    expect(label()).toContain('19.7 ft')
  })

  it('does not say high water when the depth is not tide corrected', () => {
    render(<RodeProfileDiagram {...base} tideCorrected={false} />)
    expect(label()).not.toContain('high water')
  })

  it('warns when the rode is too short to lie down under the load', () => {
    render(<RodeProfileDiagram {...base} rodeM={10} />)
    expect(screen.getByText('Too short for this load: the anchor would be lifted off the bottom.')).toBeTruthy()
  })

  it('shows the lift warning it is given', () => {
    render(<RodeProfileDiagram {...base} rodeM={10} liftWarning="The ratio rule is too short." />)
    expect(screen.getByText('The ratio rule is too short.')).toBeTruthy()
    expect(screen.queryByText(/^Too short for this load/)).toBeNull()
  })

  it('draws no rode, and says why, when there is no load to draw it against', () => {
    const { container } = render(<RodeProfileDiagram {...base} catenaryParameterM={null} />)
    const note = screen.getByText('No wind load to draw the rode against.')
    expect(note.className).toContain('text-muted-foreground')
    expect(note.className).not.toContain('amber')
    expect(container.querySelector('svg')).toBeNull()
  })

  it('shows no lift warning when none is given', () => {
    render(<RodeProfileDiagram {...base} rodeM={10} liftWarning={null} />)
    expect(screen.queryByText(/anchor would be lifted/i)).toBeNull()
  })

  it('rounds the rode as the Pay Out figure does', () => {
    render(<RodeProfileDiagram {...base} rodeM={30.4} />)
    expect(label()).toContain('30 m of rode')
    expect(screen.getByText(/^Rode 30 m$/)).toBeTruthy()
  })

  it('states the length in feet when imperial', () => {
    render(<RodeProfileDiagram {...base} rodeM={9.2} depthFromHawseM={3} isImperial />)
    expect(screen.getByText(/^Rode 30 ft$/)).toBeTruthy()
  })

  it('shows its figures in a text-xs legend under the drawing, not as tiny svg text', () => {
    const { container } = render(<RodeProfileDiagram {...base} />)
    expect(container.querySelector('svg text')).toBeNull()
    expect(screen.getByText('Bow roller to bottom 6.0 m, high water').closest('ul')!.className).toContain('text-xs')
    expect(screen.getByText('Bow 1.0 m')).toBeTruthy()
    expect(screen.getByText('Rode 30 m')).toBeTruthy()
  })

  it('draws no "today" water line, only the high-water one', () => {
    const { container } = render(<RodeProfileDiagram {...base} />)
    expect(container.querySelectorAll('svg line[x1="0"]')).toHaveLength(1)
  })

  it('states the lying length on either tab whenever a load shapes the rode', () => {
    render(<RodeProfileDiagram {...base} />)
    expect(label()).toContain('18 m lying on the bottom')
  })

  it('says "just reaching the bottom" where the lying length rounds to nothing', () => {
    // The boundary length for a = 10, h = 6 is sqrt(156), so no chain lies down.
    render(<RodeProfileDiagram {...base} rodeM={Math.sqrt(156)} />)
    expect(label()).toContain('just reaching the bottom')
    expect(label()).not.toContain('0 m lying')
  })

  it('says it cannot draw a rode whose shape is unusable, rather than inventing one', () => {
    const { container } = render(<RodeProfileDiagram {...base} catenaryParameterM={-1} />)
    expect(screen.getByText("Can't draw this rode.")).toBeTruthy()
    expect(container.querySelector('svg')).toBeNull()
  })

  it('renders nothing when the rode cannot reach the seabed', () => {
    const { container } = render(<RodeProfileDiagram {...base} rodeM={5} />)
    expect(container.firstChild).toBeNull()
  })
})

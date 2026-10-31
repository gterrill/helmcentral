import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AssistantCard } from '@/components/assistant-card'
import { AssistantMarinaCard } from '@/components/assistant-marina-card'
import type { MarinaCard } from '@/lib/mate-cards'

import { marinaCard, marinaCardWithoutGeometry } from './helpers/marina-fixture'

// The map is MapLibre; its own file is not under test here.
vi.mock('@/components/assistant-marina-map-impl', () => ({
  default: ({ card }: { card: MarinaCard }) => (
    <div data-testid="marina-map-stub">
      {card.geometry.pontoons.length} pontoons, {card.geometry.facilities.length} facility points
    </div>
  ),
}))

const config = vi.hoisted(() => ({ distanceUnits: 'metric' }))
vi.mock('@/hooks/use-app-config', () => ({ useAppConfig: () => ({ ui: { distanceUnits: config.distanceUnits } }) }))

const limit = (over: Partial<MarinaCard['vesselLimitCheck'][number]>): MarinaCard['vesselLimitCheck'][number] => ({
  particular: 'draft',
  status: 'not charted',
  boatM: 1.4,
  chartedLimitM: null,
  ...over,
})

function withChecks(checks: unknown[]) {
  return marinaCard({
    vessel_limit_check: checks.map((c) => {
      const v = c as MarinaCard['vesselLimitCheck'][number]
      return { particular: v.particular, status: v.status, boat_m: v.boatM, charted_limit_m: v.chartedLimitM }
    }),
  })
}

describe('AssistantMarinaCard', () => {
  it('names the marina and puts what the map records in one row', () => {
    render(<AssistantMarinaCard card={marinaCard()} />)
    expect(screen.getByRole('region', { name: 'Marina' })).toBeInTheDocument()
    expect(screen.getByText('Coral Sea Marina Resort')).toBeInTheDocument()
    const facts = screen.getByTestId('marina-facts')
    expect(within(facts).getByText('520 berths')).toBeInTheDocument()
    expect(within(facts).getByText('Power available')).toBeInTheDocument()
    expect(within(facts).getByText('Fee charged')).toBeInTheDocument()
  })

  it('leaves out what the map does not record', () => {
    render(<AssistantMarinaCard card={marinaCard({ berths: null, fee: '', power_supply: '' })} />)
    const facts = screen.queryByTestId('marina-facts')
    expect(facts === null || !/berths|Power|Fee/.test(facts.textContent ?? '')).toBe(true)
  })

  it('says an unnamed marina is unnamed', () => {
    render(<AssistantMarinaCard card={marinaCard({ name: '' })} />)
    expect(screen.getByText('Unnamed marina')).toBeInTheDocument()
  })

  it('shows a limit badge for each particular, with the status word', () => {
    render(
      <AssistantMarinaCard
        card={withChecks([limit({}), limit({ particular: 'length', status: 'particular not set', boatM: null, chartedLimitM: 12 })])}
      />,
    )
    expect(screen.getByText('Draft not charted')).toBeInTheDocument()
    expect(screen.getByText('Length particular not set')).toBeInTheDocument()
  })

  it('draws "exceeds" in the critical palette and "ok" plainly', () => {
    render(
      <AssistantMarinaCard
        card={withChecks([
          limit({ status: 'exceeds', boatM: 2.4, chartedLimitM: 1.8 }),
          limit({ particular: 'length', status: 'ok', boatM: 11, chartedLimitM: 12 }),
        ])}
      />,
    )
    const exceeds = screen.getByText('Draft exceeds · 2.4 m of 1.8 m')
    expect(exceeds.className).toMatch(/red-/)
    expect(exceeds.className).toMatch(/dark:/)
    const ok = screen.getByText('Length ok · 11 m of 12 m')
    expect(ok.className).not.toMatch(/red-/)
    const muted = render(<AssistantMarinaCard card={withChecks([limit({})])} />).getByText('Draft not charted')
    expect(muted.className).toMatch(/text-muted-foreground/)
  })

  it('lists facilities by their operator names, and says when the fuel type is unknown', () => {
    render(<AssistantMarinaCard card={marinaCard()} />)
    const list = screen.getByRole('list', { name: 'Facilities' })
    expect(within(list).getByText('Fuel ×2')).toBeInTheDocument()
    expect(within(list).getByText('type unknown')).toBeInTheDocument()
    expect(within(list).getByText('Haul-out')).toBeInTheDocument()
    expect(within(list).getByText('travel lift')).toBeInTheDocument()
    expect(within(list).getByText('Coral Sea Marina Boatramp · no fee')).toBeInTheDocument()
    expect(list.textContent).not.toMatch(/haul_out|boat_shop|pump_out/)
  })

  it('names the fuels a fuel facility lists', () => {
    render(
      <AssistantMarinaCard
        card={marinaCard({
          facilities: [{ kind: 'fuel', name: '', count: 1, facts: { 'fuel:diesel': 'yes', 'fuel:octane_95': 'yes' } }],
        })}
      />,
    )
    expect(screen.getByText('diesel, octane 95')).toBeInTheDocument()
    expect(screen.queryByText(/type unknown/)).not.toBeInTheDocument()
  })

  it('writes the distance to a shop in the operator\'s distance units', () => {
    config.distanceUnits = 'imperial'
    try {
      render(<AssistantMarinaCard card={marinaCard()} />)
      const woolworths = screen.getByText('Woolworths').closest('li') as HTMLElement
      expect(woolworths).toHaveTextContent('0.5 nm')
      expect(woolworths).not.toHaveTextContent('980 m')
    } finally {
      config.distanceUnits = 'metric'
    }
  })

  it('folds entrance marks, pontoon arms and services ashore into sections', () => {
    render(<AssistantMarinaCard card={marinaCard()} />)
    expect(screen.getByText('Entrance marks (4)')).toBeInTheDocument()
    expect(screen.getByText('Pontoon arms (13)')).toBeInTheDocument()
    expect(screen.getByText('Shops and services ashore (10)')).toBeInTheDocument()
    expect(screen.getByText('Beacon ×2 · port · red')).toBeInTheDocument()
    expect(screen.getByText('B&C Arm')).toBeInTheDocument()
    const woolworths = screen.getByText('Woolworths').closest('li') as HTMLElement
    expect(woolworths).toHaveTextContent('980 m')
    expect(woolworths).toHaveTextContent('Mo-Fr 08:00-21:00')
  })

  it('leaves out a section the map has nothing for', () => {
    render(<AssistantMarinaCard card={marinaCard({ pontoon_arms: [], entrance_marks: [], ashore: [], geometry: { ...(marinaCard() as unknown as { geometry: object }).geometry, ashore: [] } })} />)
    expect(screen.queryByText(/Pontoon arms/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Entrance marks/)).not.toBeInTheDocument()
    expect(screen.queryByText(/ashore/)).not.toBeInTheDocument()
  })

  it('links an http or https website and a phone number', () => {
    render(<AssistantMarinaCard card={marinaCard({ contact: { website: 'https://www.coralseamarina.com/berthing/', phone: '+61 7 4948 3000', vhf: '73', email: '' } })} />)
    const site = screen.getByRole('link', { name: 'coralseamarina.com/berthing' })
    expect(site).toHaveAttribute('href', 'https://www.coralseamarina.com/berthing/')
    expect(site).toHaveAttribute('rel', expect.stringContaining('noopener'))
    expect(screen.getByRole('link', { name: '+61 7 4948 3000' })).toHaveAttribute('href', 'tel:+61749483000')
    expect(screen.getByText('VHF 73')).toBeInTheDocument()
  })

  it('shows a website that is not a web address as plain text, never a link', () => {
    render(<AssistantMarinaCard card={marinaCard({ contact: { website: 'javascript:alert(1)', phone: 'ask at the office', vhf: '', email: '' } })} />)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
    expect(screen.getByText('javascript:alert(1)')).toBeInTheDocument()
    expect(screen.getByText('ask at the office')).toBeInTheDocument()
  })

  it('says where the data came from and how current it is', () => {
    render(<AssistantMarinaCard card={marinaCard()} />)
    expect(screen.getByTestId('marina-source')).toHaveTextContent('OpenStreetMap · data as of 29 Oct 2026')
  })

  it('shows no data date when the map did not give one, and never today', () => {
    render(<AssistantMarinaCard card={marinaCard({ data_as_of: '' })} />)
    expect(screen.getByTestId('marina-source')).toHaveTextContent(/^OpenStreetMap$/)
  })

  it('warns when the lists were cut', () => {
    render(<AssistantMarinaCard card={marinaCard({ truncated: true })} />)
    expect(screen.getByText(/lists may be incomplete/i)).toBeInTheDocument()
  })

  it('says the map is simplified when its shapes were cut, and not otherwise', () => {
    const { unmount } = render(<AssistantMarinaCard card={marinaCard({ geometry_truncated: true })} />)
    expect(screen.getByText(/map is simplified/i)).toBeInTheDocument()
    unmount()
    render(<AssistantMarinaCard card={marinaCard()} />)
    expect(screen.queryByText(/map is simplified/i)).not.toBeInTheDocument()
  })

  it('draws no map for a marina mapped only as a point', () => {
    const base = marinaCard()
    const geometry = { ...base.geometry, outline: [{ kind: 'point', points: [[-20.26, 148.71]] }], pontoons: [] }
    render(<AssistantMarinaCard card={marinaCard({ geometry })} />)
    expect(screen.queryByTestId('marina-map')).not.toBeInTheDocument()
  })

  it('draws the map when the marina has an outline or pontoons', async () => {
    render(<AssistantMarinaCard card={marinaCard()} />)
    expect(await screen.findByTestId('marina-map-stub')).toHaveTextContent('22 pontoons, 9 facility points')
  })

  it('draws no map when there is no outline and no pontoon', () => {
    render(<AssistantMarinaCard card={marinaCardWithoutGeometry()} />)
    expect(screen.queryByTestId('marina-map-stub')).not.toBeInTheDocument()
    expect(screen.queryByTestId('marina-map')).not.toBeInTheDocument()
    // The rest of the card is still there.
    expect(screen.getByText('Coral Sea Marina Resort')).toBeInTheDocument()
  })

  it('is what the thread draws for a marina card', async () => {
    render(<AssistantCard card={marinaCard()} />)
    expect(await screen.findByText('Coral Sea Marina Resort')).toBeInTheDocument()
  })

  it('never says widget', () => {
    const { container } = render(<AssistantMarinaCard card={marinaCard()} />)
    expect(container.textContent).not.toMatch(/widget/i)
  })
})

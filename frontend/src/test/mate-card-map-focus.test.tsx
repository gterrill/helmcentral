import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AssistantMarinaMapImpl from '@/components/assistant-marina-map-impl'

import { marinaCard } from './helpers/marina-fixture'

vi.mock('react-map-gl/maplibre', () => ({
  Map: ({ children }: { children?: ReactNode }) => <div data-testid="map">{children}</div>,
  Source: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  Layer: () => <i />,
}))
vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('maplibre-gl/dist/maplibre-gl.css', () => ({}))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))
// A live alarm's banner comes first in the dialog and holds a button.
vi.mock('@/components/fullscreen-alarm-banner', () => ({
  FullscreenAlarmBanner: () => <button type="button">Acknowledge</button>,
}))

beforeEach(() => {
  for (const t of ['--primary', '--gauge-primary', '--foreground', '--background', '--muted-foreground']) {
    document.documentElement.style.setProperty(t, '10 20% 30%')
  }
})

describe('the expanded card map with an alarm banner above it', () => {
  it('gives focus to Close, not to the banner, so a repeated Enter cannot acknowledge an alarm', async () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Close' })))
  })
})

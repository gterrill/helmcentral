import { describe, it, expect } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { ProfileCatalogueDialog } from '@/components/inventory/profile-catalogue-dialog'
import { useEquipmentProfiles } from '@/hooks/use-equipment-profiles'
import { stubApi } from '@/test/helpers/stub-api'

function ProfileCount() {
  const { profiles } = useEquipmentProfiles(true)
  return <p data-testid="count">{profiles.length}</p>
}

describe('ProfileCatalogueDialog', () => {
  it('refreshes the profile list when a catalogue entry is added', async () => {
    let profiles: unknown[] = []
    const fetchMock = stubApi({
      'GET /api/equipment-profiles': () => ({ profiles, problems: [] }),
      'GET /api/equipment-profiles/catalogue': {
        entries: [{ source: 'builtin', id: 'vp-d6', name: 'Volvo D6', kind: 'engine', manufacturer: 'Volvo', model: 'D6', sha256: 'x', added: false }],
      },
      'POST /api/equipment-profiles/catalogue/vp-d6': () => {
        profiles = [{ id: 'vp-d6', name: 'Volvo D6' }]
        return {}
      },
    })
    const added: string[] = []

    render(
      <>
        <ProfileCount />
        <ProfileCatalogueDialog open onOpenChange={() => {}} onAdded={(id) => added.push(id)} />
      </>,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Add Volvo D6' }))

    await waitFor(() => expect(screen.getByTestId('count').textContent).toBe('1'))
    expect(added).toEqual(['vp-d6'])
    const listReads = fetchMock.mock.calls.filter(([url]) => String(url).endsWith('/api/equipment-profiles'))
    expect(listReads).toHaveLength(2)
  })
})

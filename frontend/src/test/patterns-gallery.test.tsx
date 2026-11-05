/**
 * The dev-only /patterns gallery renders with no SidebarProvider, because
 * App.tsx returns it ahead of the provider. A gallery child that reads the
 * sidebar context throws there, and nothing else in the suite renders the
 * gallery whole, so this renders it the way App.tsx does, with no mocks.
 */
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PatternsGallery } from '@/components/patterns/gallery'

describe('PatternsGallery', () => {
  it('renders without a SidebarProvider', () => {
    render(<PatternsGallery />)

    expect(screen.getByRole('heading', { name: 'TreeLayout + FolderTree' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'ConfirmDelete' })).toBeInTheDocument()
  })
})

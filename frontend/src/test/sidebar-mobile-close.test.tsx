/**
 * On a phone the sidebar is a sheet over the page. Tapping an entry in it
 * changed the page behind the sheet but left the sheet open, so the operator
 * had to dismiss it by hand to see where they had gone.
 */
import { describe, it, expect } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import {
  Sidebar,
  SidebarContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarTrigger,
} from '@/components/ui/sidebar'
import { setViewportWidth } from './viewport'

function Harness() {
  return (
    <SidebarProvider>
      <Sidebar>
        <SidebarContent>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton>Forecast</SidebarMenuButton>
            </SidebarMenuItem>
            <SidebarMenuSub>
              <SidebarMenuSubItem>
                <SidebarMenuSubButton render={<button type="button" />}>Underway</SidebarMenuSubButton>
              </SidebarMenuSubItem>
            </SidebarMenuSub>
          </SidebarMenu>
        </SidebarContent>
      </Sidebar>
      <SidebarTrigger />
    </SidebarProvider>
  )
}

describe('mobile sidebar', () => {
  it.each(['Forecast', 'Underway'])('closes after tapping %s', async (label) => {
    setViewportWidth(390)
    render(<Harness />)

    fireEvent.click(screen.getByRole('button', { name: /toggle sidebar/i }))
    fireEvent.click(await screen.findByRole('button', { name: label }))

    expect(screen.queryByRole('button', { name: label })).not.toBeInTheDocument()
  })
})

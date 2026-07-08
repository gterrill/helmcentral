import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from 'helmcentral-dashboard'
import { Anchor, LayoutDashboard, Ship } from 'lucide-react'

export function InHeaderBar() {
  return (
    <SidebarProvider style={{ minHeight: 320 }}>
      <Sidebar collapsible="none">
        <SidebarHeader>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 8px' }}>
            <Ship size={18} />
            <span style={{ fontWeight: 600, fontSize: 14 }}>SV Kaitiaki</span>
          </div>
        </SidebarHeader>
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupLabel>Navigation</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <SidebarMenuItem>
                  <SidebarMenuButton isActive>
                    <LayoutDashboard />
                    <span>Dashboard</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
                <SidebarMenuItem>
                  <SidebarMenuButton>
                    <Anchor />
                    <span>Anchor Watch</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
      </Sidebar>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: 12, borderBottom: '1px solid var(--border, #e5e5e5)' }}>
        <SidebarTrigger />
        <span style={{ fontWeight: 600, fontSize: 14 }}>Dashboard</span>
      </div>
    </SidebarProvider>
  )
}

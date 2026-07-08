import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from 'helmcentral-dashboard'
import { Anchor, MoreVertical, Route, Ship } from 'lucide-react'

export function PerItemMenuAction() {
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
            <SidebarGroupLabel>Route Planner</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <SidebarMenuItem>
                  <SidebarMenuButton isActive>
                    <Route />
                    <span>Overnight Passage</span>
                  </SidebarMenuButton>
                  <SidebarMenuAction title="More options">
                    <MoreVertical />
                    <span className="sr-only">More options</span>
                  </SidebarMenuAction>
                </SidebarMenuItem>
                <SidebarMenuItem>
                  <SidebarMenuButton>
                    <Anchor />
                    <span>Marina to Bay</span>
                  </SidebarMenuButton>
                  <SidebarMenuAction title="More options" showOnHover>
                    <MoreVertical />
                    <span className="sr-only">More options</span>
                  </SidebarMenuAction>
                </SidebarMenuItem>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
      </Sidebar>
    </SidebarProvider>
  )
}

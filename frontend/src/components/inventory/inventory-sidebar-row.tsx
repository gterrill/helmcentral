import { memo, type ComponentType } from 'react'

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  useSidebar,
} from '@/components/ui/sidebar'
import { INVENTORY_SECTIONS, type InventorySectionId } from '@/lib/inventory-sections'

interface InventorySidebarRowProps {
  label: string
  icon: ComponentType
  isActive: boolean
  /** The section lit while Inventory is the open panel; null otherwise. */
  activeSection: InventorySectionId | null
  /** The Inventory row: resume Inventory where it was left. */
  onOpen: () => void
  /** A section row or menu item. Goes through the router's guards. */
  onSelectSection: (section: InventorySectionId) => void
}

// ADR 0123: the Inventory row and its sections. Expanded, the sections are a
// flat sub-menu under the row, shown only while Inventory is open. Collapsed to
// icons the sub-menu is hidden, so the row's icon opens the same sections as a
// menu beside it. On a phone the sidebar is a sheet that closes when Inventory
// is tapped, so the sub-menu is always shown there (as Dashboard's pages are)
// and any section opens from the sidebar; only the open section is lit.
// AppShell re-renders on every live delta; memo keeps this row out of it.
export const InventorySidebarRow = memo(function InventorySidebarRow({ label, icon: Icon, isActive, activeSection, onOpen, onSelectSection }: InventorySidebarRowProps) {
  const { state, isMobile } = useSidebar()
  const iconMenu = state === 'collapsed' && !isMobile && activeSection !== null

  if (iconMenu) {
    return (
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={<SidebarMenuButton isActive={isActive} tooltip={label} />}
          >
            <Icon />
            <span>{label}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent side="right" align="start" className="w-44">
            {/* Navigation is the item's click, not the group's value change,
                so choosing the section already open still goes through the
                guarded path, as it always did. */}
            <DropdownMenuRadioGroup value={activeSection}>
              {INVENTORY_SECTIONS.map((section) => (
                <DropdownMenuRadioItem
                  key={section.id}
                  value={section.id}
                  onClick={() => { onSelectSection(section.id) }}
                >
                  <span className="min-w-0 flex-1 truncate">{section.label}</span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    )
  }

  return (
    <>
      <SidebarMenuItem>
        <SidebarMenuButton isActive={isActive} onClick={onOpen} tooltip={label}>
          <Icon />
          <span>{label}</span>
        </SidebarMenuButton>
      </SidebarMenuItem>
      {(activeSection !== null || isMobile) && (
        <SidebarMenuSub>
          {INVENTORY_SECTIONS.map((section) => (
            <SidebarMenuSubItem key={section.id}>
              <SidebarMenuSubButton
                render={<button type="button" />}
                isActive={section.id === activeSection}
                onClick={() => { onSelectSection(section.id) }}
              >
                <span className="min-w-0 flex-1 truncate">{section.label}</span>
              </SidebarMenuSubButton>
            </SidebarMenuSubItem>
          ))}
        </SidebarMenuSub>
      )}
    </>
  )
})

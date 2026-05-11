import { X } from 'lucide-react'
import React from 'react'

interface BottomDrawerProps {
  isOpen: boolean
  onClose: () => void
  title: string
  tabs?: Array<{
    id: string
    label: string
    icon?: React.ReactNode
  }>
  activeTab?: string
  onTabChange?: (tabId: string) => void
  children: React.ReactNode
}

export function BottomDrawer({
  isOpen,
  onClose,
  title,
  tabs,
  activeTab,
  onTabChange,
  children,
}: BottomDrawerProps) {
  if (!isOpen) return null

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 top-0 z-40 bg-black/20 backdrop-blur-sm"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Drawer */}
      <div className="fixed bottom-0 left-0 right-0 z-50 flex max-h-[85vh] flex-col rounded-t-2xl border-t bg-card shadow-xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h2 className="font-display text-lg tracking-[0.24em] text-foreground">{title}</h2>
          <button
            onClick={onClose}
            className="h-8 w-8 rounded-md p-0 hover:bg-muted transition-colors"
            aria-label="Close drawer"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Tabs */}
        {tabs && tabs.length > 0 && (
          <div className="flex gap-1 border-b px-4 py-2">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => onTabChange?.(tab.id)}
                className={`flex items-center gap-2 rounded-t-lg px-4 py-2 text-sm font-semibold transition-colors ${
                  activeTab === tab.id
                    ? 'border-b-2 border-primary bg-background text-primary'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {tab.icon}
                {tab.label}
              </button>
            ))}
          </div>
        )}

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-6 py-4">
          {children}
        </div>
      </div>
    </>
  )
}

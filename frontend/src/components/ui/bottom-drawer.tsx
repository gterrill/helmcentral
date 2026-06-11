import { X } from 'lucide-react'
import React from 'react'

interface BottomDrawerProps {
  isOpen: boolean
  onOpen?: () => void
  onClose: () => void
  title: string
  tabs?: Array<{
    id: string
    label: string
    icon?: React.ReactNode
    indicator?: boolean
  }>
  activeTab?: string
  onTabChange?: (tabId: string) => void
  children: React.ReactNode
}

export function BottomDrawer({
  isOpen,
  onOpen,
  onClose,
  title,
  tabs,
  activeTab,
  onTabChange,
  children,
}: BottomDrawerProps) {
  const hasTabs = Boolean(tabs && tabs.length > 0)

  const handleTabSelect = (tabId: string) => {
    onTabChange?.(tabId)
    onOpen?.()
  }

  if (!isOpen) {
    if (!hasTabs) return null

    return (
      <div className="fixed bottom-0 left-0 right-0 z-[3000] border-t bg-card/95 shadow-[0_-6px_20px_rgba(0,0,0,0.08)] backdrop-blur-sm">
        <div className="mx-auto flex w-full max-w-[1800px] items-center gap-1 px-3 py-2 md:px-5">
          {tabs!.map((tab) => (
            <button
              key={tab.id}
              onClick={() => handleTabSelect(tab.id)}
              className={`min-h-10 min-w-10 flex-1 rounded-md px-3 py-2 text-sm font-semibold transition-colors ${
                activeTab === tab.id
                  ? 'bg-background text-primary'
                  : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
              }`}
            >
              <span className="inline-flex items-center gap-2">
                {tab.icon}
                {tab.label}
                {tab.indicator && (
                  <span className="h-1.5 w-1.5 rounded-full bg-amber-400" aria-hidden="true" />
                )}
              </span>
            </button>
          ))}
        </div>
      </div>
    )
  }

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 top-0 z-[3100] bg-black/20 backdrop-blur-sm"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Drawer */}
      <div className="fixed bottom-0 left-0 right-0 z-[3200] flex h-[85vh] flex-col rounded-t-2xl border-t bg-card shadow-xl">
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
        {hasTabs && (
          <div className="flex gap-1 border-b px-4 py-2">
            {tabs!.map((tab) => (
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
        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
          {children}
        </div>
      </div>
    </>
  )
}

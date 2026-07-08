import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from 'helmcentral-dashboard'
import { Anchor, Compass, LifeBuoy } from 'lucide-react'

export function Default() {
  return (
    <TooltipProvider>
      <Tooltip defaultOpen defaultTriggerId="tooltip-default-trigger">
        <TooltipTrigger
          id="tooltip-default-trigger"
          className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background text-foreground shadow-sm"
        >
          <Anchor className="h-4 w-4" />
        </TooltipTrigger>
        <TooltipContent side="top">Drop anchor</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

export function InToolbar() {
  return (
    <TooltipProvider>
      <div className="flex items-center gap-2 rounded-md border bg-background p-2 shadow-sm">
        <Tooltip defaultTriggerId="tooltip-toolbar-compass">
          <TooltipTrigger
            id="tooltip-toolbar-compass"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md hover:bg-muted"
          >
            <Compass className="h-4 w-4" />
          </TooltipTrigger>
          <TooltipContent side="top">Set course</TooltipContent>
        </Tooltip>
        <Tooltip defaultOpen defaultTriggerId="tooltip-toolbar-lifebuoy">
          <TooltipTrigger
            id="tooltip-toolbar-lifebuoy"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md hover:bg-muted"
          >
            <LifeBuoy className="h-4 w-4" />
          </TooltipTrigger>
          <TooltipContent side="top">Man overboard</TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  )
}

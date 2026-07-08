import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from 'helmcentral-dashboard'
import { Anchor } from 'lucide-react'

export function Closed() {
  return (
    <TooltipProvider>
      <Tooltip defaultTriggerId="tooltip-trigger-closed">
        <TooltipTrigger
          id="tooltip-trigger-closed"
          className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background text-foreground shadow-sm"
        >
          <Anchor className="h-4 w-4" />
        </TooltipTrigger>
        <TooltipContent side="top">Drop anchor</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

export function Open() {
  return (
    <TooltipProvider>
      <Tooltip defaultOpen defaultTriggerId="tooltip-trigger-open">
        <TooltipTrigger
          id="tooltip-trigger-open"
          className="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm shadow-sm"
        >
          Route
        </TooltipTrigger>
        <TooltipContent side="top">4.2nm to Marina · ETA 38 min</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

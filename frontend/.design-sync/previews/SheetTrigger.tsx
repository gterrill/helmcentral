import {
  Button,
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from 'helmcentral-dashboard'

export function Closed() {
  return (
    <Sheet>
      <SheetTrigger render={<Button variant="outline">Open route planner</Button>} />
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Route Planner</SheetTitle>
          <SheetDescription>Plan a route between waypoints.</SheetDescription>
        </SheetHeader>
        <SheetFooter>
          <SheetClose render={<Button variant="outline">Close</Button>} />
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

export function IconTrigger() {
  return (
    <Sheet>
      <SheetTrigger className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background shadow-sm">
        ⚙
      </SheetTrigger>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Settings</SheetTitle>
          <SheetDescription>Vessel configuration.</SheetDescription>
        </SheetHeader>
      </SheetContent>
    </Sheet>
  )
}

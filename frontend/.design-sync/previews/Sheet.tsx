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

export function Default() {
  return (
    <Sheet defaultOpen>
      <SheetTrigger render={<Button variant="outline">Open anchor settings</Button>} />
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Anchor Watch Settings</SheetTitle>
          <SheetDescription>Adjust drift radius and alarm thresholds.</SheetDescription>
        </SheetHeader>
        <div style={{ padding: '16px 0' }}>
          <p>Current radius: 12m · Alarm at 25m</p>
        </div>
        <SheetFooter>
          <SheetClose render={<Button variant="outline">Cancel</Button>} />
          <Button>Save changes</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

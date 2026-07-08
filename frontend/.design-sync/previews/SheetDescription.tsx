import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Sheet defaultOpen>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Anchor Watch Settings</SheetTitle>
          <SheetDescription>Adjust drift radius and alarm thresholds for this anchorage.</SheetDescription>
        </SheetHeader>
      </SheetContent>
    </Sheet>
  )
}

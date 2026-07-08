import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Sheet defaultOpen>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Tank Levels</SheetTitle>
          <SheetDescription>Fresh water 62% · Waste 18% · Fuel 74%</SheetDescription>
        </SheetHeader>
      </SheetContent>
    </Sheet>
  )
}

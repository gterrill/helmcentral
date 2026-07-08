import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Sheet defaultOpen>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Batteries</SheetTitle>
          <SheetDescription>House bank 86% · Starter 12.6V</SheetDescription>
        </SheetHeader>
      </SheetContent>
    </Sheet>
  )
}

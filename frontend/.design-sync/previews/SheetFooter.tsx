import {
  Button,
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from 'helmcentral-dashboard'

export function Default() {
  return (
    <Sheet defaultOpen>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Delete route</SheetTitle>
          <SheetDescription>This route to Marina Cove will be permanently removed.</SheetDescription>
        </SheetHeader>
        <SheetFooter>
          <SheetClose render={<Button variant="outline">Cancel</Button>} />
          <Button className="bg-destructive text-destructive-foreground hover:bg-destructive/90">Delete route</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

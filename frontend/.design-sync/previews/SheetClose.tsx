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
          <SheetTitle>Discard route?</SheetTitle>
          <SheetDescription>Unsaved waypoints will be lost.</SheetDescription>
        </SheetHeader>
        <SheetFooter>
          <SheetClose render={<Button variant="outline">Keep editing</Button>} />
          <SheetClose
            render={
              <Button className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
                Discard
              </Button>
            }
          />
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

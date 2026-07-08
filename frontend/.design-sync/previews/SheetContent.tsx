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

export function RightSide() {
  return (
    <Sheet defaultOpen>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Route to Marina</SheetTitle>
          <SheetDescription>4.2nm · ETA 38 min</SheetDescription>
        </SheetHeader>
        <div style={{ padding: '16px 0' }}>
          <p>Wind 12kts SW · Current favorable</p>
        </div>
        <SheetFooter>
          <SheetClose render={<Button variant="outline">Close</Button>} />
          <Button>Start navigation</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

export function BottomSide() {
  return (
    <Sheet defaultOpen>
      <SheetContent side="bottom">
        <SheetHeader>
          <SheetTitle>Generator</SheetTitle>
          <SheetDescription>Running · 4.2kW load</SheetDescription>
        </SheetHeader>
        <SheetFooter>
          <SheetClose render={<Button variant="outline">Close</Button>} />
          <Button className="bg-destructive text-destructive-foreground hover:bg-destructive/90">Stop generator</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

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

// SheetPortal is a structural wrapper (Base UI's Dialog.Portal) with no
// visual output of its own — it just teleports SheetContent/SheetOverlay to
// document.body. SheetContent already renders through it internally, so the
// only faithful way to preview it is to exercise a full open Sheet, same as
// the `Sheet` story. See .design-sync/learnings/batch3.md.
export function Default() {
  return (
    <Sheet defaultOpen>
      <SheetTrigger render={<Button variant="outline">Open tide details</Button>} />
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Tide Details</SheetTitle>
          <SheetDescription>Portal-rendered panel, teleported to document body.</SheetDescription>
        </SheetHeader>
        <SheetFooter>
          <SheetClose render={<Button variant="outline">Close</Button>} />
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

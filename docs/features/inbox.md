# Inbox

The Inbox is where photos and PDFs from your phone wait until you've looked
through them. You share a whole camera roll in one go and throw away the shots
you don't want. Later, on the laptop or tablet where it's easier to see what
you're doing, you put the rest on a passage or into Documents.

- Share from the phone: Pick as many photos and PDFs as you like on the upload
  page. Each photo is shrunk on the phone first, so a roll of photos doesn't
  choke the boat's connection.
- Nothing is filed for you: A photo stays in the Inbox until you put it
  somewhere or delete it. Helmcentral only makes suggestions.
- Photos find their passage by themselves: Each photo is matched to the passage
  it was taken on, using the time it was taken and the boat's own track.
- Not part of Documents yet: A photo you're about to delete is never read,
  searched or counted. It becomes a document only when you send it there.

## Sharing from your phone

Open **Passages**, tap **Inbox**, then **Share photos**. The same page is at
`/passages/upload`, so you can add that address to your phone's Home Screen and
open it like an app. Tap **Choose photos and files** and pick as many as you
want. Photos and PDFs are accepted, and nothing else.

Helmcentral sends them one at a time and keeps count while it works. When it's
done it tells you how many arrived and how many it left out. It reads like this:
"12 uploaded, 3 already in Helmcentral". A photo is left out in three cases.
The Inbox already holds it. You've already put it on a passage or sent it to
Documents. Or Documents holds exactly the same file. Helmcentral recognises
the photo itself, not the shrunk copy, so picking the same photos twice does no
harm. A photo you deleted from the Inbox is new again if you share it again.

Here is what happens to each photo before it leaves the phone:

- The time it was taken is read first, along with the direction the phone was
  facing and where the phone says it was, if the phone shares that.
- The photo is shrunk to 2560 pixels on its longest side. A photo in the iPhone's
  HEIC format is turned into an ordinary JPEG at the same time.
- Helmcentral works out how sharp the photo is and a short fingerprint of what's
  in it. These are what let the Inbox spot a burst of near-identical shots and a
  blurred one.

PDFs are sent as they are.

Android phones often hide a photo's location when you pick it in a browser. The
photo then says nothing about where it was taken or which way the phone faced,
and Helmcentral treats that as missing. It never reads it as a position at the
very top-left of the map or a bearing of due north. The time it was taken is
still there.

If a photo has no time zone of its own, Helmcentral reads its time in the boat's
zone. If it doesn't know the boat's zone either, the photo goes up with no time
and the upload page tells you, rather than guessing.

## The Inbox

**Passages** shows an **Inbox** button with a number on it while anything is
waiting. The Inbox lays the photos out as a contact sheet, in the order they were
taken, in groups:

- Each passage: Photos taken during a passage, headed by that passage's name and
  date. The newest passage is first.
- Underway now: Photos taken after the last recorded passage ended while the boat
  was still moving. A passage is only recorded once it ends, so these can't be
  put on one yet. You can still delete them, and they join their passage once
  it's recorded.
- Not on a passage: Photos taken while the boat was stopped, between passages.
- No time: Files that carried no time at all, such as most PDFs.

Tap a picture to mark it, and tap again to unmark it. **Mark all** does a whole
group. Whatever is marked can then be sent on, using the buttons above the
pictures. Each picture also shows the time it was taken, in the boat's time, and
a few small marks:

- A compass: The phone gave the direction it was facing. Its tooltip gives the
  bearing.
- A warning triangle: The phone said it was more than a nautical mile from where
  the boat's track was at that time. That nearly always means the phone's clock
  or time zone is wrong, so the photo may sit in the wrong place. Check the
  phone's date and time settings.
- **Duplicate**, **Best of 3**: These are shots that look alike and were taken
  within a minute of each other, such as a burst. The sharpest one in each set is
  marked **Best of** and the others **Duplicate**.
- **Soft**: A photo much blurrier than the rest of the batch. A plain stretch of
  sea and sky can score low honestly, so look before you delete.

**Mark suggested deletions** marks every duplicate that isn't the best of its
set and every soft photo. It only marks them. Nothing is deleted until you
confirm.

## Putting photos on a passage

Mark the photos and tap **Add to passage**. A list opens with one row for each
photo, and each row needs a title of up to 80 characters. Helmcentral fills in
the title where it can, with the nearest named place to the boat at the moment
the photo was taken, such as a bay or an island. It leaves the title empty when
there's no name to give, or when the place lookup is slow and runs out of time
after a few seconds. You can change any of them before you confirm.

When you confirm, each photo becomes a sign on its passage at the moment it was
taken, with a photo moment, so the replay stops and shows it full frame. The
photo is kept in the **Passage photos** folder in Documents, the same one the
sign editor uses, and the photo leaves the Inbox. Signs are described under
[Passages](passages.md).

- Photos not on a passage: The row asks where it goes. The nearest passage start
  or end is chosen for you, and you can pick any other from the list.
- Photos with no time: You have to choose a passage start or end by hand.
- Photos taken underway: The button stays off until the passage is recorded.
- PDFs: Only a photo can be a sign, so the button stays off if a PDF is marked.
- One photo at a time: If a photo can't be added, it stays in the Inbox with the
  reason beside it. An example is a time that isn't on any passage. The rest
  still go through.

## Sending to Documents

Mark the photos or PDFs and tap **Send to Documents**. Choose a folder and tap
**Send here**. The picker opens in the folder you sent to last time, on that
device, and you can make a new folder from it. The files become documents in that
folder and Mate reads them the way it reads any other upload. There's no
standard folder for receipts, so you choose.

## Deleting

**Delete** asks you to confirm and then removes the marked items from the boat
for good. Your phone still has the originals.

## Where it stops

- The Inbox keeps items until you act on them. Nothing expires.
- Helmcentral doesn't make thumbnails, so a long contact sheet loads slowly over
  a weak link.
- Anyone who can make changes can share, and an item doesn't say who sent it.
- Photos can't yet be sent to a piece of equipment or an open job from the Inbox.
- Sharing straight from the phone's share menu isn't there yet. Use the upload
  page.
- The Inbox is stored in `data/inbox` beside the document library. The
  [configuration reference](../reference/configuration.md) lists the setting that
  moves it. Include that folder in your backups.

The notes on **Documents** with no folder are called **Unfiled notes**, not the
Inbox, so the word Inbox means only this page. See
[Notes and the manual](notes-and-the-manual.md).

# Inbox

The Inbox is where photos and PDFs from your phone wait until you've looked
through them. You share a whole camera roll in one go and throw away the shots
you don't want. Later, on the laptop or tablet where it's easier to see what
you're doing, you put the rest on a passage, on an equipment item or a job, or
into Documents.

- Share from the phone: Pick as many photos and PDFs as you like on the upload
  page. Each photo is shrunk on the phone first, so a roll of photos doesn't
  choke the boat's connection.
- Nothing is filed for you: A photo stays in the Inbox until you put it
  somewhere or delete it. Helmcentral only makes suggestions.
- Photos find their passage by themselves: Each photo is matched to the passage
  it was taken on, using the time it was taken and the boat's own track.
- Not part of Documents yet: A photo you're about to delete is never read,
  searched or counted. It becomes a document only when you send it there.
- Mate can sort and title them: If Mate is set up, it looks at each photo as it
  arrives and suggests where it belongs, and it can write the title for a photo
  going on a passage. If Mate isn't set up, none of that happens and the Inbox
  works as described everywhere else on this page. See
  [Mate in the Inbox](#mate-in-the-inbox).

## Sharing from your phone

Open **Inbox** in the sidebar, then tap **Share photos**. The same page is at
`/inbox/upload`, so you can add that address to your phone's Home Screen and open
it like an app. On an Android phone you can also share straight from Photos or
Files, as described next. Tap **Choose photos and files** and pick as many as you
want. Photos and PDFs are accepted, and nothing else.

Helmcentral first checks your photos against what the boat already has, which
shows as "Checking" and takes a little while on a big roll. Then it sends the
rest one at a time and keeps count while it works. When it's done it tells you
how many arrived and how many it left out. It reads like this: "12 uploaded, 3
already in Helmcentral". A photo is left out in three cases. The Inbox already
holds it. You've already put it on a passage, sent it to Documents or added it
to equipment or a job, and its photo is still in Documents. Or Documents holds exactly
the same file. Helmcentral recognises the photo itself,
not the shrunk copy, so picking the same photos twice does no harm. A photo the
Inbox holds, or that you've already filed from it, is skipped before it's
shrunk, so a second go at the same roll is quick. A file Documents holds is
turned away by the boat after it arrives, so it costs the shrinking and the
send. A photo you deleted from the Inbox is new again if you share it again, and so is
one whose filed copy you later deleted from Documents. Deleting a sign or taking a
photo off an item doesn't count: the photo stays in Documents until you delete it
there.

If the boat can't be reached for that check, nothing is sent and the page tells
you why. Try again once the connection is back.

Here is what happens to each photo before it leaves the phone:

- The time it was taken is read first, along with the direction the phone was
  facing and where the phone says it was, if the phone shares that.
- The photo is shrunk to 2560 pixels on its longest side. A photo in the iPhone's
  HEIC format is turned into an ordinary JPEG at the same time.
- A small preview of about 400 pixels is made and sent with it. The Inbox shows
  these, so it loads quickly even over a weak link.
- Helmcentral works out how sharp the photo is and a short fingerprint of what's
  in it. These are what let the Inbox spot a burst of near-identical shots and a
  blurred one.

PDFs are sent as they are, with no preview. In the Inbox they show a file icon
and their name.

Android phones often hide a photo's location when you pick it in a browser. The
photo then says nothing about where it was taken or which way the phone faced,
and Helmcentral treats that as missing. It never reads it as a position at the
very top-left of the map or a bearing of due north. The time it was taken is
still there.

If a photo has no time zone of its own, Helmcentral reads its time in the boat's
zone. If it doesn't know the boat's zone either, the photo goes up with no time
and the upload page tells you, rather than guessing.

## Sharing from the share sheet

On an Android phone with Helmcentral installed as an app, **Helmcentral** appears
in the share sheet. Select photos or PDFs, tap **Share**, then **Helmcentral**.
Setting it up is in [Share photos from an Android
phone](../how-to/share-photos-from-an-android-phone.md). An iPhone has no share
sheet entry for web apps, so it uses the upload page.

Files shared from the share sheet go on a waiting list on the phone before
anything is sent. That is what lets an interrupted share carry on. Files you
pick on the upload page upload straight away and don't use the list: to carry
on after an interruption, pick them again, and the boat skips what it already
has.

- Leaving the list: A file comes off only when the boat has stored it or already
  had it. If you remove it yourself, it comes off too.
- After a share: Helmcentral opens on the upload page with the files listed and an
  **Upload** button, such as "Upload 7 photos". Nothing is sent until you tap it.
  The button, and **Choose photos and files**, wait up to about ten seconds with
  "Getting the boat's time zone…", then turn on whether or not the zone arrived.
- A file that fails: It stays on the list with the reason, and **Upload** tries it
  again.
- Next time you open the installed app: A banner says how many are waiting, such
  as "7 photos waiting to upload", with **Upload now**, which opens the upload
  page, and **Discard**, which is off while an upload runs. A browser tab or a
  wall display doesn't show the banner. If the phone can't read its waiting list,
  the banner says so.
- One upload at a time: Two open windows on the same phone don't both upload. The
  second one tells you the first is already working, and anything shared in the
  meantime waits for the next upload. If you leave the upload page while it runs
  and come back, the page shows the upload still going. When a run ends and more
  files are waiting, the page says how many are "more waiting" and offers
  **Upload** again.

Helmcentral doesn't upload in the background. The upload runs while the app is
open, so keep it open until the count reaches the end.

A shared photo goes through the same checks as a picked one: the time, the
direction the phone faced and where the phone says it was are read before the
photo is shrunk. A share may keep a location that picking in the browser hides,
so the compass mark on an item tells you whether the direction arrived.

## The Inbox

The **Inbox** row in the sidebar shows a number on it while anything is waiting.
The Inbox lays the photos out as a contact sheet, in the order they were
taken, in groups:

- Each passage: Photos taken during a passage, headed by that passage's name and
  date. The newest passage is first.
- Underway now: Photos taken after the last recorded passage ended while the boat
  was still moving. A passage is only recorded once it ends, so these can't be
  put on one yet. You can still delete them, and they join their passage once
  it's recorded.
- Not on a passage: Photos taken while the boat was stopped, between passages.
- No time: Files that carried no time at all, such as most PDFs.

When Mate is set up, three more groups appear, and a photo goes to the one Mate
suggests whatever its time:

- Documents: Receipts, paperwork and PDFs. Mate sorts a PDF here without looking.
- Equipment: Close pictures of gear on the boat, such as an engine, a pump or a
  label.
- Unsorted: Photos Mate hasn't looked at yet, or couldn't sort, that no passage
  claims by time. Each one says which it is.

The group sets the starting suggestion and nothing more. Every button still works
on every group, so you can add a photo from Documents to a passage, or send an
Unsorted photo wherever it belongs. Photos Mate calls scenery stay where their
time puts them.

Tap a picture to mark it, and tap again to unmark it. **Mark all** does a whole
group. Whatever is marked can then be sent on, using the buttons above the
pictures. Each picture also shows the time it was taken, in the boat's time, and
a few small marks:

- A compass: The phone gave the direction it was facing. Its tooltip gives the
  bearing. Helmcentral uses that bearing to find what the photo was pointing at,
  as described under [Putting photos on a passage](#putting-photos-on-a-passage).
- A warning triangle: The phone said it was more than a nautical mile from where
  the boat's track was at that time, allowing for how far the boat could have
  gone since the last recorded position. That nearly always means the phone's clock
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
the title where it can. You can change any of them before you confirm. It leaves
a title empty when there's no name to give, or when the lookup is slow and runs
out of time. What it fills in depends on the photo and on whether Mate is set up:

- A photo with no direction, without Mate: The nearest named place to the boat at
  the moment the photo was taken, such as a bay or an island.
- A photo that carries a direction, without Mate: The nearest named island or bay
  in the way the phone faced. That means within 25 degrees either side, and out to
  10 nautical miles from where the boat was. If there's nothing that way, it uses
  the nearest named place instead.
- With Mate: Mate writes the title. See [Mate in the Inbox](#mate-in-the-inbox).

The direction on a phone photo is usually measured from true north. Some phones
record it from magnetic north instead. Helmcentral doesn't know your magnetic
variation, so it reads that direction as if it were true. The error is a few
degrees, which sits well inside the 25 degrees either side.

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

## Mate in the Inbox

Mate helps in two places, and only when it's set up under Settings, Assistant,
with an OpenRouter key and a document model. If it isn't set up, the Inbox makes
no calls at all and the groups are the ones described above.

- Sorting what arrives: As each photo is stored, Mate looks at its small preview
  and says whether it's scenery, a document or equipment. This happens a moment
  after the upload finishes, so sharing a roll never waits on it, and a few
  photos at a time are sent. A PDF is a document without being sent anywhere.
  The Inbox updates by itself while Mate works through what's waiting. It checks
  again after a few seconds, then less often, and stops after about ten minutes
  if nothing has changed. Reopen the Inbox to check again.
- Writing titles: When you tap **Add to passage**, Mate looks at each photo. If
  there's a named island or bay in range, it looks at the photo at its stored
  size, up to 2560 pixels, so it can tell whether the place is in the picture.
  If there's none, the small preview is enough to say what the photo shows. It's
  told the boat's course over ground when the photo was taken. It's also given the
  five named islands and bays, each with its distance and which side of
  the course it lies on, such as "Cape Capricorn, 3.1 nm, 40° to port". They're
  the nearest ones. If the photo carried a direction, the ones within 25 degrees
  of it are marked, and the two nearest of those are always among the five, even
  when other places are closer, so the one the camera faced is never left out. Mate
  uses a place's name only if the photo shows it. Otherwise it names what's in
  the picture, such as "Dolphins off the bow". It writes one title of up to 80
  characters and no note. A title it has written is kept with the photo, even
  if Helmcentral restarts, so opening **Add to passage** again for the same photo
  doesn't ask Mate a second time. Mate writes titles for up to 20 photos at a
  time. If you've marked more, the rest say that Mate titles up to 20 photos at a
  time, and opening the list again does the next batch.
- The boat's course: This is the course over ground the boat was making at the
  time, which is the way it was moving and not necessarily the way the bow
  pointed. When the boat was barely moving, a course means nothing, so Mate is
  told the course isn't known and gets true bearings instead.

Mate sends each photo to the document model you chose, so every photo costs a
little. The preview goes when it's sorted, and a picture goes again if you ask
for a title. Mate never has more than three photos with the provider at once,
however many you've asked about. Photos leave the boat to OpenRouter only when
Mate is set up, and switching Mate off stops it. Saving a change in Settings,
Assistant takes effect at once. Photos that arrived while Mate was off are sorted
as soon as you turn it on, or when you open or refresh the Inbox with Mate on, or
when Helmcentral restarts with Mate on. You don't have to share another photo to
get them started.

If Mate can't do something, the Inbox says so and doesn't quietly do something else:

- A photo Mate couldn't sort: Whatever the reason, whether the network dropped,
  the provider is busy, the account is out of credit, the key is wrong, Mate's
  answer wasn't one of the three kinds or the photo has no small preview, the
  photo waits in **Unsorted** with one plain line, such as "Mate couldn't sort
  this yet. Trying again at 3.40 PM". The time is on the boat's clock. Mate tries
  again by itself after 1 minute, then 5, then 30, then every hour, until it
  works. A call that ran out of time isn't asked again straight away. It waits
  for its next turn like any other failure. Once the time has passed, the line
  says "Trying again shortly". If Mate is off, it says "Mate couldn't sort this.
  Mate is off", because no try is coming. The cause isn't on the screen, so if it
  lasts, look in the Helmcentral log. A photo with no small preview isn't sent
  full size. While photos wait for a try, the Inbox looks again about once a
  minute, and just after a try is due.
- Sort again: When some photos have failed, a **Sort again** button appears above
  the pictures. It clears every failure and wait, and has Mate look at those
  photos again straight away. Changing the assistant switch, the document model or
  the OpenRouter key in Settings does the same, so fixing the key or topping up
  the account needs nothing more from you. Saving anything else in Settings
  leaves the waits alone.
- Mate's settings couldn't be read: The Inbox says so in one line, and that sorting
  is paused. It groups the photos by time, as if Mate were off, so you can still
  sort and file. Helmcentral tries again after about a minute
  without waiting for the next photo. If you tap **Add to passage** then, the
  titles are the place names you'd get without Mate, and the dialog says that
  Mate's settings couldn't be read and that these are place-name titles.
- A title Mate couldn't write: The row stays empty with the reason beside it, such
  as "Mate couldn't reach the model, so no title was suggested." It isn't replaced
  with a place name, so you know it needs you.
- Nearby places that couldn't be fetched: The row says "The place-name server
  didn't answer, so no title was suggested.", instead of going on without them.
  The same goes when the place-name server hangs, which Mate's title waits about
  8 seconds for. It isn't blamed on Mate.
- A slow answer: Each photo gets about 25 seconds for Mate, counted from when its
  turn with the provider starts, and about 8 seconds for a place name. A photo
  that runs out of time on Mate is left empty with "Mate didn't answer in time" beside it,
  and the others aren't held up by it. A whole request has about 90 seconds. Any
  photo that hasn't started by then gets the same message and isn't sent.
- Too many places nearby: In a crowded archipelago the list of named places can be
  cut short, so the nearest one may be missing. Without Mate, a photo with a
  direction then shows "too many named places nearby to pick one reliably" and is
  left empty, because picking from a cut-short list could name the wrong place.
  With Mate, it's told the list may be incomplete.

Mate sees only the nearest named islands and bays. A headland or a point shows up
only where it's mapped as one of those.

## Sending to Documents

Mark the photos or PDFs and tap **Send to Documents**. Choose a folder and tap
**Send here**. The picker opens in the folder you sent to last time, on that
device, and you can make a new folder from it. The files become documents in that
folder and Mate reads them the way it reads any other upload. There's no
standard folder for receipts, so you choose.

## Putting photos on equipment or a job

Mark the photos and tap **Add to equipment** or **Add to a job**. A list opens
with a search box. Type words from what the list shows: for equipment, part of a
name, a maker, a model or the zone it lives in, and for a job, part of its title or
the equipment it's about. Each word has to match somewhere, in any order, so "volvo
d2-40" and "engine room" both work. Then tap the one you mean and tap **Add
here**. Equipment is searched the way the Equipment screen searches it. Helmcentral
never picks for you.

- On equipment: The photos join the item's own photos, at the end, and show on its
  page like any photo you added there. Stored items are in the list too.
- On a job: The list shows open jobs only, because a closed job takes no more
  photos. The photos join the job's photos.
- The last one you used: The list opens with the item or job you chose last time,
  on that device, already ticked. If it has been deleted or closed since, nothing
  is ticked.
- PDFs and other files: Only JPEG or PNG photos can go on equipment or a job. The
  buttons stay off if nothing marked is one. If you marked photos and other files
  together, the photos go and Helmcentral tells you how many files stayed. They
  stay marked, so **Send to Documents** is one tap away.
- One photo at a time: If one can't be added, it stays in the Inbox with the
  reason beside it, and the rest still go through. An example is an item that was
  deleted while the list was open.

Each photo leaves the Inbox once it's on the item or job. It's also kept in
Documents, as every equipment photo is.

## Deleting

**Delete** asks you to confirm and then removes the marked items from the boat
for good. Your phone still has the originals. An item that is being filed at
that moment is left alone, and Helmcentral says how many it left: "2 deleted, 1
is being filed".

## Where it stops

- The Inbox keeps items until you act on them. Nothing expires.
- The previews are only made on the phone, so a photo that reaches the Inbox some
  other way has none.
- Anyone who can make changes can share, and an item doesn't say who sent it.
- Helmcentral doesn't guess which equipment item or job a photo is of. You pick it.
  Mate says only that a photo looks like equipment.
- Mate can be wrong about what a photo is, or what it shows. It's a suggestion and
  you read every title before you confirm.
- Sharing from the share sheet is for Android with the installed app only. An
  iPhone uses the upload page.
- The upload doesn't carry on in the background. Close the app mid-way and the
  rest waits for the next time you open it.
- The Inbox is stored in `data/inbox` beside the document library. The
  [configuration reference](../reference/configuration.md) lists the setting that
  moves it. Include that folder in your backups.

The notes on **Documents** with no folder are called **Unfiled notes**, not the
Inbox, so the word Inbox means only this page. See
[Notes and the manual](notes-and-the-manual.md).

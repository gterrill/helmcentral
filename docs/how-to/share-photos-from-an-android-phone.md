# Share photos from an Android phone

The [Inbox](../features/inbox.md) takes photos and PDFs from your phone. On an
Android phone with Chrome you can send them from the phone's own share sheet, so
you never have to open Helmcentral first. It works only when Helmcentral is
installed on the phone as an app, and that needs the secure address. A plain
`http://` address on the boat's network can't be installed. Read [Web push over
Tailscale](../reference/configuration.md#web-push-over-tailscale) if you haven't
set that address up yet.

## Install Helmcentral as an app

1. On the phone, open Helmcentral in Chrome at your secure Tailscale address,
   the one that starts with `https://`.
2. Open Chrome's menu and choose **Install app**, or **Add to Home screen** and
   then **Install**.
3. Open Helmcentral from its new icon on the Home Screen once. This starts the
   part of the app that catches your shares.

If you installed Helmcentral as an app before this was added, **Helmcentral**
can take up to a day to appear in the share sheet. Chrome only refreshes an
installed app's details now and then. To get it straight away, remove the app
from the Home Screen and install it again from Chrome's menu.

## Share photos to Helmcentral

1. In Photos or Files, select the photos or PDFs you want to send.
2. Tap **Share**, then **Helmcentral**.
3. Helmcentral opens on its upload page with your photos listed. Tap **Upload**
   and the photos go. Each is checked against what the boat already has, shrunk
   on the phone, and sent one at a time with a running count.

Nothing is sent until you tap **Upload**. The button waits a few seconds, saying
"Getting the boat's time zone…", so each photo keeps the time it was taken. If the
boat's time zone doesn't arrive in about ten seconds, the button turns on anyway,
and a photo that carries no zone of its own goes up with no time.

Everything you share goes on a waiting list on the phone first. A file comes off
the list only when the boat has stored it or already had it. If the connection
drops, or you close the app, the rest stays on the list. If you share more while
an upload is running, the page says how many are "more waiting" when it ends, and
**Upload** is there again. The next time you open the installed app on that phone,
a banner says how many are waiting, such as "7 photos waiting to upload". Choose
**Upload now** to open the upload page, then tap **Upload**, or **Discard** to
throw the waiting files away. Discarding doesn't touch the originals in your
phone's photos, and it is off while an upload is running.

A file that the boat refused stays on the list with the reason beside it. The
upload page lists the waiting files, and the cross beside one removes just that
file.

Photos you pick on the upload page instead don't go on the list. They upload
straight away, so to carry on after an interruption, pick them again. The boat
skips what it already has.

## If something doesn't work

- **Helmcentral isn't in the share sheet:** It has to be the installed app, not a
  Chrome tab, and the app has to have been opened once from its icon. If you
  installed it a while ago, remove it and install it again, as above.
- **A message says the files could not be kept:** The phone had no room, or
  Chrome blocked storage for the app. Free some space and share again, or choose
  the files on the upload page.
- **A message says the files did not reach the app:** The share went to the boat
  without the app catching it. Open Helmcentral from its Home Screen icon, then
  share again.
- **An iPhone:** The iPhone doesn't offer web apps in its share sheet. Open the
  upload page at `/inbox/upload`, add it to the Home Screen, and choose the
  photos there.
- **Where location goes:** A shared photo may keep its location and the direction
  the phone faced where a photo picked on the page loses them. An item in the
  Inbox shows a small compass when the phone gave a direction.

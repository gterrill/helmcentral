# Jobs

Jobs is the list of what needs doing on the boat. You find it in the sidebar,
between **Documents** and **Inventory**. It puts the problems you've found and
haven't fixed yet next to the services that have come due. When you walk the
boat you're asking "what needs doing", not "is this a rule or a job".

## What's on the page

- **Open jobs:** The problems you've written down and not fixed yet, oldest
  first. Each row shows the item it's on, how many days it has been open, and
  the **Before** date if you gave one. **Done**, **Drop** and **Show closed**
  work as they do on an item's page. See
  [Keep track of an open job](../how-to/keep-track-of-an-open-job.md).
- **Services due:** The rules that are overdue or due soon, overdue first. Each
  row has the same buttons as the Service schedule: **Complete**, **Ack** and,
  for gear with no hour meter, **Log a reading**. A service you've
  acknowledged stays on the list with its **Ack** mark, below the ones nobody
  has looked at yet.
- **Service schedule:** The link above the services opens **Inventory →
  Service schedule**, where every rule is listed, including the ones that
  aren't due.

**Add job** at the top of the page starts a new job. If you can only read the
boat's records, you see the page without the buttons.

## The count on the sidebar row

The **Jobs** row shows a small number: the open jobs plus the services that are
overdue or due soon. It's the count of things you can act on, so a service you've
acknowledged isn't in it, because **Ack** means you've decided it can wait.
Items in storage aren't counted either. The number disappears when it reaches
zero. Helmcentral reads it again every few minutes and whenever you do something
here.

- **An amber "!":** If the row shows an amber **!** in place of a number, the
  count can't be trusted. Helmcentral couldn't read the schedule of one of your
  items, because its profile is missing or invalid, so it never checked that
  item's services. Open **Jobs**. The alert at the top names the item. Open it
  and fix or pick its profile, and the number comes back.
- **No badge:** If the row shows nothing even though you expect jobs, the count
  couldn't be read just now. Helmcentral doesn't show an old number in its place.
  It tries again by itself.

## When there's nothing to do

A boat with no open jobs and nothing due shows "Nothing needs doing. Open jobs
and services coming due show here." with **Add job** underneath.

## What this isn't

- **Not an alarm:** Nothing on this page raises a notification, and the count
  isn't an alarm count. It's a list you read when you want to know what's next.
- **Not a task board:** A job has no assignee, no comments and no priority.
- **Not the whole schedule:** A service that isn't due yet isn't here. Look in
  **Inventory → Service schedule** for those. See [Maintenance](maintenance.md).

Mate can read both halves of this page and can add an open job when you ask. It
can't close or drop one. See [Maintenance](maintenance.md).

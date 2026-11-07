# Manage the places you've given Mate

Use this to correct, rename or remove a place that Mate saved for you. See
[Places you give Mate](../features/place-names.md#places-you-give-mate) for how
a place gets saved in the first place.

## Save a place

1. Tell Mate the name and position of the spot, for example "Thomas Island
   South is 020°32.676' S 149°07.136' E".
2. When Mate shows a card, check the name and the position.
3. Tap **Apply**. Tap **Dismiss** if you don't want it kept.

If you also tell Mate how deep the water is there, the card shows the depth too.
Mate only adds a depth you stated. If the place already has a depth, the card
shows the old depth and the new one.

## Add a place yourself

1. Open **Settings → Saved places**.
2. Choose **Add place**.
3. Type the name and the position. Use degrees and decimal minutes, such as
   `020°32.676' S` and `149°07.136' E`. Decimal degrees such as `-20.5446` work
   too. The note is optional.
4. If you know the depth, type it in the depth box. The depth is optional.
   Use metres, or feet if Helmcentral is set to show feet. It's the depth at
   chart datum, and a drying height is negative.
5. Choose **Save**.

A depth you type is saved as charted, with today's date at the boat.

If the name matches a place you already have, Helmcentral says so and doesn't
save it. Capitals, punctuation and spacing don't count, and "Is" matches
"Island".

## Save the depth from your anchor watch

Once the anchor is down, the boat's own sounder can give a place its depth. You
need to have set the **Transducer Depth** in **Settings → Anchor Watch**. That's
how far below the waterline the depth transducer sits, so that Helmcentral can
turn the reading into the depth at chart datum.

1. Open **Anchor watch** and open the rode planner. Its line is at the foot of
   the planner, below **Set alarm to**.
2. Read the line, for example "4.2 m at datum, from the sounder". That's the
   figure Helmcentral will save. If it shows a sentence instead of a **Save…**
   link, the sentence says what's missing. If it says "Already saved at", a place
   name and "for this stay", that place holds this depth, sounded on the day the anchor
   went down, and there's nothing to do. A place with the same figure from a
   chart, or from an earlier day, still shows **Save…**, so you can replace it.
3. Tap **Save…**. The figure goes to the nearest place within 0.1 nautical mile
   of the anchor that already has a depth, which is the one Mate reads. If none
   of them has a depth, it goes to the nearest place in range. If there's none
   that close, you're asked for a name, and the new place goes at the anchor's
   position.
4. Read the confirm step. It names the place. If the place has a depth already,
   it shows the old figure and the new one, for example "Replaces 4.0 m (charted
   2026-10-30) at Butterfly Cove with 4.2 m (sounded 2026-11-01)".
5. Tap **Confirm**. Tap **Cancel** to leave things as they were.

The depth is saved as sounded, dated the day the anchor went down at the boat.
The figure uses the depth recorded when you dropped the anchor, so if you've
edited the planning depth, it's the edited one. Helmcentral takes that as
depth below the surface. Nothing is saved until you confirm.

## Change a place

1. Open **Settings → Saved places**.
2. On the place's row, open the actions menu and choose **Edit**.
3. Change the name, the latitude, the longitude, the note or the depth. Type a
   position as degrees and decimal minutes, such as `020°32.676' S`. Decimal
   degrees such as `-20.5446` work too.
4. Choose **Save**.

If the place has a depth and you type a different one, the dialog shows the old
figure and says it will be replaced. Empty the depth box to clear the depth. The
box then says what will be cleared. Changing the name or the position doesn't
touch the depth.

If a position isn't one Helmcentral can read, the box says so and **Save** stays
off until you fix it. If the new name matches another saved place, Helmcentral
refuses it and tells you which one.

## Delete a place

1. Open **Settings → Saved places**.
2. On the place's row, open the actions menu and choose **Delete**.
3. Read the question, then choose **Delete** again.

Mate stops knowing the position at once. You can give it to Mate again whenever
you like.

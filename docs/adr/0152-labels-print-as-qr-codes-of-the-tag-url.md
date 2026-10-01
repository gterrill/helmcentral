# ADR 0152: Labels Print as QR Codes of the Tag URL

## Status

Accepted

## Context

ADR 0127 gave every bin a URL and ADR 0123 gave every equipment item one, and
the way to put that URL on the physical thing was an NFC tag written from
Chrome on Android. Writing a tag needs a supported phone and a sticker. An
iPhone, a laptop or an operator with no stickers on hand has no way to label a
bin at all, and a tag is not readable by eye.

## Decision

1. **A label is a QR code of the tag URL, with the bin code or item name
   printed large beneath it.** The URL comes from the same builder the NFC tag
   row uses (`lib/tag-url.ts`), so a scan and a tap open byte-for-byte the same
   address, built from the page's own origin.
2. **Entry points.** "Print label" beside the tag row on the bin page and in the
   equipment editor's Location section; "Print bin labels" in the Locations
   page menu, a three-across sheet of dashed-border cells for plain A4 or
   Letter paper.
3. **Printing is a print stylesheet, not a PDF.** The dialog holds a preview;
   a second copy of the labels sits in a portal on `<body>`, and `@media print`
   hides everything else and forces black on white. The Print button calls
   `window.print()`.
4. **The encoder is `qrcode-generator` (MIT, no dependencies, about 50 kB
   unminified), drawn as inline SVG** so it prints crisply, with a four-module
   quiet zone and error correction M. It is loaded through a dynamic import
   (`qr-code.tsx` over `qr-code-impl.tsx`, the pattern used for the markdown
   renderers) so the wall kiosk's older WebKit never parses it in the entry
   chunk.
5. **Equipment has no asset code**, so its label prints the item's name, with
   manufacturer and model beneath when set.

## Out of scope

Handover off the tailnet. The address is the boat's tailnet https address, so a
scan opens the page only for a phone that can reach it. A label carries no
sign-in, no public link and no fallback for someone outside the boat's network.

## Consequences

- The tag URL has one builder; changing the URL scheme changes tags and labels
  together. Labels already printed keep the old address, as stuck-down NFC tags
  do.
- A new runtime dependency in `frontend/package.json`, which is why the
  worktree carries its own `node_modules` rather than the shared symlink.

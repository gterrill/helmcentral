---
target: the Mate sheet
total_score: 23
max_score: 40
na_heuristics: 
p0_count: 1
p1_count: 3
target_identity: "file:/Users/gavinator/Work/pikorua/helmcentral/frontend/src/components/mate-sheet.tsx"
target_fingerprint: "sha256:c7a4d0af6ab4a87198c048b5874a72b2f4054bf9f8ea8fa78fe93520e70e8ea2"
target_path: /Users/gavinator/Work/pikorua/helmcentral/frontend/src/components/mate-sheet.tsx
timestamp: 2026-09-12T01-38-27Z
slug: frontend-src-components-mate-sheet-tsx
---
Method: dual-agent (A: design review sub-agent · B: detector/browser sub-agent)

## Design Health Score

| # | Heuristic | Score | Key Issue |
|---|---|---|---|
| 1 | Visibility of System Status | 3 | Spinner and status line while Mate works; no live signal that money is being spent until the footer arrives |
| 2 | Match System / Real World | 3 | Domain copy is right; Maximize2 for "hand this thread to the Mate page and close" says the wrong thing |
| 3 | User Control and Freedom | 1 | With a long reply the thread cannot be scrolled and the composer is off screen; no cancel for a question in flight |
| 4 | Consistency and Standards | 2 | The primitive's close X ignores the 40 px control floor; New is a labelled button on the page and a bare icon here |
| 5 | Error Prevention | 3 | Write-tier gating is solid; Enter sends a half-typed multi-line question |
| 6 | Recognition Rather Than Recall | 1 | Three header actions are aria-label only, no visible label or title |
| 7 | Flexibility and Efficiency | 2 | Alt+M and Escape exist; no way to reach an older thread without leaving the sheet |
| 8 | Aesthetic and Minimalist Design | 4 | Reply typography and rhythm are exactly the design system's |
| 9 | Error Recovery | 2 | Flat red error text, no retry, composer already cleared by the time a send fails |
| 10 | Help and Documentation | 2 | One seed example before the first message; nothing explains Open in Mate or read-aloud |
| **Total** | | **23/40** | **Acceptable** |

## Design Specificity Verdict

LLM assessment: split down the middle. The content layer is Helmcentral's (Geist hierarchy, tabular numerals, hairline rules, the cost footer with `--` fallbacks, stop-reading-on-close). The shell is the stock shadcn sheet recipe on Base UI: `bg-black/80` overlay, `p-6`, `shadow-lg`, generic chat-panel layout, stock Lucide glyphs. None of the product's signature colour vocabulary appears, which is correct for a non-reading surface but leaves the sheet interchangeable once the word Mate is removed.

Deterministic scan: `impeccable detect` returned `[]` on all four .tsx files (exit 0), and Assessment B proved with a probe file that the CLI does not scan .tsx or .css in this environment (the same markup in .html yields findings). In the browser, of 46 findings on /forecast with the sheet open exactly one sits inside the dialog: `tiny-text` on the 11 px message footer, which AGENTS.md explicitly sanctions (false positive against the house rule). On /assistant the detector flagged `line-length` (~109 chars per line) on nine reply paragraphs of the full page, which is out of this target's scope but real, and four of its 25 groups were the detector's own overlay (self-contamination, discounted).

Visual overlays: injection succeeded and the detector ran in the page via the live server; the server was stopped afterwards, and no tab was left for you, so there is no overlay to look at. Measured facts: sheet 576 px wide at 1600 and 768, 292 px at 390; no horizontal overflow at 390; focus lands on the composer on open; Escape closes; `role="dialog"` with `aria-labelledby` but no `aria-modal`; placeholder and footer contrast 7.49:1.

## Overall Impression

The reply itself is the best-typeset surface in the app. The container around it was assembled from the primitive's defaults and never sized: the one measurement that matters, whether you can reach the composer after a real answer, fails. Fix that and the close target and this becomes the calm quick channel the plan described.

## What's Working

- Stop-on-close for read-aloud: closing the sheet silences Mate, designed for the boat rather than borrowed.
- The reply typography: heading scale, paragraph rhythm, hairline rules, tabular numbers, semantic tokens only.
- The cost footer per reply with honest `--` fallbacks, and focus landing in the composer on open.

## Priority Issues

- **[P0] The thread does not scroll and the composer is unreachable after a long reply.** Measured at 1600×1000 and 768×1024: the textarea's top is at 1261 px, Send at 1383 px, no element inside the dialog is scrollable, and the dialog overflow is `visible` inside a fixed, clipped panel. Cause: `mate-sheet.tsx` wraps `AssistantThread` in `<div className="min-h-0 min-w-0 flex-1">`, a block, so the thread's `flex-1 min-h-0` chain has no flex parent and the inner `overflow-y-auto` list never gets a bounded height. Why it matters: after any real briefing you cannot ask a follow-up or read the end of the answer. Fix: make the wrapper `flex min-h-0 flex-1 flex-col` (or `h-full`) and add a test asserting the composer's bounding box is inside the viewport with a long thread. Suggested command: /impeccable layout.
- **[P1] The close control is a 16 px X.** `ui/sheet.tsx` renders `SheetPrimitive.Close` bare with `rounded-sm`, not the 40 px `Button` the sibling actions use. AGENTS.md's 40 px floor exists for a moving boat and wet hands. Fix: render it through `Button variant="ghost" size="icon"` and drop the hand-tuned `mr-6` offset in the header. Suggested command: /impeccable polish.
- **[P1] The 80% black scrim dims a live alarm.** `bg-black/80` covers the dashboard, including the unacknowledged arrivalCircleEntered banner in the screenshots, at the moment the skipper is heads-down in the sheet. PRODUCT.md's first principle is visible failures. Fix: a light scrim (`bg-background/40` or none) and keep the alarm banner above the sheet's stacking context. Suggested command: /impeccable harden.
- **[P1] No cancel for a question in flight.** `useAssistantChat.abort()` exists but is wired only to unmount and a superseding send; the read-aloud state gets a Stop button, the request does not. Fix: a Stop button in the status row while sending. Suggested command: /impeccable harden.
- **[P2] Icon-only header actions with the wrong glyph.** New conversation, Open in Mate and Stop reading carry aria-labels and nothing visible; Maximize2 reads as "enlarge here". Fix: add `title` tooltips, swap the glyph for `PanelRight`/`ArrowUpRight`, and consider the label "Open the Mate page". Suggested command: /impeccable clarify.
- **[P3] h1 and h2 render identically** in `assistant-markdown.tsx`; give h1 one visible step. Suggested command: /impeccable typeset.

## Persona Red Flags

- **Alex (power user):** after a long answer the follow-up path is dead (P0); no cancel mid-flight; no way to switch threads without leaving the sheet.
- **Jordan (first-timer):** three unlabelled icons in the header; "Open in Mate" while looking at a sheet titled Mate; Maximize2 promises the wrong action.
- **Sam (screen reader / keyboard):** focus lands in the composer and Escape works, but the dialog lacks `aria-modal` and the aria-labels are as terse as the visuals; the close control is reachable but tiny.
- **Skipper at a sunlit helm iPad:** the P0 hits hardest (cannot scroll, cannot reply), then the 16 px close target and the scrim over the alarm banner.

## Minor Observations

- `SheetTitle` is `text-lg` (18 px) against DESIGN.md's Title token at 1 rem.
- `SheetHeader`'s default `text-center sm:text-left` is dead once overridden.
- The empty-state example and the composer placeholder make the same nudge two ways; the example never shows again once a thread exists.
- On the full Mate page the reply measure is ~109 characters per line; the sheet's 576 px is fine.
- A Base UI console error about a non-native button (`nativeButton`) fires on the page; source unattributed.
- The detector's own overlay counted itself on /assistant (4 of 25 groups); discounted above.

## Questions to Consider

- If the sheet exists to answer without leaving the page behind it, why does its overlay put 80% black over that page and its live alarm?
- What is the moment the skipper most needs a big target, and why is the smallest control in the product the one that gets them out?
- The cost promise is "never a surprise"; why is the meter silent while it runs and only itemised afterwards in 11 px?

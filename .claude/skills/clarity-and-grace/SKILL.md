---
name: clarity-and-grace
description: Edits and drafts text following Joseph Williams's "Style: Lessons in Clarity and Grace" (diagnosing nominalizations, subject-action alignment, old-to-new flow, and concision).
---

# Style: Lessons in Clarity and Grace

Apply Joseph Williams's diagnostic framework to all written human communication (documentation, commit messages, PR descriptions, guides, and prose). Prioritize reader cognitive load over mechanical grammar rules.

## 1. The Core Engine: Characters and Actions
- **Match Characters to Subjects:** Ensure the real-world actor or main entity is the grammatical subject of the sentence. Avoid abstract placeholders.
- **Match Actions to Verbs:** Diagnose and unpack **nominalizations** (verbs masked as abstract nouns ending in *-tion*, *-ment*, *-ance*, *-ing*). Turn them back into strong verbs.
  - *Reject:* "The execution of data serialization is performed by the worker."
  - *Adopt:* "The worker serializes the data."

## 2. Cohesion and Information Flow (Old-to-New)
- **Sentence Openings:** Begin sentences with information already familiar to the reader from previous context.
- **Sentence Endings (The Stress Position):** Push unfamiliar, complex, or critical ideas to the end of the clause. Save key technical terms or punchlines for the words directly before the period.
- **Topic Strings:** Keep grammatical subjects relatively consistent across sentences within a paragraph to prevent perspective drift.

## 3. Concision and Cutting Deadwood
- **Delete Meaningless Words:** Strip filler (*actually, virtually, essentially, generally, in order to* -> *to*).
- **Purge Redundancy:** Cut paired words (*any and all*, *basic and fundamental*) and implied modifiers (*past history*, *future plans*, *final outcome*).
- **Replace Phrases with Words:**
  - *due to the fact that* -> *because*
  - *in the event that* -> *if*
  - *prior to* -> *before*
  - *make an assumption* -> *assume*
- **Silence Metadiscourse:** Remove throat-clearing declarations (*"It is worth noting that..."*, *"I will now demonstrate..."*).

## 4. Sentence Architecture & Shape
- **Subject-Verb Proximity:** Keep the subject and its main verb close together. Avoid wedging long, parenthetical dependent clauses between them.
- **Parallelism:** Match grammatical structures across coordinate phrases or bullet points.
- **Passive Voice Rule:** Use passive voice deliberately only when:
  1. The actor is unknown or unimportant.
  2. Shifting the object to the subject position preserves old-to-new cohesion from the preceding sentence.

## Execution Workflow
When asked to review or edit:
1. **Diagnose:** Identify heavy nominalizations, buried agents, separated subject-verbs, and throat-clearing.
2. **Rewrite:** Present the revised version applying the principles above.
3. **Explain (if requested):** Briefly highlight which nominalizations were converted and how the information flow was reordered.
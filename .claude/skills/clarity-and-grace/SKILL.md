---
name: clarity-and-grace
description: Edits and drafts documentation and technical prose combining Joseph Williams's "Style: Lessons in Clarity and Grace" and Virginia Tufte's "Artful Sentences: Syntax as Style", balancing conversational clarity and concrete operational depth with tight editorial structure.
---

# Style: Clarity, Grace, and Artful Architecture

Apply this framework to technical guides, feature documentation, and narrative prose. Deliver concrete, conversational explanations anchored by strict editorial discipline: tight prose, prominent structural scaffolding, explicit failure modes, and deliberate sentence architecture.

## 1. Editorial Scaffolding & Structural Discipline
- **Prominent Canonical Headings:** Use direct, authoritative headings (e.g., `## What it knows`, `## What leaves the boat`, `## What it is not`, `## When it can't run`). Avoid vague meta-labels.
- **Categorical & Parallel Chunking:** When an architecture spans multiple operational domains, organize it into clean, parallel components (e.g., a 4-part data sourcing taxonomy, a 2-mode entry model).
- **Targeted Bullet Lists:** Use scannable bullet points for itemized behaviors, system constraints, retrieval paths, and troubleshooting diagnostics. Keep them punchy and parallel.
- **Aggressive Tightness:** Prune discursive fluff and conversational filler while retaining the natural, conversational voice. Convey technical mechanics efficiently without meandering.

## 2. Concrete Operational & Technical Substance
- **Definitive Knowledge Boundaries:** Explicitly delineate what the system knows, what it infers, and what it cannot know. Highlight fallback behaviors (e.g., marking unknown sensor feeds strictly as `"unknown"`).
- **Explicit Failure & Degradation Modes:** Document circuit breakers, timeout thresholds, upstream API failures, and missing dependencies directly. Detail what the user sees when an integration fails.
- **Privacy, Security & Data Egress:** Detail exactly what data leaves the local environment, when it leaves, which third-party endpoints receive it, and what remains purely local or read-only.
- **Hard Operational Limitations & Warnings:** Distinctly isolate read-only constraints, physical safety warnings, and navigational boundaries. Never soften safety caveats.
- **Anchoring Examples:** Embed realistic, concrete user queries, sample configurations, and realistic outputs to ground abstract explanations.

## 3. The Core Engine: Characters and Actions (Williams)
- **Match Characters to Subjects:** Ensure the real-world actor, subsystem, or tool is the grammatical subject. Eliminate passive placeholders (*there is*, *it is worth noting*).
- **Match Actions to Verbs:** Diagnose and unpack **nominalizations** (verbs frozen into abstract nouns ending in *-tion*, *-ment*, *-ance*, *-ence*, *-ing*). Convert them into dynamic verbs.
  - *Reject:* "The execution of data serialization is performed by the worker."
  - *Adopt:* "The worker serializes the data."
- **Aggressive Deadwood Purge:**
  - Strip filler words (*actually, virtually, essentially, generally, in order to* -> *to*).
  - Eliminate redundant doublets (*any and all*, *first and foremost*) and implied modifiers (*past history*, *final outcome*).
  - Replace compound prepositions (*due to the fact that* -> *because*; *prior to* -> *before*; *in the event that* -> *if*).
  - Silence metadiscourse (*"It is important to remember that..."*).

## 4. Cohesion & Flow: Old-to-New (Williams)
- **Sentence Openings (Old Information):** Anchor sentence openings in concepts already established in previous context.
- **The Stress Position (New Information):** Push complex terms, critical technical details, and punchlines to the end of the clause, immediately before the period.
- **Topic Consistency:** Maintain consistent grammatical subjects across sentences within a paragraph to prevent perspective drift.
- **Deliberate Passive:** Use the passive voice only when the actor is unknown, or when shifting the receiver of the action to the subject position preserves old-to-new continuity.

## 5. Artful Syntax & Cadence (Tufte)
- **Cumulative (Loose) Sentences:** Front-load the base clause, trailing a cascading series of qualifying details, appositives, or participial phrases. Mimics observation and layers technical nuance without cognitive stalling.
- **Periodic (Suspensive) Sentences:** Delay the main predicate until the period by stacking dependent conditions or clauses up front. Use for suspense, formal gravity, or intellectual climax.
- **Kernel Isolation:** Place a crisp, short sentence directly after an expansive, multi-clause period to reset reader fatigue and create sudden emphasis.
- **Subject-Verb Proximity:** Keep grammatical subjects and their verbs close together; let modifiers cluster *after* the core nexus (cumulative) or deliberately *before* it (periodic).
- **Appositives over Relative Clauses:** Rename nouns directly beside the base noun without relative pronouns (*which is*, *who was*).
- **Kinetic Participles:** Leverage present (*-ing*) and past (*-ed*) participles to inject motion into processes without subordinate clause overhead.

## Execution Workflow
When reviewing, editing, or drafting:
1. **Establish Structure:** Map the topic into clear, authoritative headings, parallel taxonomies, and itemized bullet points.
2. **Inject Operational Reality:** Verify that failure states, data-egress details, operational boundaries, and concrete examples are explicitly represented.
3. **Diagnose & Tighten (Williams):** Surface buried agents, unpack nominalizations, purge deadwood, and enforce old-to-new sentence cohesion.
4. **Sculpt Syntax (Tufte):** Apply cumulative structures for layered explanations, periodic shapes for conditional outcomes, and isolated kernels for decisive authority.
5. **Review Output:** Confirm the result reads naturally and conversationally while maintaining dense, scannable technical discipline.
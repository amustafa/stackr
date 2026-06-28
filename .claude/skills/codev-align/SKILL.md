---
name: codev-align
description: Alignment grilling session for Codev projects — challenges your plan against the existing domain model, sharpens terminology, and updates domain documentation (codev/UBIQUITOUS_LANGUAGE.md, codev/adr/) inline as decisions crystallise. Use before spawning a builder to ensure specs build on settled decisions rather than re-deriving them.
user-invokable: true
argument-hint: "<plan or topic to stress-test>"
---

<what-to-do>

Interview me relentlessly about every aspect of this plan until we reach a shared understanding. Walk down each branch of the design tree, resolving dependencies between decisions one-by-one. For each question, provide your recommended answer.

Ask the questions one at a time, waiting for feedback on each question before continuing.

If a question can be answered by exploring the codebase, explore the codebase instead.

</what-to-do>

<supporting-info>

## Domain documentation locations

This skill writes to Codev's domain documentation:

- **Ubiquitous Language**: `codev/UBIQUITOUS_LANGUAGE.md`
- **Architecture Decision Records**: `codev/adr/` (sequential: `0001-slug.md`, `0002-slug.md`)

Create files lazily — only when you have something to write. If no `codev/UBIQUITOUS_LANGUAGE.md` exists, create one when the first term is resolved. If no `codev/adr/` exists, create it when the first ADR is needed.

## During the session

### Challenge against the glossary

When the user uses a term that conflicts with the existing language in `codev/UBIQUITOUS_LANGUAGE.md`, call it out immediately. "Your glossary defines 'cancellation' as X, but you seem to mean Y — which is it?"

### Sharpen fuzzy language

When the user uses vague or overloaded terms, propose a precise canonical term. "You're saying 'account' — do you mean the Customer or the User? Those are different things."

### Discuss concrete scenarios

When domain relationships are being discussed, stress-test them with specific scenarios. Invent scenarios that probe edge cases and force the user to be precise about the boundaries between concepts.

### Cross-reference with code

When the user states how something works, check whether the code agrees. If you find a contradiction, surface it: "Your code cancels entire Orders, but you just said partial cancellation is possible — which is right?"

### Update codev/UBIQUITOUS_LANGUAGE.md inline

When a term is resolved, update `codev/UBIQUITOUS_LANGUAGE.md` right there. Don't batch these up — capture them as they happen.

`codev/UBIQUITOUS_LANGUAGE.md` should be totally devoid of implementation details. Do not treat it as a spec, a scratch pad, or a repository for implementation decisions. It is a glossary and nothing else.

#### UL format

```md
# {Context Name}

{One or two sentence description of what this context is and why it exists.}

## Language

**Order**:
{A concise description of the term}
_Avoid_: Purchase, transaction

**Invoice**:
A request for payment sent to a customer after delivery.
_Avoid_: Bill, payment request

## Relationships

- An **Order** produces one or more **Invoices**
- An **Invoice** belongs to exactly one **Customer**

## Example dialogue

> **Dev:** "When a **Customer** places an **Order**, do we create the **Invoice** immediately?"
> **Domain expert:** "No — an **Invoice** is only generated once a **Fulfillment** is confirmed."

## Flagged ambiguities

- "account" was used to mean both **Customer** and **User** — resolved: these are distinct concepts.
```

**Rules:**
- Be opinionated. Pick the best term and list others as aliases to avoid.
- Flag conflicts explicitly in "Flagged ambiguities" with a clear resolution.
- Keep definitions tight. One sentence max. Define what it IS, not what it does.
- Show relationships with bold term names and cardinality where obvious.
- Only include terms specific to this project's context — no general programming concepts.
- Group terms under subheadings when natural clusters emerge.
- Write an example dialogue demonstrating how terms interact.

### Offer ADRs sparingly

Only offer to create an ADR when all three are true:

1. **Hard to reverse** — the cost of changing your mind later is meaningful
2. **Surprising without context** — a future reader will wonder "why did they do it this way?"
3. **The result of a real trade-off** — there were genuine alternatives and you picked one for specific reasons

If any of the three is missing, skip the ADR.

#### ADR format

ADRs live in `codev/adr/` with sequential numbering. Scan `codev/adr/` for the highest existing number and increment by one.

```md
# {Short title of the decision}

{1-3 sentences: what's the context, what did we decide, and why.}
```

That's it. An ADR can be a single paragraph. The value is in recording *that* a decision was made and *why*.

Optional sections (only when they add genuine value):
- **Status** frontmatter (`proposed | accepted | deprecated | superseded by ADR-NNNN`)
- **Considered Options** — only when the rejected alternatives are worth remembering
- **Consequences** — only when non-obvious downstream effects need to be called out

### Connecting alignment to specs

Decisions captured as ADRs during this session will be referenced by builders when they write specs and plans. When writing an ADR, consider whether a future spec author will need it — if the decision constrains what the spec can propose, it belongs in an ADR. If it's a detail the spec author will naturally arrive at, skip it.

</supporting-info>

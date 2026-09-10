# Coding standards

Work in bounded expansion and consolidation cycles. Before each consolidation,
re-read this document and audit the change against it.

## Architecture and domain

- Understand the problem and solution domains before choosing abstractions.
  Identify aggregate roots that own coherent invariants and state transitions.
- Build domain-outward. Infrastructure, application, and presentation code adapt
  to domain contracts; generic components must not depend on scenario-specific,
  exporter-specific, diagram-specific, or meta-model-specific concepts.
- Maintain hexagonal dependency direction and dependency inversion. Define narrow
  ports toward the core and keep platform adapters outside it.
- Find existing or near-existing conventions and abstractions before adding a
  new one. Improve an inadequate abstraction when that is in scope and less
  surprising than creating a competing concept.
- Apply DRY to shared domain behavior, not merely similar text. Balance reuse
  against indirection and complexity; remove premature abstractions.

## Modularity and clarity

- Keep components focused, modular, and pure where practical. Go source files
  have a 250-line soft limit and a 350-line hard limit.
- Keep signatures narrow. Too many parameters usually indicate missing context
  ownership or mixed responsibilities.
- Prefer plain, readable code over clever compression. Avoid boolean mode flags;
  use separate policies or strategies when behavior differs.
- Prefer expressions, small composable functions, higher-order functions, typed
  folds, and polymorphism over large nested control structures.
- Keep cognitive and cyclomatic complexity bounded; 15 is a review heuristic,
  not a mechanical target.
- Use expressive, role-oriented names that describe solution behavior without
  leaking implementation mechanics. See Naming.

## Naming

The rule that decides cases: **name a thing for what it is, not for how it
came to be.** A function that answers with a value is named for the value. Only
a command -- something called for its effect, answering with nothing -- may be
named with a verb.

- A set of dependencies is named by the services in it, in the vocabulary a
  consumer uses to ask for them. `Repositories`, `ProviderGateways`, `Caches`.
  If no noun names the group, the grouping is wrong: split it until each part
  has a name.
- Never name a value for the act of obtaining it, or for where it came from.
  `ownRecords`, `readFromOthers`, `keptForAWhile` and `Everything` all describe
  a provenance or a duration where a service name belongs -- and a consumer
  asking for a `catalog.Repository` cannot tell which of them has it.
- Vernacular circumlocution is the tell. `ForAWhile`, `FromOthers`, `Own`,
  `Everything`, `Stuff`, `Data`, `Info`, `Manager`, `Helper` say nothing a
  reader could not have guessed, and they read as prose rather than as
  vocabulary.
- No gerunds or participles for functions. A gerund names an activity, and a
  function that answers with a `ScannedDisc` is not an activity. `Tracking`,
  `LogViewing` and `isRated` are fine: the first is a noun this domain has,
  the second is a command, the third is a predicate.
- Established pattern names -- Repository, Gateway, Policy, Cache, ReadModel --
  are earned suffixes and not decoration. Use one when the thing is one.

This is not lintable and was measured not to be: this repository has around
forty legitimate identifiers ending in `-ing` or `-ed`, so a lexical check
would produce more false positives than findings. The rule binds by being
read before the name is chosen.

## Types and correctness

- Use explicit, specific signatures and narrow interface-segregated bounds.
  Prefer composition, balanced type width, and erased evidence only where it
  materially improves correctness.
- Avoid explicit top types except where the domain genuinely permits every Go
  value. Document each unavoidable use.
- Do not cast away correctness or suppress QA findings unless there is a
  documented reason strict typing cannot express the invariant.
- Prefer typed effect or sum-based failure handling. Eliminate monadic values
  with folds or matches instead of imperative discriminator probing in domain
  workflows, where Go's ergonomics make that reasonable.
- Pursue correctness by construction using domain knowledge, formal invariants,
  and appropriate data structures. Choose advanced structures only when their
  semantics are actually needed.

## Data, quality, and delivery

- Use prepared statements for database interactions. Design indexes with
  cardinality and coverage in mind, and use streaming, CTEs, windows, cursor
  pagination, or aggregation pipelines when the problem calls for them.
- Run strict formatting, type checking, linting, static analysis, unit tests,
  race tests, and end-to-end tests appropriate to the change.
- Every public capability needs an intelligible minimal example and coverage in
  a complete executable, end-to-end-testable program. Several capabilities may
  share one coherent scenario; do not cram unrelated behavior together.
- Generated code must be deterministic, formatted, fully typed, reviewable,
  golden-tested, and checked for drift. Generation must not hide effect control
  flow or invent a second source language.

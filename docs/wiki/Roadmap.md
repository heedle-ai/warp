Each milestone ends with code, tests and updated wiki pages.

## Milestone 1: Store (Planned)

- Blob, tree and commit types with canonical encoding
- `Put` and `Get` by ID; filesystem-backed objects
- Compare-and-swap refs stored in SQLite
- Tests: stable IDs across runs and platforms; concurrent CAS where exactly one writer wins

## Milestone 2: shuttle integration (Planned)

- Activities that read and write warp; plan nodes that return object IDs
- Workflow IDs derived from the plan and input hashes
- One commit per plan run, linked to the Temporal workflow and run ID
- Demo: "what did the agent know at run X?"

## Milestone 3: provenance and invalidation (Planned)

- Each derived object records which inputs it depends on, as a provenance polynomial in ℕ[X]
- Retracting a source evaluates that polynomial into Bool and lists the conclusions that lost all support
- A plan that re-runs only the affected nodes

## Milestone 4: merge (Planned)

- Union for evidence that only accumulates, which forms a join-semilattice, so the merge always exists
- Explicit conflicts for claims that contradict each other, resolved by review through Temporal signals

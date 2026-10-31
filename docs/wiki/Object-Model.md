> **Status: Planned.** This is the design that milestone 1 will implement. Names and encodings may change before then.

## Objects

| Object | Contents | Example use |
|---|---|---|
| **Blob** | Immutable bytes | An observation, a tool result, a prompt, a claim |
| **Tree** | Sorted map from names to object IDs | An agent's memory or workspace at one moment |
| **Commit** | Root tree, parent commits, metadata | One checkpoint: a plan's hash, its Temporal workflow and run ID, and evaluation results |
| **Ref** | A name pointing to a commit | `agents/research/main`, an experimental branch |

## Identity

```text
id(o) = SHA-256( type ‖ canonicalEncode(o) )
```

- The same content always gets the same ID, and a different ID means different content.
- `canonicalEncode` is deterministic and versioned: trees sort their entries, and maps and floats have one fixed encoding. Changing the encoding is a breaking change.
- Objects never change after they're written. "Updating" state means writing new objects and a new commit.

## Refs

Refs are the only mutable state, and they move only by compare-and-swap:

```text
promote(ref, expected, new): set ref := new only if ref == expected
```

So two agents that both started from commit `A` can't silently overwrite each other. The second promotion fails, and the agent has to merge.

## How shuttle will use warp

- **Plan nodes return IDs, not payloads,** so Temporal history stays small.
- **All warp reads and writes happen inside activities,** never in workflow code, because workflow code must stay deterministic.
- **Workflow ID = hash(plan ID ‖ input tree ID),** so the same plan on the same inputs runs once (Temporal's `USE_EXISTING` conflict policy).
- **Each run ends with a commit** recording the plan, inputs, outputs, and the Temporal workflow and run IDs.

## Not covered by this design

- **Undoing real-world actions.** Moving a ref back doesn't unsend an email. Effects need idempotency keys and recorded receipts.
- **Security.** Immutable doesn't mean private. Encryption, access control and deletion for sensitive content need their own design.

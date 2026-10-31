# warp

heedle-ai's Merkle object store. In weaving, the warp is the fixed set of threads
the weft is woven through. Here it is the immutable history that computations
are recorded against.

**Status:** placeholder. No code yet.

## Intended model

| Object | Meaning |
|---|---|
| Blob | Immutable content: an observation, tool result, prompt, claim |
| Tree | Map from names to object IDs: an agent's state or workspace |
| Commit | State root, parents, metadata (plan hash, Temporal workflow and run ID) |
| Ref | Mutable name for a commit, updated only by compare-and-swap |

`id(o) = H(type ‖ canonicalEncode(o))`

The first backend will be SQLite for refs plus filesystem-backed objects. All
store I/O happens inside Temporal activities (see
[shuttle](https://github.com/heedle-ai/shuttle)), never in workflow code.

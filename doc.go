// Package warp is heddle's content-addressed object store.
//
// Immutable objects (blobs, trees, commits) are identified by the hash of
// their type and canonical encoding. Refs are the only mutable state; they
// name commits and move only by compare-and-swap. Nothing is implemented
// yet. See README.md for the intended model.
package warp

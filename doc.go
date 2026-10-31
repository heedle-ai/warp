// Package warp is heedle-ai's content-addressed object store: a bare Git
// repository driven through the git command line.
//
// Objects (blobs, trees, commits) are written with git's plumbing commands
// and identified by Git's own object IDs, so equal content always has an
// equal ID. Refs are the only mutable state and move only by
// compare-and-swap (git update-ref with an expected old value). Because the
// store is an ordinary Git repository, git log, git show, git fsck and git
// push all work on it.
//
// warp is called from Temporal activities, never from workflow code.
package warp

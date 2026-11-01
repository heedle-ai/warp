package warp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// MergeBases returns the best common ancestors of commits a and b
// (git merge-base --all), sorted. There are none when the histories are
// unrelated, and more than one after a criss-cross merge.
func (s *Store) MergeBases(ctx context.Context, a, b ID) ([]ID, error) {
	if err := s.exists(ctx, a, "commit"); err != nil {
		return nil, err
	}
	if err := s.exists(ctx, b, "commit"); err != nil {
		return nil, err
	}
	out, err := s.git(ctx, nil, "merge-base", "--all", string(a), string(b))
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 && len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil // unrelated histories
	}
	if err != nil {
		return nil, err
	}
	var bases []ID
	for _, f := range strings.Fields(string(out)) {
		bases = append(bases, ID(f))
	}
	sort.Slice(bases, func(i, j int) bool { return bases[i] < bases[j] })
	return bases, nil
}

// MergeTrees merges trees ours and theirs against their common ancestor
// base with Git's own three-way merge (git merge-tree --write-tree). An
// empty base means the empty tree. This is Git's merge, not a simpler one:
//
//   - Files are merged line by line: if ours and theirs change different
//     lines of the same file, the result has both changes and no conflict.
//   - Subtrees are merged path by path inside them.
//   - Renames are always detected (git merge-tree can't turn that off): if
//     a side deletes a path and adds similar content elsewhere, the other
//     side's change follows it.
//   - The store's own config and attributes (merge drivers) apply.
//
// So the merge is "per path, whole entries" only for a flat tree of
// one-line blobs where no side deletes a path. That's how loom uses it: its
// beliefs index is exactly that, and loom refuses any index that isn't.
//
// It returns the merged tree and the conflicting paths, sorted. A
// conflicting path's entry in the merged tree holds Git's conflict markers,
// so callers must replace every conflicting entry before using the tree.
//
// git merge-tree merges commits, so the three trees are wrapped in
// throwaway commits (base, and ours and theirs on top of it) that no ref
// points to; Git's garbage collection removes them.
func (s *Store) MergeTrees(ctx context.Context, base, ours, theirs ID) (ID, []string, error) {
	if err := s.needGit(ctx, 2, 38); err != nil {
		return "", nil, err
	}
	if base == "" {
		empty, err := s.PutTree(ctx, nil)
		if err != nil {
			return "", nil, err
		}
		base = empty
	}
	for _, t := range []ID{base, ours, theirs} {
		if err := s.exists(ctx, t, KindTree); err != nil {
			return "", nil, err
		}
	}
	wrap := func(tree ID, parent ID) (ID, error) {
		args := []string{"-c", "commit.gpgSign=false", "commit-tree", "-m", "warp merge-tree input", string(tree)}
		if parent != "" {
			args = append(args, "-p", string(parent))
		}
		return s.id(gitCmd(ctx, s.dir, nil, args,
			"GIT_AUTHOR_NAME=warp", "GIT_AUTHOR_EMAIL=warp@heedle.ai", "GIT_AUTHOR_DATE=@0 +0000",
			"GIT_COMMITTER_NAME=warp", "GIT_COMMITTER_EMAIL=warp@heedle.ai", "GIT_COMMITTER_DATE=@0 +0000"))
	}
	b, err := wrap(base, "")
	if err != nil {
		return "", nil, err
	}
	o, err := wrap(ours, b)
	if err != nil {
		return "", nil, err
	}
	t, err := wrap(theirs, b)
	if err != nil {
		return "", nil, err
	}
	out, err := s.git(ctx, nil, "merge-tree", "--write-tree", "-z",
		"--name-only", "--no-messages", string(o), string(t))
	var ee *exec.ExitError
	conflicted := errors.As(err, &ee) && ee.ExitCode() == 1
	if err != nil && !conflicted {
		return "", nil, err
	}
	fields := strings.Split(string(out), "\x00")
	tree := ID(strings.TrimSpace(fields[0]))
	if err := validID(tree); err != nil {
		return "", nil, fmt.Errorf("warp: unexpected git merge-tree output %q", out)
	}
	seen := map[string]bool{}
	var paths []string
	for _, p := range fields[1:] {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	if conflicted != (len(paths) > 0) {
		return "", nil, fmt.Errorf("warp: git merge-tree exit status and conflicts disagree: %q", out)
	}
	return tree, paths, nil
}

// RefInfo is one ref and the commit it points to.
type RefInfo struct {
	Name   string
	Commit ID
}

// Refs lists the refs under prefix (which must start with refs/ and end
// with /), sorted by name (git for-each-ref). Refs pointing at anything but
// a commit are left out.
func (s *Store) Refs(ctx context.Context, prefix string) ([]RefInfo, error) {
	if !strings.HasPrefix(prefix, "refs/") || !strings.HasSuffix(prefix, "/") {
		return nil, fmt.Errorf("%w: ref prefix %q must start with refs/ and end with /", ErrInvalid, prefix)
	}
	if err := checkRef(ctx, prefix+"x"); err != nil {
		return nil, fmt.Errorf("%w: ref prefix %q", ErrInvalid, prefix)
	}
	out, err := s.git(ctx, nil, "for-each-ref", "--sort=refname",
		"--format=%(refname)%00%(objectname)%00%(objecttype)", prefix)
	if err != nil {
		return nil, err
	}
	var refs []RefInfo
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) == 3 && f[2] == "commit" {
			refs = append(refs, RefInfo{Name: f[0], Commit: ID(f[1])})
		}
	}
	return refs, nil
}

// needGit fails unless the git binary is at least major.minor.
func (s *Store) needGit(ctx context.Context, major, minor int) error {
	out, err := s.git(ctx, nil, "version")
	if err != nil {
		return err
	}
	var M, m int
	if _, err := fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(string(out)), "git version "), "%d.%d", &M, &m); err != nil {
		return fmt.Errorf("warp: can't read git version from %q", out)
	}
	if M < major || M == major && m < minor {
		return fmt.Errorf("warp: this needs Git %d.%d or newer; %s", major, minor, strings.TrimSpace(string(out)))
	}
	return nil
}

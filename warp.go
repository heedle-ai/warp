package warp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ID is a Git object name in full hexadecimal form.
type ID string

// Kind is the type of object a tree entry points to.
type Kind string

const (
	KindBlob Kind = "blob"
	KindTree Kind = "tree"
)

// Entry is one named entry of a tree.
type Entry struct {
	Name string
	Kind Kind
	ID   ID
}

// Trailer is one "Key: Value" line in a commit message's trailer block,
// the format read and written by git interpret-trailers.
type Trailer struct {
	Key, Value string
}

// Signature identifies who made a commit and when. Git stores whole
// seconds, so When is truncated to the second.
type Signature struct {
	Name, Email string
	When        time.Time
}

// CommitSpec is everything a commit is made of. The same spec always
// produces the same commit ID.
type CommitSpec struct {
	Tree     ID
	Parents  []ID
	Message  string
	Trailers []Trailer
	// Author is used as both author and committer.
	Author Signature
}

var (
	// ErrConflict means a ref was not where the caller expected: another
	// writer moved or created it first. Re-read the ref and try again.
	ErrConflict = errors.New("warp: ref changed concurrently")
	// ErrNotFound means the object or ref does not exist.
	ErrNotFound = errors.New("warp: not found")
)

// Store is a bare Git repository driven through the git command line.
// Every operation is one or more git plumbing commands, so the store can
// be inspected, backed up and replicated with ordinary git.
type Store struct {
	dir string
}

// Init creates a bare repository at dir, or opens it if one is already
// there (git init is safe to repeat).
func Init(ctx context.Context, dir string) (*Store, error) {
	if _, err := gitCmd(ctx, "", nil, []string{"init", "--bare", "--quiet", dir}); err != nil {
		return nil, err
	}
	return Open(ctx, dir)
}

// Open opens an existing bare repository.
func Open(ctx context.Context, dir string) (*Store, error) {
	s := &Store{dir: dir}
	out, err := s.git(ctx, nil, "rev-parse", "--is-bare-repository")
	if err != nil {
		return nil, fmt.Errorf("warp: open %s: %w", dir, err)
	}
	if strings.TrimSpace(string(out)) != "true" {
		return nil, fmt.Errorf("warp: open %s: not a bare repository", dir)
	}
	return s, nil
}

// Dir is the repository path, for use with git --git-dir.
func (s *Store) Dir() string { return s.dir }

// PutBlob stores data and returns its ID (git hash-object -w).
func (s *Store) PutBlob(ctx context.Context, data []byte) (ID, error) {
	return s.id(s.git(ctx, data, "hash-object", "-w", "--stdin"))
}

// HashBlob returns the ID data would have as a blob, without writing it
// or needing a store (git hash-object --stdin).
func HashBlob(ctx context.Context, data []byte) (ID, error) {
	out, err := gitCmd(ctx, "", data, []string{"hash-object", "--stdin"})
	if err != nil {
		return "", err
	}
	return ID(strings.TrimSpace(string(out))), nil
}

// Blob returns the contents of a blob (git cat-file blob).
func (s *Store) Blob(ctx context.Context, id ID) ([]byte, error) {
	if err := s.exists(ctx, id, KindBlob); err != nil {
		return nil, err
	}
	return s.git(ctx, nil, "cat-file", "blob", string(id))
}

// PutTree stores a tree and returns its ID (git mktree). Entries may be
// in any order; git sorts them. Names must be unique and must not be
// empty, ".", "..", or contain "/" or NUL.
func (s *Store) PutTree(ctx context.Context, entries []Entry) (ID, error) {
	seen := make(map[string]bool, len(entries))
	var in bytes.Buffer
	for _, e := range entries {
		if err := validName(e.Name); err != nil {
			return "", err
		}
		if seen[e.Name] {
			return "", fmt.Errorf("warp: duplicate tree entry %q", e.Name)
		}
		seen[e.Name] = true
		mode := map[Kind]string{KindBlob: "100644", KindTree: "040000"}[e.Kind]
		if mode == "" {
			return "", fmt.Errorf("warp: entry %q: unknown kind %q", e.Name, e.Kind)
		}
		fmt.Fprintf(&in, "%s %s %s\t%s\x00", mode, e.Kind, e.ID, e.Name)
	}
	return s.id(s.git(ctx, in.Bytes(), "mktree", "-z"))
}

// Tree returns a tree's entries in Git's order (git ls-tree).
func (s *Store) Tree(ctx context.Context, id ID) ([]Entry, error) {
	if err := s.exists(ctx, id, KindTree); err != nil {
		return nil, err
	}
	out, err := s.git(ctx, nil, "ls-tree", "-z", string(id))
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, rec := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if rec == "" {
			continue
		}
		meta, name, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			return nil, fmt.Errorf("warp: unexpected ls-tree output %q", rec)
		}
		entries = append(entries, Entry{Name: name, Kind: Kind(f[1]), ID: ID(f[2])})
	}
	return entries, nil
}

// Commit stores a commit and returns its ID (git commit-tree). Trailers
// are appended with git interpret-trailers.
func (s *Store) Commit(ctx context.Context, c CommitSpec) (ID, error) {
	if c.Author.Name == "" || c.Author.Email == "" {
		return "", errors.New("warp: commit needs an author name and email")
	}
	msg := c.Message
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	if len(c.Trailers) > 0 {
		args := []string{"interpret-trailers"}
		for _, t := range c.Trailers {
			if t.Key == "" || strings.ContainsAny(t.Key, ": \t\r\n") || strings.ContainsAny(t.Value, "\r\n") {
				return "", fmt.Errorf("warp: invalid trailer %q: %q", t.Key, t.Value)
			}
			args = append(args, "--trailer", t.Key+": "+t.Value)
		}
		out, err := s.git(ctx, []byte(msg), args...)
		if err != nil {
			return "", err
		}
		msg = string(out)
	}
	args := []string{"commit-tree", string(c.Tree)}
	for _, p := range c.Parents {
		args = append(args, "-p", string(p))
	}
	date := "@" + strconv.FormatInt(c.Author.When.Unix(), 10) + " +0000"
	env := []string{
		"GIT_AUTHOR_NAME=" + c.Author.Name, "GIT_AUTHOR_EMAIL=" + c.Author.Email, "GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=" + c.Author.Name, "GIT_COMMITTER_EMAIL=" + c.Author.Email, "GIT_COMMITTER_DATE=" + date,
	}
	return s.id(gitCmd(ctx, s.dir, []byte(msg), append([]string{"-c", "commit.gpgSign=false"}, args...), env...))
}

// Resolve returns the commit a ref points to (git rev-parse --verify).
// ok is false if the ref does not exist.
func (s *Store) Resolve(ctx context.Context, ref string) (id ID, ok bool, err error) {
	if err := s.checkRef(ctx, ref); err != nil {
		return "", false, err
	}
	out, err := s.git(ctx, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return ID(strings.TrimSpace(string(out))), true, nil
}

// Advance moves ref from old to next atomically (git update-ref). An
// empty old means "create ref; it must not exist yet". If the ref is not
// at old, or another writer holds it, Advance returns ErrConflict and
// changes nothing.
func (s *Store) Advance(ctx context.Context, ref string, old, next ID) error {
	if err := s.checkRef(ctx, ref); err != nil {
		return err
	}
	if old == "" {
		z, err := s.zeroID(ctx)
		if err != nil {
			return err
		}
		old = z
	}
	_, err := s.git(ctx, nil, "update-ref", "-m", "warp: advance", ref, string(next), string(old))
	if err != nil && strings.Contains(err.Error(), "cannot lock ref") {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}

// IsAncestor reports whether commit a is an ancestor of, or equal to,
// commit b (git merge-base --is-ancestor).
func (s *Store) IsAncestor(ctx context.Context, a, b ID) (bool, error) {
	_, err := s.git(ctx, nil, "merge-base", "--is-ancestor", string(a), string(b))
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return err == nil, err
}

// FindByTrailer returns the most recent commit reachable from ref whose
// trailer key has exactly the given value (git log). ok is false if
// there is none, including when ref does not exist.
func (s *Store) FindByTrailer(ctx context.Context, ref, key, value string) (id ID, ok bool, err error) {
	head, exists, err := s.Resolve(ctx, ref)
	if err != nil || !exists {
		return "", false, err
	}
	format := "--format=%H%x00%(trailers:key=" + key + ",valueonly,separator=%x00)%x01"
	out, err := s.git(ctx, nil, "log", format, string(head))
	if err != nil {
		return "", false, err
	}
	for _, rec := range strings.Split(string(out), "\x01") {
		f := strings.Split(strings.TrimSpace(rec), "\x00")
		for _, v := range f[1:] {
			if strings.TrimSpace(v) == value {
				return ID(f[0]), true, nil
			}
		}
	}
	return "", false, nil
}

// Trailers returns a commit's trailers in order (git interpret-trailers
// --parse over the commit message).
func (s *Store) Trailers(ctx context.Context, commit ID) ([]Trailer, error) {
	if err := s.exists(ctx, commit, "commit"); err != nil {
		return nil, err
	}
	msg, err := s.git(ctx, nil, "log", "-1", "--format=%B", string(commit))
	if err != nil {
		return nil, err
	}
	out, err := s.git(ctx, msg, "interpret-trailers", "--parse")
	if err != nil {
		return nil, err
	}
	var ts []Trailer
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if k, v, ok := strings.Cut(line, ": "); ok {
			ts = append(ts, Trailer{Key: k, Value: v})
		}
	}
	return ts, nil
}

// Parents returns a commit's tree and parent IDs.
func (s *Store) Parents(ctx context.Context, commit ID) (tree ID, parents []ID, err error) {
	if err := s.exists(ctx, commit, "commit"); err != nil {
		return "", nil, err
	}
	out, err := s.git(ctx, nil, "log", "-1", "--format=%T %P", string(commit))
	if err != nil {
		return "", nil, err
	}
	f := strings.Fields(string(out))
	for _, p := range f[1:] {
		parents = append(parents, ID(p))
	}
	return ID(f[0]), parents, nil
}

// === helpers ===============================================================

func validName(n string) error {
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\x00") {
		return fmt.Errorf("warp: invalid tree entry name %q", n)
	}
	return nil
}

func (s *Store) checkRef(ctx context.Context, ref string) error {
	if !strings.HasPrefix(ref, "refs/") {
		return fmt.Errorf("warp: ref %q must start with refs/", ref)
	}
	if _, err := gitCmd(ctx, "", nil, []string{"check-ref-format", ref}); err != nil {
		return fmt.Errorf("warp: invalid ref name %q", ref)
	}
	return nil
}

func (s *Store) exists(ctx context.Context, id ID, kind Kind) error {
	out, err := s.git(ctx, nil, "cat-file", "-t", string(id))
	if err != nil || strings.TrimSpace(string(out)) != string(kind) {
		return fmt.Errorf("%w: %s %s", ErrNotFound, kind, id)
	}
	return nil
}

func (s *Store) zeroID(ctx context.Context) (ID, error) {
	out, err := s.git(ctx, nil, "rev-parse", "--show-object-format")
	if err != nil {
		return "", err
	}
	n := map[string]int{"sha1": 40, "sha256": 64}[strings.TrimSpace(string(out))]
	if n == 0 {
		return "", fmt.Errorf("warp: unknown object format %q", out)
	}
	return ID(strings.Repeat("0", n)), nil
}

func (s *Store) id(out []byte, err error) (ID, error) {
	if err != nil {
		return "", err
	}
	return ID(strings.TrimSpace(string(out))), nil
}

func (s *Store) git(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	return gitCmd(ctx, s.dir, stdin, args)
}

// gitCmd runs git with a clean environment: no inherited GIT_* variables
// (a caller inside a git hook would otherwise redirect us), no system or
// global config, no prompts, and a fixed locale so messages are stable.
func gitCmd(ctx context.Context, gitDir string, stdin []byte, args []string, extraEnv ...string) ([]byte, error) {
	if gitDir != "" {
		args = append([]string{"--git-dir", gitDir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "LC_") && !strings.HasPrefix(kv, "LANG") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, extraEnv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

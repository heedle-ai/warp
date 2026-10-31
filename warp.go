package warp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ID is a Git object name in full lowercase hexadecimal form: 40 digits for
// SHA-1 repositories, 64 for SHA-256. Abbreviations and revision
// expressions are rejected.
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
// the format read and written by git interpret-trailers. Keys are letters,
// digits and hyphens, starting with a letter or digit. Values are non-empty,
// on one line, without leading or trailing whitespace. A trailer reads back
// exactly as written, in order, duplicates included.
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
	Tree    ID
	Parents []ID
	// Message must not end in a paragraph Git would read as trailers; put
	// those in Trailers.
	Message  string
	Trailers []Trailer
	// Author is used as both author and committer.
	Author Signature
}

var (
	// ErrConflict means a ref was not where the caller expected, or another
	// writer held its lock: re-read the ref and try again. Nothing changed.
	ErrConflict = errors.New("warp: ref changed concurrently")
	// ErrNotFound means the object or ref does not exist.
	ErrNotFound = errors.New("warp: not found")
	// ErrInvalid means an argument can never be accepted: a malformed ID,
	// ref, name or trailer, or a ref update that isn't a fast-forward.
	// Retrying won't help.
	ErrInvalid = errors.New("warp: invalid argument")
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

// HashBlob returns the ID data would have as a blob, without writing it
// or needing a store (git hash-object --stdin).
func HashBlob(ctx context.Context, data []byte) (ID, error) {
	out, err := gitCmd(ctx, "", data, []string{"hash-object", "--stdin"})
	if err != nil {
		return "", err
	}
	return ID(strings.TrimSpace(string(out))), nil
}

// PutBlob stores data and returns its ID (git hash-object -w).
func (s *Store) PutBlob(ctx context.Context, data []byte) (ID, error) {
	return s.id(s.git(ctx, data, "hash-object", "-w", "--stdin"))
}

// Blob returns the contents of a blob (git cat-file blob).
func (s *Store) Blob(ctx context.Context, id ID) ([]byte, error) {
	if err := s.exists(ctx, id, KindBlob); err != nil {
		return nil, err
	}
	return s.git(ctx, nil, "cat-file", "blob", string(id))
}

// ValidName reports whether name can be a tree entry: non-empty, not ".",
// "..", or ".git" in any case, and without "/" or NUL. It does no I/O, so
// it is safe to call from Temporal workflow code.
func ValidName(name string) error {
	if name == "" || name == "." || name == ".." || strings.EqualFold(name, ".git") || strings.ContainsAny(name, "/\x00") {
		return fmt.Errorf("%w: tree entry name %q", ErrInvalid, name)
	}
	return nil
}

// PutTree stores a tree and returns its ID (git mktree). Entries may be
// in any order; git sorts them. Names must pass ValidName and be unique.
func (s *Store) PutTree(ctx context.Context, entries []Entry) (ID, error) {
	seen := make(map[string]bool, len(entries))
	var in bytes.Buffer
	for _, e := range entries {
		if err := ValidName(e.Name); err != nil {
			return "", err
		}
		if seen[e.Name] {
			return "", fmt.Errorf("%w: duplicate tree entry %q", ErrInvalid, e.Name)
		}
		seen[e.Name] = true
		mode := map[Kind]string{KindBlob: "100644", KindTree: "040000"}[e.Kind]
		if mode == "" {
			return "", fmt.Errorf("%w: entry %q: unknown kind %q", ErrInvalid, e.Name, e.Kind)
		}
		if err := validID(e.ID); err != nil {
			return "", err
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

var trailerKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

func validTrailer(t Trailer) error {
	if !trailerKey.MatchString(t.Key) || t.Value == "" || strings.TrimSpace(t.Value) != t.Value || strings.ContainsAny(t.Value, "\r\n") {
		return fmt.Errorf("%w: trailer %q: %q", ErrInvalid, t.Key, t.Value)
	}
	return nil
}

// Commit stores a commit and returns its ID (git commit-tree). Trailers
// are appended with git interpret-trailers, keeping duplicates and order.
func (s *Store) Commit(ctx context.Context, c CommitSpec) (ID, error) {
	if c.Author.Name == "" || c.Author.Email == "" {
		return "", fmt.Errorf("%w: commit needs an author name and email", ErrInvalid)
	}
	if err := s.exists(ctx, c.Tree, KindTree); err != nil {
		return "", err
	}
	for _, p := range c.Parents {
		if err := s.exists(ctx, p, "commit"); err != nil {
			return "", err
		}
	}
	msg := c.Message
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	// A message whose last paragraph parses as trailers would blur into
	// the real trailers on read; ask git whether it does.
	if parsed, err := s.git(ctx, []byte(msg), "interpret-trailers", "--parse"); err != nil {
		return "", err
	} else if len(bytes.TrimSpace(parsed)) > 0 {
		return "", fmt.Errorf("%w: message ends in trailer-like lines; use CommitSpec.Trailers", ErrInvalid)
	}
	if len(c.Trailers) > 0 {
		args := []string{"interpret-trailers", "--if-exists", "add", "--if-missing", "add"}
		for _, t := range c.Trailers {
			if err := validTrailer(t); err != nil {
				return "", err
			}
			args = append(args, "--trailer", t.Key+": "+t.Value)
		}
		out, err := s.git(ctx, []byte(msg), args...)
		if err != nil {
			return "", err
		}
		msg = string(out)
	}
	args := []string{"-c", "commit.gpgSign=false", "commit-tree", string(c.Tree)}
	for _, p := range c.Parents {
		args = append(args, "-p", string(p))
	}
	date := "@" + strconv.FormatInt(c.Author.When.Unix(), 10) + " +0000"
	env := []string{
		"GIT_AUTHOR_NAME=" + c.Author.Name, "GIT_AUTHOR_EMAIL=" + c.Author.Email, "GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=" + c.Author.Name, "GIT_COMMITTER_EMAIL=" + c.Author.Email, "GIT_COMMITTER_DATE=" + date,
	}
	return s.id(gitCmd(ctx, s.dir, []byte(msg), args, env...))
}

// Resolve returns the commit a ref points to. The name must match exactly
// (git show-ref --verify): Git's usual guessing, such as trying refs/tags/
// in front of it, is not applied. ok is false if the ref does not exist.
func (s *Store) Resolve(ctx context.Context, ref string) (id ID, ok bool, err error) {
	if err := checkRef(ctx, ref); err != nil {
		return "", false, err
	}
	out, err := s.git(ctx, nil, "show-ref", "--verify", "--hash", ref)
	if err != nil {
		if strings.Contains(err.Error(), "not a valid ref") {
			return "", false, nil
		}
		return "", false, err
	}
	id = ID(strings.TrimSpace(string(out)))
	if err := s.exists(ctx, id, "commit"); err != nil {
		return "", false, fmt.Errorf("warp: ref %s points to a non-commit %s", ref, id)
	}
	return id, true, nil
}

// Advance moves ref from old to next atomically (git update-ref). An empty
// old means "create ref; it must not exist yet". next must be a commit, and
// when old is set, next must descend from it: refs only move forward.
//
// Errors: ErrConflict if the ref isn't at old or another writer holds its
// lock (re-read and retry); ErrInvalid if the update can never succeed
// (bad name, not a commit, not a fast-forward, a name clash with another
// ref). Nothing changes on error.
func (s *Store) Advance(ctx context.Context, ref string, old, next ID) error {
	if err := checkRef(ctx, ref); err != nil {
		return err
	}
	if err := s.exists(ctx, next, "commit"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	expected := old
	if old == "" {
		z, err := s.zeroID(ctx)
		if err != nil {
			return err
		}
		expected = z
	} else {
		if err := s.exists(ctx, old, "commit"); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		if ok, err := s.IsAncestor(ctx, old, next); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%w: %s is not a descendant of %s", ErrInvalid, next, old)
		}
	}
	_, err := s.git(ctx, nil, "update-ref", "-m", "warp: advance", ref, string(next), string(expected))
	if err == nil {
		return nil
	}
	// Classify by looking at the ref, not by parsing the message.
	cur, exists, rerr := s.Resolve(ctx, ref)
	switch {
	case rerr != nil:
		return err
	case old == "" && exists, old != "" && cur != old:
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case strings.Contains(err.Error(), ".lock': File exists"):
		return fmt.Errorf("%w: %v", ErrConflict, err)
	default:
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
}

// IsAncestor reports whether commit a is an ancestor of, or equal to,
// commit b (git merge-base --is-ancestor).
func (s *Store) IsAncestor(ctx context.Context, a, b ID) (bool, error) {
	if err := validID(a); err != nil {
		return false, err
	}
	if err := validID(b); err != nil {
		return false, err
	}
	_, err := s.git(ctx, nil, "merge-base", "--is-ancestor", string(a), string(b))
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return err == nil, err
}

// FindByTrailer returns the most recent commit reachable from commit from
// (including from itself) that has a trailer with exactly this key and
// value, compared case-sensitively and byte for byte (git log).
func (s *Store) FindByTrailer(ctx context.Context, from ID, key, value string) (id ID, ok bool, err error) {
	if err := validTrailer(Trailer{Key: key, Value: value}); err != nil {
		return "", false, err
	}
	if err := s.exists(ctx, from, "commit"); err != nil {
		return "", false, err
	}
	out, err := s.git(ctx, nil, "log", "--format=%H%x00%(trailers:only,unfold,separator=%x00)%x01", string(from))
	if err != nil {
		return "", false, err
	}
	want := key + ": " + value
	for _, rec := range strings.Split(string(out), "\x01") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x00")
		for _, t := range f[1:] {
			if t == want {
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

// CheckRef reports whether ref is a valid full ref name for a store: it
// starts with refs/ and passes git check-ref-format.
func CheckRef(ctx context.Context, ref string) error { return checkRef(ctx, ref) }

// === helpers ===============================================================

var hexID = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

func validID(id ID) error {
	if !hexID.MatchString(string(id)) {
		return fmt.Errorf("%w: object ID %q", ErrInvalid, id)
	}
	return nil
}

func checkRef(ctx context.Context, ref string) error {
	if !strings.HasPrefix(ref, "refs/") {
		return fmt.Errorf("%w: ref %q must start with refs/", ErrInvalid, ref)
	}
	if _, err := gitCmd(ctx, "", nil, []string{"check-ref-format", ref}); err != nil {
		return fmt.Errorf("%w: ref name %q", ErrInvalid, ref)
	}
	return nil
}

// exists checks id is a well-formed ID of an object of the given kind.
func (s *Store) exists(ctx context.Context, id ID, kind Kind) error {
	if err := validID(id); err != nil {
		return err
	}
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

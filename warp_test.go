package warp_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heedle-ai/warp"
)

var ctx = context.Background()

func newStore(t *testing.T) *warp.Store {
	t.Helper()
	s, err := warp.Init(ctx, t.TempDir()+"/store.git")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// must panics on error; in a test, a panic fails the test with a stack trace.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// gitOut runs the real git CLI against the store, as a user would.
func gitOut(t *testing.T, s *warp.Store, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"--git-dir", s.Dir()}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

var author = warp.Signature{Name: "warp", Email: "warp@heedle.ai", When: time.Unix(1700000000, 0)}

func TestBlob_ContentAddressed(t *testing.T) {
	s := newStore(t)
	a := must(s.PutBlob(ctx, []byte("hello\n")))
	b := must(s.PutBlob(ctx, []byte("hello\n")))
	c := must(s.PutBlob(ctx, []byte("hello!\n")))
	if a != b {
		t.Errorf("equal content, different IDs: %s %s", a, b)
	}
	if a == c {
		t.Errorf("different content, equal IDs")
	}
	// Git's well-known ID for "hello\n".
	if a != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Errorf("id = %s, want Git's ce01362…", a)
	}
	if h := must(warp.HashBlob(ctx, []byte("hello\n"))); h != a {
		t.Errorf("HashBlob = %s, want %s (same as PutBlob)", h, a)
	}
	if got := string(must(s.Blob(ctx, a))); got != "hello\n" {
		t.Errorf("read back %q", got)
	}
	if _, err := s.Blob(ctx, "0123456789012345678901234567890123456789"); !errors.Is(err, warp.ErrNotFound) {
		t.Errorf("missing blob err = %v, want ErrNotFound", err)
	}
}

func TestTree_RoundTripAndValidation(t *testing.T) {
	s := newStore(t)
	x := must(s.PutBlob(ctx, []byte("x")))
	sub := must(s.PutTree(ctx, []warp.Entry{{Name: "leaf", Kind: warp.KindBlob, ID: x}}))
	// Unsorted input: git sorts it, so order doesn't change the ID.
	t1 := must(s.PutTree(ctx, []warp.Entry{{Name: "z", Kind: warp.KindBlob, ID: x}, {Name: "a", Kind: warp.KindTree, ID: sub}}))
	t2 := must(s.PutTree(ctx, []warp.Entry{{Name: "a", Kind: warp.KindTree, ID: sub}, {Name: "z", Kind: warp.KindBlob, ID: x}}))
	if t1 != t2 {
		t.Errorf("entry order changed the tree ID")
	}
	got := must(s.Tree(ctx, t1))
	want := []warp.Entry{{Name: "a", Kind: warp.KindTree, ID: sub}, {Name: "z", Kind: warp.KindBlob, ID: x}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("tree = %v, want %v", got, want)
	}
	for _, bad := range [][]warp.Entry{
		{{Name: "", Kind: warp.KindBlob, ID: x}},
		{{Name: "a/b", Kind: warp.KindBlob, ID: x}},
		{{Name: "..", Kind: warp.KindBlob, ID: x}},
		{{Name: "d", Kind: warp.KindBlob, ID: x}, {Name: "d", Kind: warp.KindBlob, ID: x}},
		{{Name: "k", Kind: "commit", ID: x}},
		{{Name: ".git", Kind: warp.KindBlob, ID: x}},
		{{Name: ".GIT", Kind: warp.KindBlob, ID: x}},
		{{Name: "ok", Kind: warp.KindBlob, ID: "HEAD"}},
	} {
		if _, err := s.PutTree(ctx, bad); err == nil {
			t.Errorf("PutTree(%v) succeeded, want error", bad)
		}
	}
}

func TestCommit_DeterministicWithTrailers(t *testing.T) {
	s := newStore(t)
	tree := must(s.PutTree(ctx, nil))
	spec := warp.CommitSpec{
		Tree: tree, Message: "record run\n\nBody text.", Author: author,
		Trailers: []warp.Trailer{{Key: "Shuttle-Workflow", Value: "wf-1"}, {Key: "Shuttle-Run", Value: "run-1"}},
	}
	c1 := must(s.Commit(ctx, spec))
	c2 := must(s.Commit(ctx, spec))
	if c1 != c2 {
		t.Fatalf("same spec, different commits: %s %s", c1, c2)
	}
	ts := must(s.Trailers(ctx, c1))
	if fmt.Sprint(ts) != fmt.Sprint(spec.Trailers) {
		t.Errorf("trailers = %v, want %v", ts, spec.Trailers)
	}
	// The real git CLI sees the same thing.
	if got := gitOut(t, s, "log", "-1", "--format=%(trailers:key=Shuttle-Run,valueonly)", string(c1)); got != "run-1" {
		t.Errorf("git log trailer = %q", got)
	}
	if _, err := s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "m", Author: author,
		Trailers: []warp.Trailer{{Key: "Bad Key", Value: "v"}}}); err == nil {
		t.Error("trailer key with a space accepted")
	}
	tr, parents := must2(t, s.Parents, c1)
	if tr != tree || len(parents) != 0 {
		t.Errorf("Parents = %s %v", tr, parents)
	}
}

func must2(t *testing.T, f func(context.Context, warp.ID) (warp.ID, []warp.ID, error), id warp.ID) (warp.ID, []warp.ID) {
	t.Helper()
	a, b, err := f(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}

func commitOn(t *testing.T, s *warp.Store, msg string, parents ...warp.ID) warp.ID {
	t.Helper()
	tree := must(s.PutTree(ctx, nil))
	return must(s.Commit(ctx, warp.CommitSpec{Tree: tree, Parents: parents, Message: msg, Author: author}))
}

func TestAdvance_CompareAndSwap(t *testing.T) {
	s := newStore(t)
	const ref = "refs/agents/research"
	c1 := commitOn(t, s, "one")
	c2 := commitOn(t, s, "two", c1)
	c3 := commitOn(t, s, "three", c1)

	if _, ok := must3(t, s, ref); ok {
		t.Fatal("ref exists before creation")
	}
	if err := s.Advance(ctx, ref, "", c1); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Advance(ctx, ref, "", c2); !errors.Is(err, warp.ErrConflict) {
		t.Errorf("create over existing: err = %v, want ErrConflict", err)
	}
	if err := s.Advance(ctx, ref, c1, c2); err != nil {
		t.Fatalf("cas c1->c2: %v", err)
	}
	if err := s.Advance(ctx, ref, c1, c3); !errors.Is(err, warp.ErrConflict) {
		t.Errorf("stale cas: err = %v, want ErrConflict", err)
	}
	if head, _ := must3(t, s, ref); head != c2 {
		t.Errorf("head = %s, want c2", head)
	}
	for _, bad := range []string{"heads/main", "refs/bad..name", "refs/x y"} {
		if err := s.Advance(ctx, bad, "", c1); err == nil || errors.Is(err, warp.ErrConflict) {
			t.Errorf("Advance(%q) err = %v, want invalid-name error", bad, err)
		}
	}
}

func must3(t *testing.T, s *warp.Store, ref string) (warp.ID, bool) {
	t.Helper()
	id, ok, err := s.Resolve(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	return id, ok
}

func TestAdvance_ExactlyOneConcurrentWinner(t *testing.T) {
	s := newStore(t)
	const ref = "refs/agents/race"
	base := commitOn(t, s, "base")
	if err := s.Advance(ctx, ref, "", base); err != nil {
		t.Fatal(err)
	}
	for trial := 0; trial < 3; trial++ {
		head, _ := must3(t, s, ref)
		var wins, conflicts atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 24; i++ {
			next := commitOn(t, s, fmt.Sprintf("trial %d racer %d", trial, i), head)
			wg.Add(1)
			go func() {
				defer wg.Done()
				switch err := s.Advance(ctx, ref, head, next); {
				case err == nil:
					wins.Add(1)
				case errors.Is(err, warp.ErrConflict):
					conflicts.Add(1)
				default:
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		wg.Wait()
		if wins.Load() != 1 || conflicts.Load() != 23 {
			t.Fatalf("trial %d: %d wins, %d conflicts; want 1 and 23", trial, wins.Load(), conflicts.Load())
		}
		newHead, _ := must3(t, s, ref)
		if ok, _ := s.IsAncestor(ctx, head, newHead); !ok {
			t.Fatalf("ref moved to a commit that doesn't descend from the old head")
		}
	}
	// Losing commits are unreachable, never corrupt.
	gitOut(t, s, "fsck", "--strict")
}

func TestFindByTrailer(t *testing.T) {
	s := newStore(t)
	tree := must(s.PutTree(ctx, nil))
	var head warp.ID
	for i := 1; i <= 3; i++ {
		var parents []warp.ID
		if head != "" {
			parents = []warp.ID{head}
		}
		head = must(s.Commit(ctx, warp.CommitSpec{Tree: tree, Parents: parents, Message: "run", Author: author,
			Trailers: []warp.Trailer{{Key: "Shuttle-Run", Value: fmt.Sprintf("run-%d", i)}}}))
	}
	if err := s.Advance(ctx, "refs/runs", "", head); err != nil {
		t.Fatal(err)
	}
	id, ok, err := s.FindByTrailer(ctx, head, "Shuttle-Run", "run-2")
	if err != nil || !ok {
		t.Fatalf("find run-2: %v %v", ok, err)
	}
	if _, ps := must2(t, s.Parents, head); ps[0] != id {
		t.Errorf("found %s, want head's parent %s", id, ps[0])
	}
	if _, ok, _ := s.FindByTrailer(ctx, head, "Shuttle-Run", "run-9"); ok {
		t.Error("found a run that was never recorded")
	}
}

func TestIgnoresInheritedGitEnvironment(t *testing.T) {
	// Inside a git hook, GIT_DIR and friends point elsewhere. warp must not follow them.
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_INDEX_FILE", "/nonexistent/index")
	s := newStore(t)
	if _, err := s.PutBlob(ctx, []byte("ok")); err != nil {
		t.Fatal(err)
	}
}

func TestOpen_RejectsNonBare(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := warp.Open(ctx, dir); err == nil {
		t.Error("opened a non-bare repository")
	}
}

// === Regression tests from the independent review ==========================

func TestAdvance_OnlyFastForwardToCommits(t *testing.T) {
	s := newStore(t)
	const ref = "refs/agents/ff"
	a := commitOn(t, s, "a")
	unrelated := commitOn(t, s, "unrelated root")
	tree := must(s.PutTree(ctx, nil))
	if err := s.Advance(ctx, ref, "", a); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(ctx, ref, a, unrelated); !errors.Is(err, warp.ErrInvalid) {
		t.Errorf("move to an unrelated commit: err = %v, want ErrInvalid", err)
	}
	if err := s.Advance(ctx, ref, a, tree); !errors.Is(err, warp.ErrInvalid) {
		t.Errorf("move to a tree: err = %v, want ErrInvalid", err)
	}
	if err := s.Advance(ctx, "refs/agents/new", "", tree); !errors.Is(err, warp.ErrInvalid) {
		t.Errorf("create pointing at a tree: err = %v, want ErrInvalid", err)
	}
	if head, ok := must3(t, s, ref); !ok || head != a {
		t.Errorf("ref = %s %v, want unchanged %s", head, ok, a)
	}
}

func TestAdvance_NameClashIsNotAConflict(t *testing.T) {
	s := newStore(t)
	c := commitOn(t, s, "c")
	if err := s.Advance(ctx, "refs/loom/runs", "", c); err != nil {
		t.Fatal(err)
	}
	// refs/loom/runs is a ref, so refs/loom/runs/x can never exist.
	err := s.Advance(ctx, "refs/loom/runs/x", "", c)
	if errors.Is(err, warp.ErrConflict) || !errors.Is(err, warp.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid and not ErrConflict", err)
	}
}

func TestResolve_ExactNameNoGuessing(t *testing.T) {
	s := newStore(t)
	c := commitOn(t, s, "c")
	// Only a tag whose name ends in refs/loom/runs exists.
	gitOut(t, s, "update-ref", "refs/tags/refs/loom/runs", string(c))
	if id, ok := must3(t, s, "refs/loom/runs"); ok {
		t.Errorf("Resolve guessed %s", id)
	}
	if err := s.Advance(ctx, "refs/loom/runs", "", c); err != nil {
		t.Errorf("create after a guessable tag: %v", err)
	}
}

func TestIDs_RejectRevisionExpressions(t *testing.T) {
	s := newStore(t)
	b := must(s.PutBlob(ctx, []byte("hello\n")))
	tree := must(s.PutTree(ctx, []warp.Entry{{Name: "f", Kind: warp.KindBlob, ID: b}}))
	c := must(s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "m", Author: author}))
	if err := s.Advance(ctx, "refs/r", "", c); err != nil {
		t.Fatal(err)
	}
	for _, id := range []warp.ID{"refs/r:f", "ce01362", "HEAD", warp.ID(strings.ToUpper(string(b)))} {
		if _, err := s.Blob(ctx, id); !errors.Is(err, warp.ErrInvalid) {
			t.Errorf("Blob(%q) err = %v, want ErrInvalid", id, err)
		}
	}
}

func TestTrailers_ReadBackExactly(t *testing.T) {
	s := newStore(t)
	tree := must(s.PutTree(ctx, nil))
	want := []warp.Trailer{{Key: "K", Value: "v"}, {Key: "K", Value: "v"}, {Key: "Url", Value: "https://x/y?a=b:c"}, {Key: "A-1", Value: "two words"}}
	c := must(s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "m", Author: author, Trailers: want}))
	if got := must(s.Trailers(ctx, c)); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("trailers = %v, want %v", got, want)
	}
	for _, bad := range []warp.Trailer{{"a=b", "c"}, {"#k", "v"}, {"k", ""}, {"k", " padded"}, {"k", "padded "}, {"k y", "v"}, {"k", "two\nlines"}} {
		if _, err := s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "m", Author: author, Trailers: []warp.Trailer{bad}}); !errors.Is(err, warp.ErrInvalid) {
			t.Errorf("trailer %q accepted (err %v)", bad, err)
		}
	}
	if _, err := s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "subject\n\nSneaky: trailer", Author: author}); !errors.Is(err, warp.ErrInvalid) {
		t.Errorf("message ending in trailer-like lines accepted (err %v)", err)
	}
}

func TestFindByTrailer_ExactMatchOnly(t *testing.T) {
	s := newStore(t)
	tree := must(s.PutTree(ctx, nil))
	c := must(s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "m", Author: author,
		Trailers: []warp.Trailer{{Key: "shuttle-run", Value: "lower"}, {Key: "Shuttle-Run", Value: "abc"}}}))
	if _, ok, _ := s.FindByTrailer(ctx, c, "Shuttle-Run", "lower"); ok {
		t.Error("key matched case-insensitively")
	}
	if _, ok, _ := s.FindByTrailer(ctx, c, "Shuttle-Run", "ab"); ok {
		t.Error("value matched as a prefix")
	}
	if id, ok, _ := s.FindByTrailer(ctx, c, "Shuttle-Run", "abc"); !ok || id != c {
		t.Error("exact match not found")
	}
	if _, _, err := s.FindByTrailer(ctx, c, "Shuttle-Run,valueonly=false", "abc"); !errors.Is(err, warp.ErrInvalid) {
		t.Errorf("injected key: err = %v, want ErrInvalid", err)
	}
}

func TestCommit_RejectsDashLinesThatHideTrailers(t *testing.T) {
	s := newStore(t)
	tree := must(s.PutTree(ctx, nil))
	_, err := s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "subj\n\nbody\n---\ntail", Author: author,
		Trailers: []warp.Trailer{{Key: "Run", Value: "r1"}}})
	if !errors.Is(err, warp.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestResolve_RefToNonCommitIsInvalid(t *testing.T) {
	s := newStore(t)
	tree := must(s.PutTree(ctx, nil))
	gitOut(t, s, "update-ref", "refs/broken", string(tree))
	if _, _, err := s.Resolve(ctx, "refs/broken"); !errors.Is(err, warp.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestFindByTrailer_ValueInBodyIsNotATrailer(t *testing.T) {
	// --grep also matches the body; only real trailers count.
	s := newStore(t)
	tree := must(s.PutTree(ctx, nil))
	c := must(s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "subject\n\nmentions Run: r1 in the body\n\nmore text", Author: author}))
	if _, ok, _ := s.FindByTrailer(ctx, c, "Run", "r1"); ok {
		t.Error("matched text in the body")
	}
}

func TestLogAndDiff(t *testing.T) {
	s := newStore(t)
	var head warp.ID
	for i, text := range []string{"one\n", "two\n", "three\n"} {
		b := must(s.PutBlob(ctx, []byte(text)))
		tree := must(s.PutTree(ctx, []warp.Entry{{Name: "f", Kind: warp.KindBlob, ID: b}}))
		var parents []warp.ID
		if head != "" {
			parents = []warp.ID{head}
		}
		head = must(s.Commit(ctx, warp.CommitSpec{Tree: tree, Parents: parents, Message: fmt.Sprintf("run %d", i),
			Author:   warp.Signature{Name: "w", Email: "w@x", When: time.Unix(1700000000+int64(i), 0)},
			Trailers: []warp.Trailer{{Key: "Run", Value: fmt.Sprint(i)}, {Key: "Url", Value: "a: b"}}}))
	}
	all := must(s.Log(ctx, head, 0))
	if len(all) != 3 || all[0].ID != head || all[0].Subject != "run 2" || all[2].Subject != "run 0" {
		t.Fatalf("log = %+v", all)
	}
	if fmt.Sprint(all[0].Trailers) != "[{Run 2} {Url a: b}]" || !all[0].Time.Equal(time.Unix(1700000002, 0)) {
		t.Errorf("newest = %+v", all[0])
	}
	if len(all[0].Parents) != 1 || all[0].Parents[0] != all[1].ID || len(all[2].Parents) != 0 {
		t.Errorf("parents wrong: %+v", all)
	}
	if two := must(s.Log(ctx, head, 2)); len(two) != 2 {
		t.Errorf("limit 2 returned %d", len(two))
	}
	d := string(must(s.Diff(ctx, all[1].ID, all[0].ID)))
	if !strings.Contains(d, "-two") || !strings.Contains(d, "+three") || !strings.Contains(d, "a/f") {
		t.Errorf("diff = %q", d)
	}
	if _, err := s.Diff(ctx, "HEAD", all[0].ID); !errors.Is(err, warp.ErrInvalid) {
		t.Errorf("Diff accepted a revision expression: %v", err)
	}
}

// TestAdvance_HeldLockIsAConflict: while another writer holds a ref's lock
// file, Advance returns ErrConflict (retry later) and the ref is unchanged;
// once the lock is gone, the same Advance succeeds. Guarantees.md documents
// this for stale locks.
func TestAdvance_HeldLockIsAConflict(t *testing.T) {
	s := newStore(t)
	const ref = "refs/agents/locked"
	c1 := commitOn(t, s, "one")
	c2 := commitOn(t, s, "two", c1)
	must(0, s.Advance(ctx, ref, "", c1))
	lock := s.Dir() + "/" + ref + ".lock"
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(ctx, ref, c1, c2); !errors.Is(err, warp.ErrConflict) {
		t.Fatalf("Advance with the lock held: err = %v, want ErrConflict", err)
	}
	if head, _ := must3(t, s, ref); head != c1 {
		t.Fatalf("ref moved to %s while locked", head)
	}
	must(0, os.Remove(lock))
	if err := s.Advance(ctx, ref, c1, c2); err != nil {
		t.Fatalf("Advance after the lock is gone: %v", err)
	}
}

// TestCommit_RejectsControlCharactersInAuthor: author names and emails go
// into Git's headers, so control characters are refused, never stored.
func TestCommit_RejectsControlCharactersInAuthor(t *testing.T) {
	s := newStore(t)
	tree := must(s.PutTree(ctx, nil))
	for _, a := range []warp.Signature{
		{Name: "a\nb", Email: "a@x"}, {Name: "a", Email: "a@x\x00"}, {Name: "a\x1b[31m", Email: "a@x"},
		{Name: "a\xff", Email: "a@x"},
	} {
		a.When = time.Unix(1700000000, 0)
		if _, err := s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "m", Author: a}); !errors.Is(err, warp.ErrInvalid) {
			t.Errorf("author %q <%q>: err = %v, want ErrInvalid", a.Name, a.Email, err)
		}
	}
}

// TestInv_DiffBlobsShowsOneFile: DiffBlobs diffs exactly two blobs, says
// "Binary files" rather than printing binary content, and refuses
// anything that isn't a blob ID (a commit, a tree, a revision expression).
func TestInv_DiffBlobsShowsOneFile(t *testing.T) {
	s := newStore(t)
	a := must(s.PutBlob(ctx, []byte("rain\nMonday\n")))
	b := must(s.PutBlob(ctx, []byte("sun\nMonday\n")))
	d := string(must(s.DiffBlobs(ctx, a, b)))
	if !strings.Contains(d, "-rain") || !strings.Contains(d, "+sun") || !strings.Contains(d, " Monday") {
		t.Errorf("diff = %q", d)
	}
	x := must(s.PutBlob(ctx, []byte{0, 1, 2, 'z'}))
	y := must(s.PutBlob(ctx, []byte{0, 1, 3, 'z'}))
	if d := string(must(s.DiffBlobs(ctx, x, y))); !strings.Contains(d, "Binary files") || strings.Contains(d, "z") {
		t.Errorf("binary diff = %q", d)
	}
	tree := must(s.PutTree(ctx, []warp.Entry{{Name: "f", Kind: warp.KindBlob, ID: a}}))
	c := must(s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: "m", Author: warp.Signature{Name: "w", Email: "w@x", When: time.Unix(1700000000, 0)}}))
	for _, bad := range []warp.ID{tree, c, "HEAD", warp.ID(string(a) + ":f"), ""} {
		if _, err := s.DiffBlobs(ctx, bad, b); err == nil {
			t.Errorf("DiffBlobs accepted %q", bad)
		}
	}
}

// TestInv_FilesListsATreeInOneProcess: Files lists every blob at any
// depth with its path and size, leaves out other entries, and runs one
// git process for the listing however many directories there are;
// BlobSize agrees with the content's length. (Review 9: walking a tree
// cost two processes a directory.)
func TestInv_FilesListsATreeInOneProcess(t *testing.T) {
	s := newStore(t)
	a := must(s.PutBlob(ctx, []byte("hello")))
	e := must(s.PutBlob(ctx, nil))
	dir := must(s.PutTree(ctx, []warp.Entry{{Name: "a b", Kind: warp.KindBlob, ID: a}}))
	for i := 0; i < 30; i++ {
		dir = must(s.PutTree(ctx, []warp.Entry{{Name: fmt.Sprintf("d%d", i), Kind: warp.KindTree, ID: dir}}))
	}
	root := must(s.PutTree(ctx, []warp.Entry{{Name: "deep", Kind: warp.KindTree, ID: dir}, {Name: "two\nlines", Kind: warp.KindBlob, ID: e}}))
	calls := countGit(t)
	fs := must(s.Files(ctx, root))
	if n := calls(); n != 2 { // the tree check, then the listing
		t.Errorf("Files ran %d git processes, want 2", n)
	}
	deep := "deep/" + func() string {
		var p []string
		for i := 29; i >= 0; i-- {
			p = append(p, fmt.Sprintf("d%d", i))
		}
		return strings.Join(p, "/")
	}() + "/a b"
	want := []warp.File{{Path: deep, ID: a, Size: 5}, {Path: "two\nlines", ID: e, Size: 0}}
	if fmt.Sprint(fs) != fmt.Sprint(want) {
		t.Errorf("files %v", fs)
	}
	if n, err := s.BlobSize(ctx, a); err != nil || n != 5 {
		t.Errorf("size %d %v", n, err)
	}
	for _, bad := range []warp.ID{root, "HEAD", ""} {
		if _, err := s.BlobSize(ctx, bad); err == nil {
			t.Errorf("BlobSize accepted %q", bad)
		}
	}
	if _, err := s.Files(ctx, a); err == nil {
		t.Error("Files accepted a blob")
	}
}

// countGit puts a git on PATH that logs each run before running the real
// one, and returns how many runs there have been since.
func countGit(t *testing.T) func() int {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "runs")
	script := fmt.Sprintf("#!/bin/sh\necho run >> %q\nexec %q \"$@\"\n", log, real)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		b, _ := os.ReadFile(log)
		return strings.Count(string(b), "run")
	}
}

package warp_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
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
	id, ok, err := s.FindByTrailer(ctx, "refs/runs", "Shuttle-Run", "run-2")
	if err != nil || !ok {
		t.Fatalf("find run-2: %v %v", ok, err)
	}
	if _, ps := must2(t, s.Parents, head); ps[0] != id {
		t.Errorf("found %s, want head's parent %s", id, ps[0])
	}
	if _, ok, _ := s.FindByTrailer(ctx, "refs/runs", "Shuttle-Run", "run-9"); ok {
		t.Error("found a run that was never recorded")
	}
	if _, ok, _ := s.FindByTrailer(ctx, "refs/none", "Shuttle-Run", "run-1"); ok {
		t.Error("found a run on a missing ref")
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

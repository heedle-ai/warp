package warp_test

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/heedle-ai/warp"
)

// flat builds a tree of blobs from name -> content.
func flat(t *testing.T, s *warp.Store, files map[string]string) warp.ID {
	t.Helper()
	var es []warp.Entry
	for name, content := range files {
		es = append(es, warp.Entry{Name: name, Kind: warp.KindBlob, ID: must(s.PutBlob(ctx, []byte(content)))})
	}
	return must(s.PutTree(ctx, es))
}

// read is the inverse of flat.
func read(t *testing.T, s *warp.Store, tree warp.ID) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, e := range must(s.Tree(ctx, tree)) {
		out[e.Name] = string(must(s.Blob(ctx, e.ID)))
	}
	return out
}

// threeWay is the specification git merge-tree must meet on flat trees of
// whole files: per path, take the side that changed; equal changes merge;
// different changes conflict.
func threeWay(base, ours, theirs map[string]string) (map[string]string, []string) {
	names := map[string]bool{}
	for _, m := range []map[string]string{base, ours, theirs} {
		for k := range m {
			names[k] = true
		}
	}
	out := map[string]string{}
	var conflicts []string
	for n := range names {
		b, bok := base[n]
		o, ook := ours[n]
		th, tok := theirs[n]
		switch {
		case ook == tok && o == th:
			if ook {
				out[n] = o
			}
		case ook == bok && o == b:
			if tok {
				out[n] = th
			}
		case tok == bok && th == b:
			if ook {
				out[n] = o
			}
		default:
			conflicts = append(conflicts, n)
		}
	}
	sort.Strings(conflicts)
	return out, conflicts
}

// TestLaw_MergeTreesIsThreeWayMerge: on random flat trees where neither
// side deletes a path (how loom uses it), git merge-tree (through
// MergeTrees) agrees exactly with the path-by-path three-way merge
// specification, for clean merges and for the set of conflicts.
func TestLaw_MergeTreesIsThreeWayMerge(t *testing.T) {
	s := newStore(t)
	r := rand.New(rand.NewSource(1))
	gen := func(from map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range from {
			m[k] = v // append-only: never delete a path
		}
		for i := 0; i < r.Intn(3); i++ {
			m[fmt.Sprintf("q%d", r.Intn(6))] = fmt.Sprintf("run %d\n", r.Intn(3))
		}
		return m
	}
	for i := 0; i < 60; i++ {
		base := gen(nil)
		ours, theirs := gen(base), gen(base)
		want, wantConflicts := threeWay(base, ours, theirs)
		tree, conflicts, err := s.MergeTrees(ctx, flat(t, s, base), flat(t, s, ours), flat(t, s, theirs))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(conflicts, wantConflicts) && !(len(conflicts) == 0 && len(wantConflicts) == 0) {
			t.Fatalf("base %v ours %v theirs %v: conflicts %v, want %v", base, ours, theirs, conflicts, wantConflicts)
		}
		got := read(t, s, tree)
		for _, c := range conflicts {
			delete(got, c)
		}
		if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Fatalf("base %v ours %v theirs %v: merged %v, want %v", base, ours, theirs, got, want)
		}
	}
}

// TestMergeTrees_RenamesAreFollowed documents Git's behaviour that the
// path-by-path rule depends on avoiding: deleting a file and adding the
// same content under another name is a rename, and the other side's change
// follows it. This is why loom never deletes a path from its index.
func TestMergeTrees_RenamesAreFollowed(t *testing.T) {
	s := newStore(t)
	content := "the same long content that rename detection would match\n"
	base := flat(t, s, map[string]string{"q1": content})
	ours := flat(t, s, map[string]string{"q5": content})
	theirs := flat(t, s, map[string]string{"q1": "changed\n"})
	tree, conflicts, err := s.MergeTrees(ctx, base, ours, theirs)
	if err != nil || len(conflicts) != 0 {
		t.Fatal(conflicts, err)
	}
	if got := read(t, s, tree); !reflect.DeepEqual(got, map[string]string{"q5": "changed\n"}) {
		t.Fatalf("merged = %v; Git's rename following changed, update the docs", got)
	}
}

func TestMergeTrees_EmptyBaseAndValidation(t *testing.T) {
	s := newStore(t)
	a := flat(t, s, map[string]string{"x": "1\n"})
	b := flat(t, s, map[string]string{"y": "2\n"})
	tree, conflicts, err := s.MergeTrees(ctx, "", a, b)
	if err != nil || len(conflicts) != 0 || !reflect.DeepEqual(read(t, s, tree), map[string]string{"x": "1\n", "y": "2\n"}) {
		t.Fatalf("unrelated merge = %v %v %v", read(t, s, tree), conflicts, err)
	}
	blob := must(s.PutBlob(ctx, []byte("x")))
	if _, _, err := s.MergeTrees(ctx, "", blob, b); !errors.Is(err, warp.ErrNotFound) {
		t.Errorf("blob as a tree: err = %v, want ErrNotFound", err)
	}
	if _, _, err := s.MergeTrees(ctx, "", "HEAD", b); !errors.Is(err, warp.ErrInvalid) {
		t.Errorf("revision expression: err = %v, want ErrInvalid", err)
	}
}

func TestMergeBases(t *testing.T) {
	s := newStore(t)
	root := commitOn(t, s, "root")
	a := commitOn(t, s, "a", root)
	b := commitOn(t, s, "b", root)
	if got := must(s.MergeBases(ctx, a, b)); !reflect.DeepEqual(got, []warp.ID{root}) {
		t.Errorf("bases(a, b) = %v, want root", got)
	}
	if got := must(s.MergeBases(ctx, a, a)); !reflect.DeepEqual(got, []warp.ID{a}) {
		t.Errorf("bases(a, a) = %v, want a", got)
	}
	other := commitOn(t, s, "unrelated")
	if got := must(s.MergeBases(ctx, a, other)); len(got) != 0 {
		t.Errorf("unrelated = %v, want none", got)
	}
	// Criss-cross: x and y each merge both a and b.
	x := commitOn(t, s, "x", a, b)
	y := commitOn(t, s, "y", b, a)
	if got := must(s.MergeBases(ctx, x, y)); len(got) != 2 {
		t.Errorf("criss-cross bases = %v, want two", got)
	}
	if _, err := s.MergeBases(ctx, "main", a); !errors.Is(err, warp.ErrInvalid) {
		t.Errorf("revision expression: err = %v", err)
	}
}

func TestRefs(t *testing.T) {
	s := newStore(t)
	c := commitOn(t, s, "c")
	must(0, s.Advance(ctx, "refs/loom/runs", "", c))
	must(0, s.Advance(ctx, "refs/loom/what-if", "", c))
	must(0, s.Advance(ctx, "refs/other/x", "", c))
	got := must(s.Refs(ctx, "refs/loom/"))
	want := []warp.RefInfo{{Name: "refs/loom/runs", Commit: c}, {Name: "refs/loom/what-if", Commit: c}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Refs = %v, want %v", got, want)
	}
	for _, bad := range []string{"refs/loom", "loom/", "refs/a..b/", "refs/*/"} {
		if _, err := s.Refs(ctx, bad); !errors.Is(err, warp.ErrInvalid) {
			t.Errorf("Refs(%q) err = %v, want ErrInvalid", bad, err)
		}
	}
}

// TestMergeTrees_MultiLineFilesMergeByLine documents that MergeTrees is
// Git's line-by-line merge: changes to different lines of one file both
// land, giving a file neither side wrote. (Review 4: the docs claimed a
// whole-entry rule.) loom only merges one-line entries for this reason.
func TestMergeTrees_MultiLineFilesMergeByLine(t *testing.T) {
	s := newStore(t)
	base := flat(t, s, map[string]string{"f": "1\n2\n3\n4\n5\n6\n7\n"})
	ours := flat(t, s, map[string]string{"f": "OURS\n2\n3\n4\n5\n6\n7\n"})
	theirs := flat(t, s, map[string]string{"f": "1\n2\n3\n4\n5\n6\nTHEIRS\n"})
	tree, conflicts, err := s.MergeTrees(ctx, base, ours, theirs)
	if err != nil || len(conflicts) != 0 {
		t.Fatal(conflicts, err)
	}
	if got := read(t, s, tree)["f"]; got != "OURS\n2\n3\n4\n5\n6\nTHEIRS\n" {
		t.Fatalf("merged %q", got)
	}
}

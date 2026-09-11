package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// graphFixture keeps graph benchmarks independent of repository I/O.
type graphFixture struct {
	branchReader
	branches Branches
	parents  map[string]string
	current  string
}

func (f *graphFixture) AllBranches() Branches { return f.branches }
func (f *graphFixture) Trunk() Branch         { return NewBranch("main", f) }
func (f *graphFixture) CurrentBranch() *Branch {
	if f.current == "" {
		return nil
	}
	b := NewBranch(f.current, f)
	return &b
}
func (f *graphFixture) GetParent(b Branch) *Branch {
	name := f.parents[b.GetName()]
	if name == "" {
		return nil
	}
	parent := NewBranch(name, f)
	return &parent
}

func newGraphFixture(depth int, siblings bool) *graphFixture {
	f := &graphFixture{parents: make(map[string]string)}
	f.branches = append(f.branches, f.Trunk())
	parent := "main"
	for i := range depth {
		name := fmt.Sprintf("branch-%04d", i)
		f.parents[name] = parent
		f.branches = append(f.branches, NewBranch(name, f))
		if siblings {
			sibling := fmt.Sprintf("sibling-%04d", i)
			f.parents[sibling] = parent
			f.branches = append(f.branches, NewBranch(sibling, f))
		}
		parent = name
	}
	f.current = parent
	return f
}

func TestStackGraphSmartSortActivePath(t *testing.T) {
	t.Parallel()
	for _, current := range []string{"branch-0002", "", "missing"} {
		t.Run(current, func(t *testing.T) {
			t.Parallel()
			f := newGraphFixture(3, true)
			f.current = current
			g := BuildStackGraph(f, SortStrategySmart, nil)
			parent := "main"
			for i := range 3 {
				branch := fmt.Sprintf("branch-%04d", i)
				sibling := fmt.Sprintf("sibling-%04d", i)
				want := []string{sibling, branch}
				if current == "branch-0002" {
					want = []string{branch, sibling}
				}
				require.Equal(t, want, g.GetNode(parent).Children)
				parent = branch
			}
		})
	}
}

func TestStackGraphDeepGroups(t *testing.T) {
	t.Parallel()
	f := newGraphFixture(100, true)
	g := BuildStackGraph(f, SortStrategyAlphabetical, nil)
	groups := g.DepthGroups()
	require.Len(t, groups, 101)
	require.Equal(t, []string{"main"}, groups[0].Branches.Names())
	for depth := 1; depth <= 100; depth++ {
		require.Equal(t, depth, groups[depth].Depth)
		require.Equal(t, []string{
			fmt.Sprintf("branch-%04d", depth-1),
			fmt.Sprintf("sibling-%04d", depth-1),
		}, groups[depth].Branches.Names())
	}
}

func BenchmarkStackGraphSmartSort(b *testing.B) {
	for _, depth := range []int{100, 1000} {
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			f := newGraphFixture(depth, true)
			b.ReportAllocs()
			for b.Loop() {
				BuildStackGraph(f, SortStrategySmart, nil)
			}
		})
	}
}

func BenchmarkStackGraphDepthGroups(b *testing.B) {
	for _, depth := range []int{100, 1000} {
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			f := newGraphFixture(depth, false)
			g := BuildStackGraph(f, SortStrategyAlphabetical, nil)
			b.ReportAllocs()
			for b.Loop() {
				g.DepthGroups()
			}
		})
	}
}

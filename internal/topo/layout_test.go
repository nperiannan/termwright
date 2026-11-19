package topo

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"testing"
)

func boxes(ids ...string) []LNode {
	var out []LNode
	for _, id := range ids {
		out = append(out, LNode{ID: id, W: 160, H: 60})
	}
	return out
}

func edges(pairs ...string) []LEdge {
	var out []LEdge
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, LEdge{pairs[i], pairs[i+1]})
	}
	return out
}

func at(r LayoutResult, id string) LNode {
	for _, n := range r.Nodes {
		if n.ID == id {
			return n
		}
	}
	panic("no node " + id)
}

func noOverlap(t *testing.T, name string, ns []LNode) {
	t.Helper()
	for i := 0; i < len(ns); i++ {
		for j := i + 1; j < len(ns); j++ {
			a, b := ns[i], ns[j]
			if math.Abs(a.X-b.X) < (a.W+b.W)/2 && math.Abs(a.Y-b.Y) < (a.H+b.H)/2 {
				t.Errorf("%s: %s and %s overlap (%v,%v) (%v,%v)", name, a.ID, b.ID, a.X, a.Y, b.X, b.Y)
			}
		}
	}
}

// a campus: two cores (a full link between them), two distribution boxes each linked to both cores, access boxes under them, two ghosts
func campus() ([]LNode, []LEdge) {
	ns := boxes("core1", "core2", "dist1", "dist2", "dist3", "acc1", "acc2", "acc3", "acc4")
	ns = append(ns, LNode{ID: "ap", W: 160, H: 60, Ghost: true}, LNode{ID: "nas", W: 160, H: 60, Ghost: true})
	es := edges("core1", "core2", "core1", "dist1", "core1", "dist2", "core1", "dist3", "core2", "dist1", "core2", "dist2", "core2", "dist3",
		"dist1", "acc1", "dist1", "acc2", "dist2", "acc3", "dist2", "acc4", "acc1", "ap", "acc4", "nas")
	return ns, es
}

func TestEveryStyleHasNoOverlapAndIsStable(t *testing.T) {
	ns, es := campus()
	for _, st := range []string{"auto", "layered", "spine-leaf", "tree", "star", "ring", "mesh", "grid", "organic"} {
		r := Layout(LayoutRequest{Style: st, Nodes: ns, Edges: es})
		if len(r.Nodes) != len(ns) {
			t.Fatalf("%s: %d boxes", st, len(r.Nodes))
		}
		noOverlap(t, st, r.Nodes)
		for _, n := range r.Nodes {
			if math.IsNaN(n.X) || math.IsNaN(n.Y) || math.IsInf(n.X, 0) || math.IsInf(n.Y, 0) {
				t.Fatalf("%s: %s at %v,%v", st, n.ID, n.X, n.Y)
			}
		}
		if again := Layout(LayoutRequest{Style: st, Nodes: ns, Edges: es}); !reflect.DeepEqual(r, again) {
			t.Errorf("%s: the same input gave another picture", st)
		}
		if st != "auto" && r.Style != st {
			t.Errorf("%s answered as %s", st, r.Style)
		}
	}
}

func TestLayeredPutsTheBestConnectedOnTopAndGhostsLast(t *testing.T) {
	ns, es := campus()
	r := Layout(LayoutRequest{Style: "layered", Nodes: ns, Edges: es})
	core := at(r, "core1") // core1 has 4 links, as dist1 does: the tie goes to the more central box, then the name
	if !(core.Y < at(r, "dist1").Y && at(r, "dist1").Y < at(r, "acc1").Y) {
		t.Fatalf("rows: core1 %v dist1 %v acc1 %v", core.Y, at(r, "dist1").Y, at(r, "acc1").Y)
	}
	if at(r, "ap").Y <= at(r, "acc1").Y || at(r, "nas").Y <= at(r, "acc4").Y {
		t.Fatal("a ghost must be below the box that sees it")
	}
	if c := Crossings(r.Nodes, es); c != 0 {
		t.Fatalf("a campus tree-like graph should have no crossings, got %d", c)
	}
}

func TestSpineLeaf(t *testing.T) {
	ns := boxes("s1", "s2", "l1", "l2", "l3", "l4")
	es := edges("s1", "l1", "s1", "l2", "s1", "l3", "s1", "l4", "s2", "l1", "s2", "l2", "s2", "l3", "s2", "l4")
	r := Layout(LayoutRequest{Style: "spine-leaf", Nodes: ns, Edges: es})
	for _, s := range []string{"s1", "s2"} {
		for _, l := range []string{"l1", "l2", "l3", "l4"} {
			if at(r, s).Y >= at(r, l).Y {
				t.Fatalf("spine %s must be above leaf %s", s, l)
			}
		}
	}
	if at(r, "s1").Y != at(r, "s2").Y || at(r, "l1").Y != at(r, "l4").Y {
		t.Fatal("spines are one row, leaves another")
	}
	noOverlap(t, "spine-leaf", r.Nodes)
}

func TestRingFollowsTheCable(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e", "f"}
	es := edges("a", "b", "b", "c", "c", "d", "d", "e", "e", "f", "f", "a")
	r := Layout(LayoutRequest{Style: "ring", Nodes: boxes(ids...), Edges: es})
	cx, cy := 0.0, 0.0
	for _, n := range r.Nodes {
		cx += n.X / 6
		cy += n.Y / 6
	}
	rad := math.Hypot(at(r, "a").X-cx, at(r, "a").Y-cy)
	for _, n := range r.Nodes {
		if d := math.Hypot(n.X-cx, n.Y-cy); math.Abs(d-rad) > 1 {
			t.Fatalf("%s is %v from the centre, ring radius %v", n.ID, d, rad)
		}
	}
	// every cable joins neighbours on the ring: all links have the same (shortest) length
	var l0 float64
	for k, e := range es {
		d := math.Hypot(at(r, e.A).X-at(r, e.B).X, at(r, e.A).Y-at(r, e.B).Y)
		if k == 0 {
			l0 = d
		}
		if math.Abs(d-l0) > 1 {
			t.Fatalf("link %v is %v long, others %v: the ring order does not follow the cables", e, d, l0)
		}
	}
	if Crossings(r.Nodes, es) != 0 {
		t.Fatal("a ring has no crossings")
	}
	// the same ring given in scrambled name order still follows the cable
	ids2 := []string{"a", "d", "b", "e", "c", "f"}
	es2 := edges("a", "d", "d", "b", "b", "e", "e", "c", "c", "f", "f", "a")
	if r2 := Layout(LayoutRequest{Style: "ring", Nodes: boxes(ids2...), Edges: es2}); Crossings(r2.Nodes, es2) != 0 {
		t.Fatalf("scrambled ring crosses %d times", Crossings(r2.Nodes, es2))
	}
}

func TestStarHubInTheMiddle(t *testing.T) {
	ns := boxes("hub", "a", "b", "c", "d", "far")
	es := edges("hub", "a", "hub", "b", "hub", "c", "hub", "d", "a", "far")
	r := Layout(LayoutRequest{Style: "star", Nodes: ns, Edges: es})
	h := at(r, "hub")
	da := math.Hypot(at(r, "a").X-h.X, at(r, "a").Y-h.Y)
	for _, id := range []string{"b", "c", "d"} {
		if d := math.Hypot(at(r, id).X-h.X, at(r, id).Y-h.Y); math.Abs(d-da) > 1 {
			t.Fatalf("%s at %v from the hub, a at %v", id, d, da)
		}
	}
	if math.Hypot(at(r, "far").X-h.X, at(r, "far").Y-h.Y) <= da {
		t.Fatal("a box two links away must be further out")
	}
	// the hub can be chosen
	r = Layout(LayoutRequest{Style: "star", Nodes: ns, Edges: es, Root: "a"})
	a := at(r, "a")
	if da := math.Hypot(at(r, "far").X-a.X, at(r, "far").Y-a.Y); da > math.Hypot(at(r, "b").X-a.X, at(r, "b").Y-a.Y) {
		t.Fatal("with a as hub, far (one link) must be nearer than b (two links)")
	}
}

func TestTreeParentsAboveAndCentred(t *testing.T) {
	ns := boxes("root", "a", "b", "a1", "a2", "b1")
	es := edges("root", "a", "root", "b", "a", "a1", "a", "a2", "b", "b1")
	r := Layout(LayoutRequest{Style: "tree", Nodes: ns, Edges: es, Root: "root"})
	if !(at(r, "root").Y < at(r, "a").Y && at(r, "a").Y < at(r, "a1").Y) {
		t.Fatal("rows")
	}
	if math.Abs(at(r, "a").X-(at(r, "a1").X+at(r, "a2").X)/2) > 1 {
		t.Fatal("a is not centred over its children")
	}
	if math.Abs(at(r, "root").X-(at(r, "a").X+at(r, "b").X)/2) > 1 {
		t.Fatal("root is not centred over a and b")
	}
	if Crossings(r.Nodes, es) != 0 {
		t.Fatal("a tree has no crossings")
	}
}

func TestMeshAndGrid(t *testing.T) {
	ns := boxes("a", "b", "c", "d", "e")
	var es []LEdge
	for i := 0; i < 5; i++ {
		for j := i + 1; j < 5; j++ {
			es = append(es, LEdge{string(rune('a' + i)), string(rune('a' + j))})
		}
	}
	r := Layout(LayoutRequest{Style: "mesh", Nodes: ns, Edges: es})
	noOverlap(t, "mesh", r.Nodes)
	g := Layout(LayoutRequest{Style: "grid", Nodes: boxes("a", "b", "c", "d", "e", "f", "g"), Edges: nil})
	cells := map[[2]float64]bool{}
	for _, n := range g.Nodes {
		cells[[2]float64{n.X, n.Y}] = true
	}
	if len(cells) != 7 {
		t.Fatalf("grid cells: %d", len(cells))
	}
}

func TestSeparateGroupsSitSideBySideAndPinnedStay(t *testing.T) {
	ns := boxes("a1", "a2", "b1", "b2", "lone")
	es := edges("a1", "a2", "b1", "b2")
	r := Layout(LayoutRequest{Style: "layered", Nodes: ns, Edges: es})
	noOverlap(t, "groups", r.Nodes)
	// pinned boxes keep their place, the others go clear of them
	ns[0].Pinned, ns[0].X, ns[0].Y = true, 500, 500
	r = Layout(LayoutRequest{Style: "layered", Nodes: ns, Edges: es})
	if p := at(r, "a1"); p.X != 500 || p.Y != 500 {
		t.Fatalf("pinned moved to %v,%v", p.X, p.Y)
	}
	noOverlap(t, "pinned", r.Nodes)
	// a box pinned over where another would go: the other is pushed off it
	ns2 := boxes("a", "b")
	ns2[1].Pinned, ns2[1].X, ns2[1].Y = true, 0, 0
	r = Layout(LayoutRequest{Style: "grid", Nodes: ns2, Edges: nil})
	noOverlap(t, "pinned over", r.Nodes)
	if p := at(r, "b"); p.X != 0 || p.Y != 0 {
		t.Fatal("pinned moved")
	}
}

func TestAutoPicksFewCrossings(t *testing.T) {
	// a ring: auto must not choose a picture with crossings
	ids := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	var es []LEdge
	for i := range ids {
		es = append(es, LEdge{ids[i], ids[(i+1)%len(ids)]})
	}
	r := Layout(LayoutRequest{Style: "auto", Nodes: boxes(ids...), Edges: es})
	if c := Crossings(r.Nodes, es); c != 0 {
		t.Fatalf("auto chose %s with %d crossings", r.Style, c)
	}
	// a tree-like campus: layered
	ns, ce := campus()
	r = Layout(LayoutRequest{Style: "auto", Nodes: ns, Edges: ce})
	if c := Crossings(r.Nodes, ce); c != 0 {
		t.Fatalf("auto on the campus: %s with %d crossings", r.Style, c)
	}
	if r.Style != "layered" && r.Style != "tree" {
		t.Fatalf("a campus should come out as layered or tree, not %s", r.Style)
	}
}

func TestLayoutEdgeCases(t *testing.T) {
	if r := Layout(LayoutRequest{Style: "auto"}); len(r.Nodes) != 0 {
		t.Fatalf("%+v", r)
	}
	one := Layout(LayoutRequest{Style: "ring", Nodes: boxes("only")})
	if len(one.Nodes) != 1 {
		t.Fatal("one box")
	}
	// an edge to a box that is not there, and a self link, are ignored
	r := Layout(LayoutRequest{Style: "layered", Nodes: boxes("a", "b"), Edges: edges("a", "zzz", "b", "b", "a", "b")})
	noOverlap(t, "bad edges", r.Nodes)
	// many boxes
	var ns []LNode
	var es []LEdge
	for i := 0; i < 80; i++ {
		ns = append(ns, LNode{ID: "n" + strconv.Itoa(i), W: 160, H: 60})
		if i > 0 {
			es = append(es, LEdge{"n" + strconv.Itoa((i-1)/3), "n" + strconv.Itoa(i)})
		}
	}
	for _, st := range []string{"auto", "layered", "tree", "organic", "star"} {
		r := Layout(LayoutRequest{Style: st, Nodes: ns, Edges: es})
		noOverlap(t, fmt.Sprint(st, " x80"), r.Nodes)
	}
}

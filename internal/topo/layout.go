package topo

import (
	"math"
	"sort"
)

// LNode is a box to place; X and Y (its centre) are the result, or the fixed place of a Pinned box.
type LNode struct {
	ID     string  `json:"id"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Pinned bool    `json:"pinned,omitempty"`
	// Ghost: seen only by a neighbour; kept at the edge (the bottom row of layered styles).
	Ghost bool `json:"ghost,omitempty"`
}

// LEdge joins two boxes.
type LEdge struct {
	A string `json:"a"`
	B string `json:"b"`
}

// LayoutRequest asks for an arrangement. Style: auto, layered, spine-leaf, tree, ring, star, mesh, grid,
// organic. Root is the box a tree or star grows from ("" = the best connected one).
type LayoutRequest struct {
	Style string  `json:"style"`
	Nodes []LNode `json:"nodes"`
	Edges []LEdge `json:"edges"`
	Root  string  `json:"root,omitempty"`
	// Width is the room to the right before rows of separate groups wrap (0 = 1400).
	Width float64 `json:"width,omitempty"`
}

// LayoutResult has every box with its place, and the style used (what "auto" chose).
type LayoutResult struct {
	Style string  `json:"style"`
	Nodes []LNode `json:"nodes"`
}

const gap = 56.0

type lg struct {
	nodes []LNode
	idx   map[string]int
	adj   [][]int
	deg   []int
}

func newLG(nodes []LNode, edges []LEdge) *lg {
	g := &lg{nodes: append([]LNode(nil), nodes...), idx: map[string]int{}}
	sort.SliceStable(g.nodes, func(i, j int) bool { return g.nodes[i].ID < g.nodes[j].ID })
	for i, n := range g.nodes {
		g.idx[n.ID] = i
	}
	g.adj = make([][]int, len(g.nodes))
	seen := map[[2]int]bool{}
	for _, e := range edges {
		a, ok1 := g.idx[e.A]
		b, ok2 := g.idx[e.B]
		if !ok1 || !ok2 || a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		if seen[[2]int{a, b}] {
			continue
		}
		seen[[2]int{a, b}] = true
		g.adj[a] = append(g.adj[a], b)
		g.adj[b] = append(g.adj[b], a)
	}
	g.deg = make([]int, len(g.nodes))
	for i := range g.adj {
		sort.Ints(g.adj[i])
		g.deg[i] = len(g.adj[i])
	}
	return g
}

func (g *lg) components() [][]int {
	seen := make([]bool, len(g.nodes))
	var out [][]int
	for s := range g.nodes {
		if seen[s] {
			continue
		}
		var c []int
		q := []int{s}
		seen[s] = true
		for len(q) > 0 {
			u := q[0]
			q = q[1:]
			c = append(c, u)
			for _, v := range g.adj[u] {
				if !seen[v] {
					seen[v] = true
					q = append(q, v)
				}
			}
		}
		sort.Ints(c)
		out = append(out, c)
	}
	// the biggest group first
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// best is the best connected non-ghost box of a group; among equals the more central one (the smaller
// distance to the farthest box), then the first by ID.
func (g *lg) best(c []int, root string) int {
	if r, ok := g.idx[root]; ok {
		for _, i := range c {
			if i == r {
				return r
			}
		}
	}
	ecc := func(i int) int {
		d, _ := g.bfs(i, c)
		m := 0
		for _, v := range d {
			m = max(m, v)
		}
		return m
	}
	b, be := -1, 0
	for _, i := range c {
		if b < 0 {
			b, be = i, -1
		}
		switch {
		case g.nodes[b].Ghost && !g.nodes[i].Ghost, g.nodes[b].Ghost == g.nodes[i].Ghost && g.deg[i] > g.deg[b]:
			b, be = i, -1
		case g.nodes[b].Ghost == g.nodes[i].Ghost && g.deg[i] == g.deg[b] && i != b:
			if be < 0 {
				be = ecc(b)
			}
			if e := ecc(i); e < be {
				b, be = i, e
			}
		}
	}
	return b
}

func (g *lg) bfs(root int, c []int) (depth map[int]int, parent map[int]int) {
	depth, parent = map[int]int{root: 0}, map[int]int{root: -1}
	q := []int{root}
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		for _, v := range g.adj[u] {
			if _, ok := depth[v]; !ok {
				depth[v], parent[v] = depth[u]+1, u
				q = append(q, v)
			}
		}
	}
	return
}

type pt struct{ x, y float64 }

// layoutGroup places the boxes of one connected group around (0,0): a top-left of 0,0 for the group.
func (g *lg) layoutGroup(style string, c []int, root string) map[int]pt {
	switch style {
	case "ring":
		return g.ring(c, g.ringOrder(c))
	case "mesh":
		o := append([]int(nil), c...)
		sort.SliceStable(o, func(a, b int) bool { return g.deg[o[a]] > g.deg[o[b]] })
		return g.ring(c, o)
	case "star":
		return g.star(c, root)
	case "spine-leaf":
		return g.spineLeaf(c)
	case "tree":
		return g.tree(c, root)
	case "grid":
		return g.grid(c)
	case "organic":
		return g.organic(c)
	}
	return g.layered(c, root)
}

func (g *lg) maxW(c []int) (w, h float64) {
	for _, i := range c {
		w, h = math.Max(w, g.nodes[i].W), math.Max(h, g.nodes[i].H)
	}
	return
}

// layered: rows by distance from the best connected box, ordered to cross as little as possible.
func (g *lg) layered(c []int, root string) map[int]pt {
	r := g.best(c, root)
	depth, _ := g.bfs(r, c)
	rows := map[int][]int{}
	maxd := 0
	for _, i := range c {
		d := depth[i]
		maxd = max(maxd, d)
		rows[d] = append(rows[d], i)
	}
	// a ghost sits one row below the box that sees it, never above
	return g.placeRows(c, rows, maxd)
}

func (g *lg) placeRows(c []int, rows map[int][]int, maxd int) map[int]pt {
	pos := map[int]float64{}
	for d := 0; d <= maxd; d++ {
		sort.SliceStable(rows[d], func(a, b int) bool {
			x, y := rows[d][a], rows[d][b]
			if g.nodes[x].Ghost != g.nodes[y].Ghost {
				return !g.nodes[x].Ghost
			}
			return g.nodes[x].ID < g.nodes[y].ID
		})
		for k, i := range rows[d] {
			pos[i] = float64(k)
		}
	}
	rowOf := map[int]int{}
	for d := 0; d <= maxd; d++ {
		for _, i := range rows[d] {
			rowOf[i] = d
		}
	}
	bary := func(i, other int) (float64, bool) {
		s, n := 0.0, 0
		for _, v := range g.adj[i] {
			if rowOf[v] == other {
				s += pos[v]
				n++
			}
		}
		return s / math.Max(1, float64(n)), n > 0
	}
	for it := 0; it < 6; it++ {
		for pass := 0; pass < 2; pass++ {
			ds := []int{}
			for d := 0; d <= maxd; d++ {
				ds = append(ds, d)
			}
			if pass == 1 {
				for a, b := 0, len(ds)-1; a < b; a, b = a+1, b-1 {
					ds[a], ds[b] = ds[b], ds[a]
				}
			}
			for _, d := range ds {
				other := d - 1
				if pass == 1 {
					other = d + 1
				}
				row := rows[d]
				keys := map[int]float64{}
				for _, i := range row {
					if b, ok := bary(i, other); ok {
						keys[i] = b
					} else {
						keys[i] = pos[i]
					}
				}
				sort.SliceStable(row, func(a, b int) bool {
					x, y := row[a], row[b]
					if g.nodes[x].Ghost != g.nodes[y].Ghost {
						return !g.nodes[x].Ghost
					}
					return keys[x] < keys[y]
				})
				for k, i := range row {
					pos[i] = float64(k)
				}
			}
		}
	}
	cw, ch := g.maxW(c)
	step, rowH := cw+gap*2.6, ch+gap*2.8
	widest := 0
	for d := 0; d <= maxd; d++ {
		widest = max(widest, len(rows[d]))
	}
	out := map[int]pt{}
	for d := 0; d <= maxd; d++ {
		w := float64(len(rows[d])-1) * step
		off := (float64(widest-1)*step - w) / 2
		for k, i := range rows[d] {
			out[i] = pt{off + float64(k)*step, float64(d) * rowH}
		}
	}
	return out
}

// spineLeaf: the boxes with the most links on top, the rest below, leaves under the spines they touch.
func (g *lg) spineLeaf(c []int) map[int]pt {
	maxDeg := 0
	for _, i := range c {
		if !g.nodes[i].Ghost {
			maxDeg = max(maxDeg, g.deg[i])
		}
	}
	thr := max(2, int(math.Ceil(float64(maxDeg)*0.7)))
	rows := map[int][]int{}
	for _, i := range c {
		switch {
		case g.nodes[i].Ghost:
			rows[2] = append(rows[2], i)
		case g.deg[i] >= thr && len(c) > 2:
			rows[0] = append(rows[0], i)
		default:
			rows[1] = append(rows[1], i)
		}
	}
	if len(rows[0]) == 0 || len(rows[1]) == 0 && len(rows[2]) == 0 {
		return g.layered(c, "")
	}
	if len(rows[1]) == 0 {
		rows[1], rows[2] = rows[2], nil
	}
	maxd := 1
	if len(rows[2]) > 0 {
		maxd = 2
	}
	return g.placeRows(c, rows, maxd)
}

// tree: a strict tree from the root, each parent centred over its children.
func (g *lg) tree(c []int, root string) map[int]pt {
	r := g.best(c, root)
	depth, parent := g.bfs(r, c)
	kids := map[int][]int{}
	for _, i := range c {
		if p := parent[i]; p >= 0 {
			kids[p] = append(kids[p], i)
		}
	}
	cw, ch := g.maxW(c)
	step, rowH := cw+gap*2.6, ch+gap*2.8
	out := map[int]pt{}
	next := 0.0
	var place func(i int) float64
	place = func(i int) float64 {
		ks := kids[i]
		if len(ks) == 0 {
			x := next * step
			next++
			out[i] = pt{x, float64(depth[i]) * rowH}
			return x
		}
		sort.SliceStable(ks, func(a, b int) bool {
			if g.nodes[ks[a]].Ghost != g.nodes[ks[b]].Ghost {
				return !g.nodes[ks[a]].Ghost
			}
			return g.nodes[ks[a]].ID < g.nodes[ks[b]].ID
		})
		first, last := place(ks[0]), 0.0
		last = first
		for _, k := range ks[1:] {
			last = place(k)
		}
		x := (first + last) / 2
		out[i] = pt{x, float64(depth[i]) * rowH}
		return x
	}
	place(r)
	return out
}

func (g *lg) ringOrder(c []int) []int {
	in := map[int]bool{}
	for _, i := range c {
		in[i] = true
	}
	seen := map[int]bool{}
	start := c[0]
	for _, i := range c { // start at a box with the fewest links: the chain then follows the cable
		if g.deg[i] < g.deg[start] {
			start = i
		}
	}
	order := []int{start}
	seen[start] = true
	cur := start
	for len(order) < len(c) {
		next := -1
		for _, v := range g.adj[cur] {
			if !seen[v] && in[v] {
				next = v
				break
			}
		}
		if next < 0 { // dead end: continue from the nearest box that still has an unseen neighbour
			for k := len(order) - 1; k >= 0 && next < 0; k-- {
				for _, v := range g.adj[order[k]] {
					if !seen[v] && in[v] {
						next = v
						break
					}
				}
			}
		}
		if next < 0 {
			for _, i := range c {
				if !seen[i] {
					next = i
					break
				}
			}
		}
		order = append(order, next)
		seen[next] = true
		cur = next
	}
	return order
}

func (g *lg) ring(c, order []int) map[int]pt {
	cw, _ := g.maxW(c)
	n := float64(len(order))
	r := math.Max(200, (cw+gap*2.6)*n/(2*math.Pi))
	if len(order) == 1 {
		return map[int]pt{order[0]: {0, 0}}
	}
	out := map[int]pt{}
	for k, i := range order {
		a := -math.Pi/2 + 2*math.Pi*float64(k)/n
		out[i] = pt{r * math.Cos(a), r * math.Sin(a)}
	}
	return out
}

// star: the hub in the middle, its neighbours around it, the rest further out near the box they hang on.
func (g *lg) star(c []int, root string) map[int]pt {
	hub := g.best(c, root)
	depth, parent := g.bfs(hub, c)
	cw, _ := g.maxW(c)
	cell := cw + gap*2.4
	byDepth := map[int][]int{}
	maxd := 0
	for _, i := range c {
		byDepth[depth[i]] = append(byDepth[depth[i]], i)
		maxd = max(maxd, depth[i])
	}
	out := map[int]pt{hub: {0, 0}}
	ang := map[int]float64{}
	for d := 1; d <= maxd; d++ {
		ring := byDepth[d]
		sort.SliceStable(ring, func(a, b int) bool {
			x, y := ring[a], ring[b]
			if d > 1 && ang[parent[x]] != ang[parent[y]] {
				return ang[parent[x]] < ang[parent[y]]
			}
			return g.nodes[x].ID < g.nodes[y].ID
		})
		r := math.Max(float64(d)*cell*1.15, cell*float64(len(ring))/(2*math.Pi))
		for k, i := range ring {
			a := -math.Pi/2 + 2*math.Pi*float64(k)/float64(len(ring))
			ang[i] = a
			out[i] = pt{r * math.Cos(a), r * math.Sin(a)}
		}
	}
	return out
}

func (g *lg) grid(c []int) map[int]pt {
	cw, ch := g.maxW(c)
	cols := int(math.Ceil(math.Sqrt(float64(len(c)))))
	out := map[int]pt{}
	for k, i := range c {
		out[i] = pt{float64(k%cols) * (cw + gap*2.4), float64(k/cols) * (ch + gap*2)}
	}
	return out
}

// organic: a force layout (links pull, boxes push) from a fixed start, so the same graph always looks the same.
func (g *lg) organic(c []int) map[int]pt {
	cw, ch := g.maxW(c)
	k := math.Max(cw, ch) + gap*1.5
	pos := g.ring(c, g.ringOrder(c))
	for i, p := range pos {
		pos[i] = pt{p.x * 0.8, p.y * 0.8}
	}
	if len(c) == 1 {
		return pos
	}
	temp := k * float64(len(c)) / 4
	for it := 0; it < 300; it++ {
		disp := map[int]pt{}
		for _, a := range c {
			for _, b := range c {
				if a == b {
					continue
				}
				dx, dy := pos[a].x-pos[b].x, pos[a].y-pos[b].y
				d := math.Max(math.Hypot(dx, dy), 1)
				f := k * k / d
				disp[a] = pt{disp[a].x + dx/d*f, disp[a].y + dy/d*f}
			}
		}
		for _, a := range c {
			for _, b := range g.adj[a] {
				dx, dy := pos[a].x-pos[b].x, pos[a].y-pos[b].y
				d := math.Max(math.Hypot(dx, dy), 1)
				f := d * d / k
				disp[a] = pt{disp[a].x - dx/d*f, disp[a].y - dy/d*f}
			}
		}
		for _, a := range c {
			dl := math.Max(math.Hypot(disp[a].x, disp[a].y), 1)
			m := math.Min(dl, temp)
			pos[a] = pt{pos[a].x + disp[a].x/dl*m, pos[a].y + disp[a].y/dl*m}
		}
		temp *= 0.97
	}
	return pos
}

func bounds(g *lg, c []int, pos map[int]pt) (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, i := range c {
		p, n := pos[i], g.nodes[i]
		x0, y0 = math.Min(x0, p.x-n.W/2), math.Min(y0, p.y-n.H/2)
		x1, y1 = math.Max(x1, p.x+n.W/2), math.Max(y1, p.y+n.H/2)
	}
	return
}

// separate pushes overlapping boxes apart (never the pinned ones) until none touch.
func separate(g *lg, pos map[int]pt, pinned map[int]bool) {
	ids := make([]int, 0, len(pos))
	for i := range pos {
		ids = append(ids, i)
	}
	sort.Ints(ids)
	for it := 0; it < 400; it++ {
		moved := false
		for a := 0; a < len(ids); a++ {
			for b := a + 1; b < len(ids); b++ {
				i, j := ids[a], ids[b]
				pi, pj := pos[i], pos[j]
				ox := (g.nodes[i].W+g.nodes[j].W)/2 + gap/2 - math.Abs(pi.x-pj.x)
				oy := (g.nodes[i].H+g.nodes[j].H)/2 + gap/2 - math.Abs(pi.y-pj.y)
				if ox <= 0 || oy <= 0 {
					continue
				}
				moved = true
				mi, mj := 0.5, 0.5
				if pinned[i] {
					mi, mj = 0, 1
				}
				if pinned[j] {
					mi, mj = 1, 0
				}
				if pinned[i] && pinned[j] {
					continue
				}
				if ox < oy {
					s := 1.0
					if pi.x > pj.x || pi.x == pj.x && i > j {
						s = -1
					}
					pos[i] = pt{pi.x - s*ox*mi, pi.y}
					pos[j] = pt{pj.x + s*ox*mj, pj.y}
				} else {
					s := 1.0
					if pi.y > pj.y || pi.y == pj.y && i > j {
						s = -1
					}
					pos[i] = pt{pos[i].x, pi.y - s*oy*mi}
					pos[j] = pt{pos[j].x, pj.y + s*oy*mj}
				}
			}
		}
		if !moved {
			break
		}
	}
}

// Crossings counts pairs of links that cross (links sharing a box do not count).
func Crossings(nodes []LNode, edges []LEdge) int {
	at := map[string]LNode{}
	for _, n := range nodes {
		at[n.ID] = n
	}
	n := 0
	for i := 0; i < len(edges); i++ {
		for j := i + 1; j < len(edges); j++ {
			a, b := edges[i], edges[j]
			if a.A == b.A || a.A == b.B || a.B == b.A || a.B == b.B {
				continue
			}
			p1, p2, q1, q2 := at[a.A], at[a.B], at[b.A], at[b.B]
			if segCross(p1.X, p1.Y, p2.X, p2.Y, q1.X, q1.Y, q2.X, q2.Y) {
				n++
			}
		}
	}
	return n
}

func segCross(ax, ay, bx, by, cx, cy, dx, dy float64) bool {
	o := func(px, py, qx, qy, rx, ry float64) float64 { return (qx-px)*(ry-py) - (qy-py)*(rx-px) }
	d1, d2 := o(ax, ay, bx, by, cx, cy), o(ax, ay, bx, by, dx, dy)
	d3, d4 := o(cx, cy, dx, dy, ax, ay), o(cx, cy, dx, dy, bx, by)
	return d1*d2 < 0 && d3*d4 < 0
}

func edgeLength(nodes []LNode, edges []LEdge) float64 {
	at := map[string]LNode{}
	for _, n := range nodes {
		at[n.ID] = n
	}
	t := 0.0
	for _, e := range edges {
		t += math.Hypot(at[e.A].X-at[e.B].X, at[e.A].Y-at[e.B].Y)
	}
	return t
}

// Layout arranges the boxes in the requested style. Separate groups (boxes not joined by any link) are laid
// out one by one and set side by side in rows; pinned boxes stay where they are and the rest is placed
// clear of them. No two boxes overlap in the result.
func Layout(req LayoutRequest) LayoutResult {
	style := req.Style
	if style == "auto" || style == "" {
		return layoutAuto(req)
	}
	return LayoutResult{Style: style, Nodes: layoutStyle(req, style)}
}

func layoutStyle(req LayoutRequest, style string) []LNode {
	g := newLG(req.Nodes, req.Edges)
	width := req.Width
	if width <= 0 {
		width = 1400
	}
	pinned := map[int]bool{}
	pos := map[int]pt{}
	for i, n := range g.nodes {
		if n.Pinned {
			pinned[i] = true
			pos[i] = pt{n.X, n.Y}
		}
	}
	// where free space starts: below whatever is pinned
	y0, x0 := 0.0, 0.0
	if len(pinned) > 0 {
		var all []int
		for i := range pinned {
			all = append(all, i)
		}
		bx0, _, _, by := bounds(g, all, pos)
		y0, x0 = by+gap*1.5, bx0
	}
	rowX, rowY, rowH := x0, y0, 0.0
	for _, c := range g.components() {
		var free []int
		for _, i := range c {
			if !pinned[i] {
				free = append(free, i)
			}
		}
		if len(free) == 0 {
			continue
		}
		gp := g.layoutGroup(style, free, req.Root)
		bx0, by0, bx1, by1 := bounds(g, free, gp)
		w, h := bx1-bx0, by1-by0
		if rowX > x0 && rowX+w > x0+width {
			rowX, rowY, rowH = x0, rowY+rowH+gap*1.5, 0
		}
		for _, i := range free { // the group's left and top edge go to the row's place; positions are centres
			pos[i] = pt{rowX + gp[i].x - bx0, rowY + gp[i].y - by0}
		}
		rowX += w + gap*2
		rowH = math.Max(rowH, h)
	}
	separate(g, pos, pinned)
	out := make([]LNode, len(g.nodes))
	for i, n := range g.nodes {
		n.X, n.Y = math.Round(pos[i].x*10)/10, math.Round(pos[i].y*10)/10
		out[i] = n
	}
	return out
}

func layoutAuto(req LayoutRequest) LayoutResult {
	best, bestScore := "", math.Inf(1)
	var bestNodes []LNode
	cell, n := 1.0, math.Max(float64(len(req.Nodes)), 1)
	for _, b := range req.Nodes {
		cell = math.Max(cell, math.Max(b.W, b.H)+gap)
	}
	for _, st := range []string{"layered", "spine-leaf", "tree", "star", "ring", "organic"} {
		ns := layoutStyle(req, st)
		x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, b := range ns {
			x0, y0 = math.Min(x0, b.X), math.Min(y0, b.Y)
			x1, y1 = math.Max(x1, b.X), math.Max(y1, b.Y)
		}
		diag := math.Hypot(x1-x0, y1-y0)
		avgEdge := 0.0
		if len(req.Edges) > 0 {
			avgEdge = edgeLength(ns, req.Edges) / float64(len(req.Edges))
		}
		// crossings dominate; then short links and a compact drawing (both in box sizes, so a bigger drawing is not "better")
		score := float64(Crossings(ns, req.Edges))*1000 + avgEdge/cell*10 + diag/(cell*math.Sqrt(n))*5
		if st != "layered" {
			score += 5 // a tie goes to the layered look people expect
		}
		if score < bestScore {
			best, bestScore, bestNodes = st, score, ns
		}
	}
	return LayoutResult{Style: best, Nodes: bestNodes}
}

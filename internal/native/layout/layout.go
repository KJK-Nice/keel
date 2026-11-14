// Package layout is the split tree of one tab: a binary tree whose leaves are
// panes. It has no UI.
package layout

// Dir is how a split lays out its two children.
type Dir int

const (
	Row Dir = iota // side by side
	Col            // stacked
)

// Node is a leaf (Pane set) or a split (A and B set).
type Node struct {
	Pane  string
	Dir   Dir
	Ratio float32 // share of the first child, 0 to 1
	A, B  *Node
}

// Leaf is a tree of one pane.
func Leaf(pane string) *Node { return &Node{Pane: pane} }

func (n *Node) leaf() bool { return n.A == nil }

// Leaves lists the panes in reading order.
func (n *Node) Leaves() []string {
	if n == nil {
		return nil
	}
	if n.leaf() {
		return []string{n.Pane}
	}
	return append(n.A.Leaves(), n.B.Leaves()...)
}

// Split puts added after target in direction d, at ratio 0.5, and returns the
// new root. An unknown target leaves the tree as it is.
func (n *Node) Split(target, added string, d Dir) *Node {
	if n == nil {
		return nil
	}
	if n.leaf() {
		if n.Pane != target {
			return n
		}
		return &Node{Dir: d, Ratio: 0.5, A: n, B: Leaf(added)}
	}
	n.A, n.B = n.A.Split(target, added, d), n.B.Split(target, added, d)
	return n
}

// Remove drops a pane; its sibling takes the parent's place. It returns nil
// when the last leaf goes.
func (n *Node) Remove(pane string) *Node {
	if n == nil {
		return nil
	}
	if n.leaf() {
		if n.Pane == pane {
			return nil
		}
		return n
	}
	a, b := n.A.Remove(pane), n.B.Remove(pane)
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	n.A, n.B = a, b
	return n
}

// Swap exchanges the places of two panes of the tree: each keeps its split and ratio.
func (n *Node) Swap(a, b string) {
	if n == nil || a == b {
		return
	}
	if n.leaf() {
		switch n.Pane {
		case a:
			n.Pane = b
		case b:
			n.Pane = a
		}
		return
	}
	n.A.Swap(a, b)
	n.B.Swap(a, b)
}

// Edge is the side of a pane another one is dropped on.
type Edge string

const (
	Left, Right, Top, Bottom Edge = "left", "right", "top", "bottom"
)

// Beside takes pane out of the tree and puts it on an edge of target at ratio 0.5: left and right split side by side, top and bottom stack, left and top put it first. It
// returns the new root, or the tree unchanged and false when pane is the only leaf, pane is target, or either is unknown.
func Beside(root *Node, pane, target string, edge Edge) (*Node, bool) {
	has := func(id string) bool {
		for _, l := range root.Leaves() {
			if l == id {
				return true
			}
		}
		return false
	}
	if root == nil || pane == target || !has(pane) || !has(target) || len(root.Leaves()) < 2 {
		return root, false
	}
	d := Row
	switch edge {
	case Left, Right:
	case Top, Bottom:
		d = Col
	default:
		return root, false
	}
	rest := root.Remove(pane)
	in := Leaf(pane)
	var put func(n *Node) *Node
	put = func(n *Node) *Node {
		if n.leaf() {
			if n.Pane != target {
				return n
			}
			a, b := n, in
			if edge == Left || edge == Top {
				a, b = in, n
			}
			return &Node{Dir: d, Ratio: 0.5, A: a, B: b}
		}
		n.A, n.B = put(n.A), put(n.B)
		return n
	}
	return put(rest), true
}

// Dock puts the whole tree in beside target's pane of root, on edge, at ratio 0.5 ("center" or no edge docks to the right). It returns the new root, or root unchanged and false when
// target is not a leaf of root, in is nil or the two share a pane.
func Dock(root, in *Node, target string, edge Edge) (*Node, bool) {
	if root == nil || in == nil {
		return root, false
	}
	have := map[string]bool{}
	for _, l := range root.Leaves() {
		have[l] = true
	}
	if !have[target] {
		return root, false
	}
	for _, l := range in.Leaves() {
		if have[l] {
			return root, false
		}
	}
	d := Row
	switch edge {
	case Left, Right, "center", "":
	case Top, Bottom:
		d = Col
	default:
		return root, false
	}
	var put func(n *Node) *Node
	put = func(n *Node) *Node {
		if n.leaf() {
			if n.Pane != target {
				return n
			}
			a, b := n, in
			if edge == Left || edge == Top {
				a, b = in, n
			}
			return &Node{Dir: d, Ratio: 0.5, A: a, B: b}
		}
		n.A, n.B = put(n.A), put(n.B)
		return n
	}
	return put(root), true
}

// ClampRatio keeps both sides of a split at least min wide when avail is the
// room for both. With less room than two minimums it is 0.5.
func ClampRatio(r, avail, min float32) float32 {
	if avail <= 0 {
		return 0.5
	}
	lo := min / avail
	if lo >= 0.5 {
		return 0.5
	}
	return max(lo, minf(r, 1-lo))
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

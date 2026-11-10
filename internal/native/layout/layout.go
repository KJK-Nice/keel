// Package layout is the split tree of one tab: a binary tree whose leaves are
// panes, the same model as cmd/keel/web/js/lib/layout.js. It has no UI.
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

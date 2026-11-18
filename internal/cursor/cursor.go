// Package cursor implements unread cursors as api/post-format.md defines them:
// per node, the numbers a reader has seen, as a contiguous prefix plus the
// numbers seen beyond a gap. It is pure.
package cursor

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Range is what a reader has seen of one node.
type Range struct {
	Contig int   `json:"contig"`
	Above  []int `json:"above,omitempty"`
}

// Cursor is what a reader has seen of every node.
type Cursor struct {
	// Reader is the reader's harness/session, recorded because the cursor's
	// file is named by a hash of it.
	Reader string            `json:"reader,omitempty"`
	Nodes  map[string]*Range `json:"nodes"`
}

// New returns a cursor that has seen nothing.
func New() *Cursor { return &Cursor{Nodes: map[string]*Range{}} }

// Parse reads a cursor file.
func Parse(data []byte) (*Cursor, error) {
	c := New()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	if c.Nodes == nil {
		c.Nodes = map[string]*Range{}
	}
	// Normalize, so that a cursor written by hand or by another version
	// still obeys the invariants that Seen and Mark rely on.
	for node, r := range c.Nodes {
		if r == nil {
			delete(c.Nodes, node)
			continue
		}
		r.Contig = max(r.Contig, 0)
		above := r.Above
		r.Above = nil
		for _, n := range above {
			c.Mark(node, n)
		}
	}
	return c, nil
}

// Marshal returns the cursor file's contents.
func (c *Cursor) Marshal() []byte {
	// Marshaling maps with string keys, ints, and slices can't fail.
	data, _ := json.Marshal(c)
	return append(data, '\n')
}

// Seen reports whether the reader has seen post n of node.
func (c *Cursor) Seen(node string, n int) bool {
	r := c.Nodes[node]
	if r == nil {
		return false
	}
	if n <= r.Contig {
		return true
	}
	_, found := slices.BinarySearch(r.Above, n)
	return found
}

// Mark records that the reader has seen post n of node.
func (c *Cursor) Mark(node string, n int) {
	if n < 1 || c.Seen(node, n) {
		return
	}
	r := c.Nodes[node]
	if r == nil {
		r = &Range{}
		c.Nodes[node] = r
	}
	i, _ := slices.BinarySearch(r.Above, n)
	r.Above = slices.Insert(r.Above, i, n)
	for len(r.Above) > 0 && r.Above[0] == r.Contig+1 {
		r.Contig++
		r.Above = r.Above[1:]
	}
	if len(r.Above) == 0 {
		r.Above = nil
	}
}

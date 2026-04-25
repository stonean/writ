package ast

// Node is the marker interface implemented by every AST node.
//
// Every node carries a Span identifying the original source range it
// covers. After include flattening the span still references the file
// the construct was written in, not the post-flatten position in the
// root file.
type Node interface {
	Span() Span
}

// nodeBase is embedded by every concrete node and carries the span.
type nodeBase struct {
	span Span
}

// Span returns the source range this node covers.
func (n nodeBase) Span() Span { return n.span }

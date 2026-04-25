// Package ast defines the AST produced by the Writ DSL parser.
//
// The AST is exported so the runtime, code generator, and tooling
// (LSP, writ show, writ routes) can consume it from a sibling package.
// Pre-1.0 the AST is treated as an internal contract: node shapes,
// fields, and method sets may change without notice across minor
// versions. Third-party consumers are welcome but accept that
// instability. At Writ 1.0 the AST contract is reassessed and stable
// parts are promoted to compatibility guarantees.
package ast

// Source owns the original bytes of a single .writ file.
//
// A Source is created once when a file is read and is shared by every
// Position and Span pointing into it. Callers must not mutate Bytes;
// the parser and downstream tools rely on its content being stable.
type Source struct {
	Path  string
	Bytes []byte
}

// Position points to a single byte boundary inside a Source.
//
// Line and Column are 1-based. Offset is a 0-based byte offset into
// Source.Bytes. The zero Position (Source == nil, Line == 0, Column == 0,
// Offset == 0) is reserved for optional spans that were absent in the
// source (see, for example, CommitStmt.NameSpan when no result name is
// written).
type Position struct {
	Source *Source
	Line   int
	Column int
	Offset int
}

// Span is a half-open [Start, End) byte range inside a single Source.
// Start and End always reference the same Source.
type Span struct {
	Start Position
	End   Position
}

// Text returns the verbatim source bytes the span covers.
//
// The returned slice aliases the underlying Source.Bytes; callers must
// not mutate it. If the span is the zero value, Text returns nil.
func (s Span) Text() []byte {
	if s.Start.Source == nil {
		return nil
	}
	return s.Start.Source.Bytes[s.Start.Offset:s.End.Offset]
}
